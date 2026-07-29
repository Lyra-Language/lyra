package llvm

import (
	"fmt"
	"strings"

	"github.com/llir/llvm/ir"
	"github.com/llir/llvm/ir/constant"
	lltypes "github.com/llir/llvm/ir/types"
	"github.com/llir/llvm/ir/value"

	"github.com/Lyra-Language/lyra/pkg/analyzer/ownership"
	"github.com/Lyra-Language/lyra/pkg/types"
)

// Recursive drop glue — freeing the managed values a dying aggregate owns.
//
// A ref-counted box is `{ i64 rc, payload }`, and lyra_rc_release takes a
// `drop_fn` to run on the payload just before the box is freed (runtime.go). Until
// this file existed every release passed a *null* drop_fn, so a managed value
// stored in an aggregate field — a string in a struct, a `shared` tail in a `Cons`
// cell — was simply abandoned when its owner died: freeing a list freed the head
// cell and leaked the entire tail.
//
// dropFnFor closes that: for a payload type T it generates (once, cached)
//
//	void @lyra_drop_T(i8* payload)
//
// which releases every managed reference reachable *by value* from T. "By value"
// is the recursion's stopping rule and the reason it terminates: a managed field
// (a string, or a `shared X`) is released with a single lyra_rc_release and never
// walked into — that box runs its *own* drop_fn if and when its refcount reaches
// zero. So the walk only descends through inline stack aggregates, and a recursive
// type's cycle must pass through a `shared` field (lyra-E014), which is exactly
// where the walk stops. Deep structures are therefore freed one box at a time, by
// a chain of drop_fn calls, not by one deep recursion at compile time.
//
// The generated function is registered in the cache *before* its body is built, so
// a self-referential type (`data List = Cons(string, shared List)`) emits one
// function that calls itself through the tail's release rather than recursing
// forever during generation.
//
// Scope: this runs wherever a value that owns managed references dies — as a box's
// drop_fn when a `shared` value's refcount reaches zero, and (since deep-retain-on-
// copy) as the scope-exit release of a plain **stack** aggregate binding, called
// directly on the value via deepRelease (retain.go).
//
// This glue has an exact counterpart in retain.go, and the two must stay in step.
// Whatever a *copy* of a type retains, the copy's death has to release: a retain
// without a matching drop leaks, and a drop without a matching retain is a
// use-after-free. That symmetry is the whole ownership invariant for aggregates, and
// TestEmit_RetainGlueMirrorsDropGlue checks the generated pair covers the same
// fields rather than leaving it to inspection.

// needsDrop reports whether a value of Lyra type t owns any reference-counted
// reference that must be released when the value dies — and, symmetrically, retained
// when it is copied. A managed type *is* such a reference; an inline aggregate needs
// a drop when any field does.
//
// It delegates to ownership.OwnsManaged so the backend and the ownership pass share
// **one** definition. They must agree exactly: the pass decides where a +1 is minted
// and this side decides where one is released, so any divergence between them is
// either a leak or a double free.
func (l *lowerer) needsDrop(t types.Type) bool {
	return ownership.OwnsManaged(t, l.res.SymbolTable)
}

// resolveNamedType resolves an UnresolvedType (how a reference to another declared
// type is recorded in a field/element) to the declaration's actual type, carrying
// the reference's own allocation flavor across. Any other type is returned
// unchanged. Unlike resolveForLayout this is shallow — one level is all the drop
// walk needs, since it recurses field by field anyway.
func (l *lowerer) resolveNamedType(t types.Type) types.Type {
	// Strip newtype wrappers first: a field declared `Email` is recorded as an
	// UnresolvedType naming a newtype over string, and the walk must see the
	// string it *is* — reading the wrapper matches no case below, so its box
	// would never be released.
	t = l.stripNewtype(t)
	u, ok := t.(types.UnresolvedType)
	if !ok {
		return t
	}
	decl, ok := l.res.SymbolTable.Types[u.Name]
	if !ok {
		return t
	}
	return l.stripNewtype(types.WithAllocation(decl.Type, u.Allocation))
}

// nullDropFn is the "nothing to drop" drop_fn argument: a null i8*, which
// lyra_rc_release / lyra_rc_drop_reuse both test for and skip.
func nullDropFn() value.Value {
	return constant.NewNull(lltypes.NewPointer(lltypes.I8))
}

// boxDropFn returns the drop_fn to pass when releasing a managed value of Lyra
// type t — the function that frees what t's *box payload* owns:
//
//   - a string: null. Its payload is raw bytes, owning nothing.
//   - a `shared X`: the drop glue for X's by-value payload, which releases X's own
//     managed fields (and, transitively through their boxes, the rest).
//   - anything else (including an unknown/nil type): null, the conservative answer
//     — a missing drop leaks, which is memory-safe.
func (l *lowerer) boxDropFn(t types.Type) (value.Value, error) {
	t = l.stripNewtype(t) // a newtype's box is its base's box
	if t == nil || types.IsString(t) {
		return nullDropFn(), nil
	}
	// A dynamic array `[]T` owns its elements: its drop_fn loops over the runtime
	// length releasing each (null when T owns nothing managed).
	if dyn, ok := t.(types.DynamicArrayType); ok {
		return l.dynArrayDropFn(dyn)
	}
	if types.AllocationOf(t) != types.Shared {
		return nullDropFn(), nil
	}
	// Strip to Stack (an explicit flavor — WithAllocation treats Unspecified as "no
	// change") to name the by-value payload sitting inside the box.
	return l.dropFnFor(types.WithAllocation(t, types.Stack))
}

// dropFnFor returns an i8* to the drop function for a payload of Lyra type t, or a
// null i8* when t owns nothing managed — the form lyra_rc_release wants for a box's
// drop_fn argument. The returned pointer is a constant expression, so it needs no
// block. Use dropFuncFor when you need to *call* the glue rather than pass it.
func (l *lowerer) dropFnFor(t types.Type) (value.Value, error) {
	fn, err := l.dropFuncFor(t)
	if err != nil {
		return nil, err
	}
	if fn == nil {
		return nullDropFn(), nil
	}
	return constant.NewBitCast(fn, lltypes.NewPointer(lltypes.I8)), nil
}

// dropFuncFor returns the drop glue for a payload of Lyra type t, or nil when t owns
// nothing managed. Generated once per type and cached. This is the callable form —
// retain.go's deepRelease calls it directly on an aggregate value, while dropFnFor
// wraps it as the i8* a box's drop_fn slot takes.
func (l *lowerer) dropFuncFor(t types.Type) (*ir.Func, error) {
	if !l.needsDrop(t) {
		return nil, nil
	}
	// Key on the base, so a newtype and its base share one glue rather than
	// generating two identical copies under different names.
	t = l.stripNewtype(t)
	key := t.String()
	if fn, ok := l.dropFns[key]; ok {
		return fn, nil
	}

	payloadTy, err := l.lowerType(t)
	if err != nil {
		return nil, fmt.Errorf("llvm: cannot generate drop glue for %s: %w", t, err)
	}
	fn := l.module.NewFunc(l.dropFnName(key), lltypes.Void, ir.NewParam("payload", lltypes.NewPointer(lltypes.I8)))
	// Cache before building the body: a recursive type's glue reaches itself (via a
	// `shared` field's release), and it must find this entry rather than regenerate.
	l.dropFns[key] = fn

	entry := fn.NewBlock("entry")
	typed := entry.NewBitCast(fn.Params[0], lltypes.NewPointer(payloadTy))
	end, err := l.emitDropValue(entry, entry.NewLoad(payloadTy, typed), t)
	if err != nil {
		return nil, err
	}
	end.NewRet(nil)
	return fn, nil
}

// dropFnName builds a unique, readable LLVM symbol for a type's drop glue. A counter
// guarantees uniqueness even if two distinct keys mangle alike.
func (l *lowerer) dropFnName(key string) string {
	l.dropFnCount++
	return fmt.Sprintf("lyra_drop_%d_%s", l.dropFnCount, mangleTypeKey(key))
}

// mangleTypeKey reduces a type key to identifier characters so it can appear in an
// LLVM symbol name. Readability only — uniqueness comes from the caller's counter.
func mangleTypeKey(key string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			return r
		default:
			return '_'
		}
	}, key)
}

// emitDropValue releases everything the first-class value v (of Lyra type t) owns.
// It returns the block control ends in — a `data` value switches on its tag, so
// dropping it is not straight-line code.
func (l *lowerer) emitDropValue(block *ir.Block, v value.Value, t types.Type) (*ir.Block, error) {
	if !l.needsDrop(t) {
		return block, nil
	}
	// Resolve once, up front: this strips any newtype wrapper, so the managed
	// check below sees the base a `newtype Email = string` actually is.
	resolved := l.resolveNamedType(t)
	// A managed value is released as a unit; its box's own drop_fn handles whatever
	// *it* owns, when its count reaches zero. This is where the walk stops.
	if ownership.IsManaged(resolved) {
		return block, l.lowerManagedRelease(block, v, resolved)
	}

	switch rt := resolved.(type) {
	case types.NamedStructType:
		return l.emitDropFields(block, v, fieldTypesOf(rt))
	case types.TupleType:
		return l.emitDropFields(block, v, rt.Elements)
	case types.DataType:
		return l.emitDropData(block, v, rt)
	case types.StaticArrayType:
		return l.emitDropArray(block, v, rt)
	}
	return block, nil
}

// emitDropArray drops each element a `shared [N]T` box owns. N is a compile-time
// constant, so the element drops are unrolled (extractvalue per index — no branch,
// an array has one shape), the same straight-line shape as a struct/tuple's fields.
func (l *lowerer) emitDropArray(block *ir.Block, v value.Value, at types.StaticArrayType) (*ir.Block, error) {
	if !l.needsDrop(at.ElementType) {
		return block, nil
	}
	var err error
	for i := 0; i < at.Size; i++ {
		block, err = l.emitDropValue(block, block.NewExtractValue(v, uint64(i)), at.ElementType)
		if err != nil {
			return nil, err
		}
	}
	return block, nil
}

// emitDropFields drops each element of an inline aggregate that owns something,
// reading it out with extractvalue (no branch — a struct/tuple has one shape).
func (l *lowerer) emitDropFields(block *ir.Block, v value.Value, fieldTypes []types.Type) (*ir.Block, error) {
	for i, ft := range fieldTypes {
		if !l.needsDrop(ft) {
			continue
		}
		var err error
		block, err = l.emitDropValue(block, block.NewExtractValue(v, uint64(i)), ft)
		if err != nil {
			return nil, err
		}
	}
	return block, nil
}

// fieldTypesOf is a struct's field types in declaration order — the extractvalue
// index order, matching lowerStructDef.
func fieldTypesOf(st types.NamedStructType) []types.Type {
	out := make([]types.Type, len(st.Fields))
	for i, f := range st.Fields {
		out[i] = f.Type
	}
	return out
}

// emitDropData drops a `data` value: which fields it owns depends on which variant
// is live, so this switches on the tag and gives each owning variant its own block,
// reinterpreting the payload blob as that variant's payload struct (the same
// bitcast-the-blob move as construction and match) before dropping its fields.
// Variants that own nothing — and the tags of an all-nullary type — fall to the
// exit block directly.
func (l *lowerer) emitDropData(block *ir.Block, v value.Value, dt types.DataType) (*ir.Block, error) {
	llType, err := l.lowerType(types.WithAllocation(dt, types.Stack))
	if err != nil {
		return nil, err
	}
	unionTy, ok := llType.(*lltypes.StructType)
	if !ok || len(unionTy.Fields) == 0 {
		return nil, fmt.Errorf("llvm: data type %q did not lower to a tagged union", dt.Name)
	}

	fn := block.Parent
	// Store the union so a variant's payload can be reinterpreted out of the blob.
	slot := fn.Blocks[0].NewAlloca(unionTy)
	block.NewStore(v, slot)
	tagTy := unionTy.Fields[0].(*lltypes.IntType)
	tagPtr := block.NewGetElementPtr(unionTy, slot,
		constant.NewInt(lltypes.I32, 0), constant.NewInt(lltypes.I32, 0))
	tag := block.NewLoad(tagTy, tagPtr)

	exit := fn.NewBlock("")
	var cases []*ir.Case
	for i, ctor := range dt.Constructors {
		fields := ctor.FieldTypes()
		if !anyNeedsDrop(l, fields) {
			continue // nullary, or a variant owning nothing — nothing to do for this tag
		}
		armBlock := fn.NewBlock("")
		payloadStructTy, err := l.dataPayloadStructType(ctor)
		if err != nil {
			return nil, err
		}
		blobPtr := armBlock.NewGetElementPtr(unionTy, slot,
			constant.NewInt(lltypes.I32, 0), constant.NewInt(lltypes.I32, 1))
		typedPtr := armBlock.NewBitCast(blobPtr, lltypes.NewPointer(payloadStructTy))
		payload := armBlock.NewLoad(payloadStructTy, typedPtr)

		end, err := l.emitDropFields(armBlock, payload, fields)
		if err != nil {
			return nil, err
		}
		end.NewBr(exit)
		cases = append(cases, ir.NewCase(constant.NewInt(tagTy, int64(i)), armBlock))
	}
	block.NewSwitch(tag, exit, cases...)
	return exit, nil
}

// anyNeedsDrop reports whether any of these types owns something managed.
func anyNeedsDrop(l *lowerer, ts []types.Type) bool {
	for _, t := range ts {
		if l.needsDrop(t) {
			return true
		}
	}
	return false
}
