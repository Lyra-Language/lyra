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
// Scope: this covers managed values inside a *boxed* (`shared`) aggregate, whose
// death is a refcount reaching zero. A managed value inside a plain **stack**
// aggregate is still leaked conservatively — a stack aggregate is a value, so
// `let q = p` copies it and duplicates its field references with no retain, and
// dropping both copies would double-free. Making that sound needs deep-retain-on-
// copy in the ownership pass first; see ALLOCATION.md.

// needsDrop reports whether a value of Lyra type t owns any reference-counted
// reference that must be released when the value dies. A managed type *is* such a
// reference; an inline aggregate needs a drop when any field does.
func (l *lowerer) needsDrop(t types.Type) bool {
	if t == nil {
		return false
	}
	if ownership.IsManaged(t) {
		return true // a string, or a `shared` box pointer — a reference in its own right
	}
	switch v := l.resolveNamedType(t).(type) {
	case types.NamedStructType:
		for _, f := range v.Fields {
			if l.needsDrop(f.Type) {
				return true
			}
		}
	case types.TupleType:
		for _, e := range v.Elements {
			if l.needsDrop(e) {
				return true
			}
		}
	case types.DataType:
		for _, c := range v.Constructors {
			for _, f := range c.FieldTypes() {
				if l.needsDrop(f) {
					return true
				}
			}
		}
	case types.StaticArrayType:
		// A `shared [N]T` box owns its elements; it needs a drop iff T does.
		return l.needsDrop(v.ElementType)
	}
	return false
}

// resolveNamedType resolves an UnresolvedType (how a reference to another declared
// type is recorded in a field/element) to the declaration's actual type, carrying
// the reference's own allocation flavor across. Any other type is returned
// unchanged. Unlike resolveForLayout this is shallow — one level is all the drop
// walk needs, since it recurses field by field anyway.
func (l *lowerer) resolveNamedType(t types.Type) types.Type {
	u, ok := t.(types.UnresolvedType)
	if !ok {
		return t
	}
	decl, ok := l.res.SymbolTable.Types[u.Name]
	if !ok {
		return t
	}
	return types.WithAllocation(decl.Type, u.Allocation)
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
// null i8* when t owns nothing managed. The function is generated once per type
// and cached; the returned pointer is a constant expression, so it needs no block.
func (l *lowerer) dropFnFor(t types.Type) (value.Value, error) {
	if !l.needsDrop(t) {
		return nullDropFn(), nil
	}
	key := t.String()
	if fn, ok := l.dropFns[key]; ok {
		return constant.NewBitCast(fn, lltypes.NewPointer(lltypes.I8)), nil
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
	return constant.NewBitCast(fn, lltypes.NewPointer(lltypes.I8)), nil
}

// dropFnName builds a unique, readable LLVM symbol for a type's drop glue. The
// type key is mangled to identifier characters for readability; a counter
// guarantees uniqueness even if two distinct keys mangle alike.
func (l *lowerer) dropFnName(key string) string {
	mangled := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			return r
		default:
			return '_'
		}
	}, key)
	l.dropFnCount++
	return fmt.Sprintf("lyra_drop_%d_%s", l.dropFnCount, mangled)
}

// emitDropValue releases everything the first-class value v (of Lyra type t) owns.
// It returns the block control ends in — a `data` value switches on its tag, so
// dropping it is not straight-line code.
func (l *lowerer) emitDropValue(block *ir.Block, v value.Value, t types.Type) (*ir.Block, error) {
	if !l.needsDrop(t) {
		return block, nil
	}
	// A managed value is released as a unit; its box's own drop_fn handles whatever
	// *it* owns, when its count reaches zero. This is where the walk stops.
	if ownership.IsManaged(t) {
		return block, l.lowerManagedRelease(block, v, t)
	}

	switch rt := l.resolveNamedType(t).(type) {
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
