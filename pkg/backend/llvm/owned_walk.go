package llvm

import (
	"fmt"

	"github.com/llir/llvm/ir"
	"github.com/llir/llvm/ir/constant"
	lltypes "github.com/llir/llvm/ir/types"
	"github.com/llir/llvm/ir/value"

	"github.com/Lyra-Language/lyra/pkg/analyzer/ownership"
	"github.com/Lyra-Language/lyra/pkg/types"
)

// The walk over what a value owns — written once, run twice.
//
// retain.go and drop.go are the two halves of one invariant: a copy of an aggregate must
// add a reference to **exactly** the managed values its death removes one from. Miss a
// field on the retain side and it leaks; miss it on the drop side and it is freed while a
// copy still points at it. CLAUDE.md rule 8 states the consequence — "paired walks must be
// fixed in one change" — and the history is that they were not:
//
//   - `AnonymousStructType` was missing from both until 08/08, so `{ m: string }` leaked
//     one reference per value;
//   - `ParameterizedType` was missing from both until 08/07, and adding it to the *drop*
//     alone crashed TestExec_WeakOptionalField immediately — a half-fix caught, as the
//     invariant is supposed to catch it.
//
// Both of those are cases the two switches lacked *together*, which is the failure mode a
// side-by-side reading cannot find: two copies can agree and be wrong. The durable fix is
// the one rule 8 asks for — stop having two switches. The walk below is the only one; the
// two callers differ solely in what they do at a managed **leaf**, which is the single
// place the retain and the release genuinely disagree.
//
// **Equality is deliberately not folded in here.** emitEqValue looks like a third copy —
// the same arms in the same order — but it answers a different question with a different
// stopping rule: retain and drop stop *at* a managed value (its own box owns what it
// holds), while equality descends *into* one to compare it, and it returns a value rather
// than performing an effect. Nothing breaks if it visits a different set of fields than
// these two do, which is exactly what makes it a different walk rather than a third copy
// of this one.

// ownedWalk is which of the paired walks is being emitted. A constant rather than a
// struct of closures, because a closure over lowerManagedRelease makes a package-level
// initialization cycle (release reaches the drop glue, which reaches this walk).
type ownedWalk int

const (
	retainWalk ownedWalk = iota // add one reference at each managed leaf — what a *copy* owes
	dropWalk                    // remove one — what a value's *death* owes
)

// verb names the walk in the one error message that mentions it.
func (w ownedWalk) verb() string {
	if w == retainWalk {
		return "retain"
	}
	return "drop"
}

// leaf is the single place the two walks genuinely differ: what to do when the walk
// reaches a managed value. Everything above it — which fields exist, which variant is
// live, how a newtype resolves — is shared, which is the point.
func (w ownedWalk) leaf(l *lowerer, block *ir.Block, v value.Value, t types.Type) error {
	if w == retainWalk {
		l.lowerManagedRetain(block, v, t)
		return nil
	}
	return l.lowerManagedRelease(block, v, t)
}

// emitOwnedValue applies w's leaf to every managed value reachable **by value** from v.
//
// It returns the block control ends in, because a `data` value switches on its tag and so
// is not straight-line code.
//
// "By value" is the stopping rule and the termination argument both: a managed field gets
// the leaf and is never walked into, so the walk only descends through inline stack
// aggregates — and a recursive type's cycle must pass through a `shared` field
// (lyra-E014), which is precisely where it stops.
func (l *lowerer) emitOwnedValue(block *ir.Block, v value.Value, t types.Type, w ownedWalk) (*ir.Block, error) {
	if !l.needsDrop(t) {
		return block, nil
	}
	// Resolved once up front, which also strips any newtype wrapper so the managed check
	// below sees the base a `newtype Email = string` actually is.
	resolved := l.resolveNamedType(t)
	if ownership.IsManaged(resolved) {
		return block, w.leaf(l, block, v, resolved)
	}

	switch rt := resolved.(type) {
	case types.NamedStructType:
		return l.emitOwnedFields(block, v, fieldTypesOf(rt), w)
	case types.AnonymousStructType:
		return l.emitOwnedFields(block, v, anonFieldTypesOf(rt), w)
	case types.TupleType:
		return l.emitOwnedFields(block, v, rt.Elements, w)
	case types.DataType:
		return l.emitOwnedData(block, v, rt, w)
	case types.StaticArrayType:
		return l.emitOwnedArray(block, v, rt, w)
	case types.ParameterizedType:
		// One instantiation owns what its *substituted* contents own — `Maybe<string>`
		// holds a string, `Maybe<i64>` nothing — so the decision must be made against the
		// substitution. resolveNamedType above only resolves an UnresolvedType, so a
		// ParameterizedType arrives here still parameterized and would otherwise match no
		// case at all, emitting nothing.
		inst := l.resolveShape(rt)
		if _, unresolved := inst.(types.ParameterizedType); unresolved {
			// Unreachable in a well-typed program: needsDrop returned true above, and it
			// says so only by resolving this same instantiation. Rule 5 — a silent
			// `return block, nil` here is exactly the leak this case exists to prevent.
			return nil, fmt.Errorf("llvm: cannot %s a value of generic type %s: "+
				"its instantiation did not resolve", w.verb(), rt)
		}
		return l.emitOwnedValue(block, v, inst, w)
	}
	return block, nil
}

// emitOwnedFields walks each element of an inline aggregate that owns something, reading
// it out with extractvalue — no branch, since a struct or tuple has one shape.
func (l *lowerer) emitOwnedFields(block *ir.Block, v value.Value, fieldTypes []types.Type, w ownedWalk) (*ir.Block, error) {
	for i, ft := range fieldTypes {
		if !l.needsDrop(ft) {
			continue
		}
		var err error
		block, err = l.emitOwnedValue(block, block.NewExtractValue(v, uint64(i)), ft, w)
		if err != nil {
			return nil, err
		}
	}
	return block, nil
}

// emitOwnedArray walks each element of an inline `[N]T`. N is a compile-time constant, so
// the elements are unrolled — the same straight-line shape as a struct's fields.
func (l *lowerer) emitOwnedArray(block *ir.Block, v value.Value, at types.StaticArrayType, w ownedWalk) (*ir.Block, error) {
	if !l.needsDrop(at.ElementType) {
		return block, nil
	}
	var err error
	for i := 0; i < at.Size; i++ {
		block, err = l.emitOwnedValue(block, block.NewExtractValue(v, uint64(i)), at.ElementType, w)
		if err != nil {
			return nil, err
		}
	}
	return block, nil
}

// emitOwnedData walks a `data` value: which fields it owns depends on which variant is
// live, so this switches on the tag and gives each owning variant its own block,
// reinterpreting the payload blob as that variant's payload struct (the same
// bitcast-the-blob move as construction and match) before walking its fields. Variants
// owning nothing — and the tags of an all-nullary type — fall to the exit block directly.
//
// One walk means a variant's retain and its drop cover the same fields by construction,
// rather than by two tag switches happening to agree on order.
func (l *lowerer) emitOwnedData(block *ir.Block, v value.Value, dt types.DataType, w ownedWalk) (*ir.Block, error) {
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

		end, err := l.emitOwnedFields(armBlock, payload, fields, w)
		if err != nil {
			return nil, err
		}
		end.NewBr(exit)
		cases = append(cases, ir.NewCase(constant.NewInt(tagTy, int64(i)), armBlock))
	}
	block.NewSwitch(tag, exit, cases...)
	return exit, nil
}

// fieldTypesOf is a struct's field types in declaration order — the extractvalue index
// order, matching lowerStructDef. anonFieldTypesOf is the same for the structural struct:
// two functions rather than one, because the two field slices are separate types. What
// matters is that every walk uses these, in the type's own field order.
func fieldTypesOf(st types.NamedStructType) []types.Type {
	out := make([]types.Type, len(st.Fields))
	for i, f := range st.Fields {
		out[i] = f.Type
	}
	return out
}

func anonFieldTypesOf(st types.AnonymousStructType) []types.Type {
	out := make([]types.Type, len(st.Fields))
	for i, f := range st.Fields {
		out[i] = f.Type
	}
	return out
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
