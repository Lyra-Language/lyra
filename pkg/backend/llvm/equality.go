package llvm

import (
	"fmt"

	"github.com/llir/llvm/ir"
	"github.com/llir/llvm/ir/constant"
	"github.com/llir/llvm/ir/enum"
	lltypes "github.com/llir/llvm/ir/types"
	"github.com/llir/llvm/ir/value"

	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/types"
)

// Structural equality on an aggregate — a struct, a tuple, a `data` value, an inline
// array.
//
// The typechecker has always accepted `a == b` on these (`areEqualityCompatible`) and
// the backend has always refused to lower it: *"comparison of non-integer operands not
// implemented"*. That is hazard 5 inverted — the front end accepted a form the backend
// never built — and it is the third instance of that shape dug out this week, after the
// type-name member call and the `where`-bound call.
//
// **A per-type glue function, not an inlined comparison.** The two reasons are the same
// ones drop.go gives for its glue. A `data` value's equality has to branch on the tag,
// and a branching *call site* returns a merge block, which the pending-temporaries
// machinery does not handle (the fault that made `read_line`'s lowering branchless).
// Emitting a function keeps the call site a single instruction and puts the branching
// inside, where it is nobody else's problem. It also means a recursive type's equality
// reaches itself through a cache entry rather than expanding forever.
//
// Equality is **structural and total** here: every field is compared, in declaration
// order, with `&&`. A type that wants different equality implements the prelude's `Eq`,
// which is dispatched before this is ever reached (typechecker's dispatchEq).

// eqFuncFor returns the equality glue for t, generating it on first use.
//
// Keyed on the base type, so a newtype and its base share one function rather than
// generating two identical copies — the same keying dropFuncFor uses, for the same
// reason.
func (l *lowerer) eqFuncFor(t types.Type) (*ir.Func, error) {
	t = l.stripNewtype(t)
	key := t.String()
	if fn, ok := l.eqFns[key]; ok {
		return fn, nil
	}
	valTy, err := l.lowerType(t)
	if err != nil {
		return nil, fmt.Errorf("llvm: cannot generate equality for %s: %w", t, err)
	}
	a := ir.NewParam("a", valTy)
	b := ir.NewParam("b", valTy)
	fn := l.module.NewFunc(l.eqFnName(key), lltypes.I1, a, b)
	// Cached before the body is built: a recursive type's equality reaches itself.
	l.eqFns[key] = fn

	entry := fn.NewBlock("entry")
	result, end, err := l.emitEqValue(entry, a, b, t)
	if err != nil {
		return nil, err
	}
	end.NewRet(result)
	return fn, nil
}

// emitEqValue compares two values of type t, returning the i1 result and the block to
// continue in.
//
// The scalar cases are the leaves and everything else recurses into them. A managed
// value (a string, a `shared` box) compares through the path that already exists rather
// than by pointer identity: two equal strings in different boxes are equal.
func (l *lowerer) emitEqValue(block *ir.Block, a, b value.Value, t types.Type) (value.Value, *ir.Block, error) {
	// A `shared` aggregate is a box pointer, so unbox before comparing — which is what
	// this function's own header already promised ("compares through the path that
	// already exists rather than by pointer identity") and did not do. Without it the
	// field walk ran over the *box*, so `a == b` on two shared structs compared their
	// refcounts and then failed to lower the payload field; rule 5 is why that was an
	// error rather than an answer.
	//
	// Two boxes with equal payloads are equal, matching the string case beside it.
	if types.AllocationOf(t) == types.Shared {
		if _, isPtr := a.Type().(*lltypes.PointerType); isPtr {
			stack := types.WithAllocation(t, types.Stack)
			av, err := l.loadSharedPayload(block, a, stack)
			if err != nil {
				return nil, nil, err
			}
			bv, err := l.loadSharedPayload(block, b, stack)
			if err != nil {
				return nil, nil, err
			}
			return l.emitEqValue(block, av, bv, stack)
		}
	}
	resolved := l.resolveNamedType(t)
	switch rt := resolved.(type) {
	case types.NamedStructType:
		return l.emitEqFields(block, a, b, fieldTypesOf(rt))
	case types.AnonymousStructType:
		anon := make([]types.Type, len(rt.Fields))
		for i, f := range rt.Fields {
			anon[i] = f.Type
		}
		return l.emitEqFields(block, a, b, anon)
	case types.TupleType:
		return l.emitEqFields(block, a, b, rt.Elements)
	case types.StaticArrayType:
		elems := make([]types.Type, rt.Size)
		for i := range elems {
			elems[i] = rt.ElementType
		}
		return l.emitEqFields(block, a, b, elems)
	case types.DataType:
		return l.emitEqData(block, a, b, rt)
	case types.ParameterizedType:
		// One instantiation compares what its *substituted* contents compare — the arm
		// retain.go and drop.go both needed and both were missing until 08/07.
		inst := l.resolveShape(rt)
		if _, unresolved := inst.(types.ParameterizedType); unresolved {
			return nil, nil, fmt.Errorf("llvm: cannot compare a value of generic type %s: "+
				"its instantiation did not resolve", rt)
		}
		return l.emitEqValue(block, a, b, inst)
	}
	// A scalar leaf: string, float, or an integer-shaped value (bool and rune
	// included). These are exactly the cases lowerBooleanBinaryOpExpr already handles
	// for a top-level `==`, reached here for a *field* of an aggregate.
	if isStringLLVMType(a.Type()) {
		return l.lowerStringEquality(block, a, b), block, nil
	}
	if _, isFloat := a.Type().(*lltypes.FloatType); isFloat {
		return block.NewFCmp(enum.FPredOEQ, a, b), block, nil
	}
	if _, isInt := a.Type().(*lltypes.IntType); isInt {
		return block.NewICmp(enum.IPredEQ, a, b), block, nil
	}
	return nil, nil, fmt.Errorf("llvm: structural equality on %s is not implemented", t)
}

// loadSharedPayload reads a box's payload as a first-class value. `stack` is the payload's
// type with the `shared` flavor already stripped, so lowering it yields the payload struct
// rather than the box pointer it came from.
func (l *lowerer) loadSharedPayload(block *ir.Block, box value.Value, stack types.Type) (value.Value, error) {
	payloadTy, err := l.lowerType(stack)
	if err != nil {
		return nil, err
	}
	boxTy := SharedBoxType(payloadTy)
	ptr := block.NewGetElementPtr(boxTy, box, i32c(0), i32c(boxPayloadField))
	return block.NewLoad(payloadTy, ptr), nil
}

// emitEqFields ANDs the field-wise comparisons of an aggregate.
//
// Not short-circuiting: every field is compared and the results are ANDed. A field
// comparison is a handful of instructions with no side effect and no trap — the string
// case is a length test and a memcmp — so branching past the rest would cost more in
// blocks than it saves in work, and it would reintroduce the merge block this file
// exists to avoid.
func (l *lowerer) emitEqFields(block *ir.Block, a, b value.Value, fields []types.Type) (value.Value, *ir.Block, error) {
	result := value.Value(constant.NewInt(lltypes.I1, 1))
	for i, ft := range fields {
		fa := block.NewExtractValue(a, uint64(i))
		fb := block.NewExtractValue(b, uint64(i))
		eq, next, err := l.emitEqValue(block, fa, fb, ft)
		if err != nil {
			return nil, nil, err
		}
		block = next
		result = block.NewAnd(result, eq)
	}
	return result, block, nil
}

// emitEqData compares two `data` values: same variant, and equal payloads within it.
//
// The tag test comes first and the payload comparison is per-variant, which needs a
// branch — the reason this file emits a function at all. Each variant's block compares
// that variant's payload and jumps to a merge; a tag mismatch skips straight to it with
// `false`.
func (l *lowerer) emitEqData(block *ir.Block, a, b value.Value, dt types.DataType) (value.Value, *ir.Block, error) {
	llType, err := l.lowerType(types.WithAllocation(dt, types.Stack))
	if err != nil {
		return nil, nil, err
	}
	unionTy, ok := llType.(*lltypes.StructType)
	if !ok || len(unionTy.Fields) == 0 {
		return nil, nil, fmt.Errorf("llvm: data type %q did not lower to a tagged union", dt.Name)
	}
	fn := block.Parent
	// Stored so a variant's payload can be reinterpreted out of the blob — the same
	// bitcast-the-blob move construction, match and the drop glue all use.
	slotA := fn.Blocks[0].NewAlloca(unionTy)
	slotB := fn.Blocks[0].NewAlloca(unionTy)
	block.NewStore(a, slotA)
	block.NewStore(b, slotB)

	tagA := block.NewExtractValue(a, 0)
	tagB := block.NewExtractValue(b, 0)
	sameTag := block.NewICmp(enum.IPredEQ, tagA, tagB)
	merge := fn.NewBlock("")
	payloads := fn.NewBlock("")
	block.NewCondBr(sameTag, payloads, merge)
	mismatch := block

	// A variant with no payload is equal once its tags match, so a data type of only
	// nullary constructors needs no switch at all.
	anyPayload := false
	for _, c := range dt.Constructors {
		if len(c.FieldTypes()) > 0 {
			anyPayload = true
		}
	}
	if !anyPayload {
		payloads.NewBr(merge)
		phi := merge.NewPhi(
			ir.NewIncoming(constant.NewInt(lltypes.I1, 0), mismatch),
			ir.NewIncoming(constant.NewInt(lltypes.I1, 1), payloads),
		)
		return phi, merge, nil
	}

	incoming := []*ir.Incoming{ir.NewIncoming(constant.NewInt(lltypes.I1, 0), mismatch)}
	var cases []*ir.Case
	for i, c := range dt.Constructors {
		fields := c.FieldTypes()
		arm := fn.NewBlock("")
		if len(fields) == 0 {
			arm.NewBr(merge)
			incoming = append(incoming, ir.NewIncoming(constant.NewInt(lltypes.I1, 1), arm))
		} else {
			payloadStructTy, err := l.dataPayloadStructType(c)
			if err != nil {
				return nil, nil, err
			}
			load := func(slot value.Value) value.Value {
				blob := arm.NewGetElementPtr(unionTy, slot,
					i32c(0), i32c(1))
				return arm.NewLoad(payloadStructTy, arm.NewBitCast(blob, lltypes.NewPointer(payloadStructTy)))
			}
			pa, pb := load(slotA), load(slotB)
			eq, end, err := l.emitEqFields(arm, pa, pb, fields)
			if err != nil {
				return nil, nil, err
			}
			end.NewBr(merge)
			incoming = append(incoming, ir.NewIncoming(eq, end))
		}
		cases = append(cases, ir.NewCase(constant.NewInt(lltypes.I8, int64(i)), arm))
	}
	// The default is unreachable in a well-typed program — every tag is a declared
	// variant — but LLVM requires one, and `merge` with a false incoming is the answer
	// that cannot be wrong.
	unreachableArm := fn.NewBlock("")
	unreachableArm.NewBr(merge)
	incoming = append(incoming, ir.NewIncoming(constant.NewInt(lltypes.I1, 0), unreachableArm))
	payloads.NewSwitch(tagA, unreachableArm, cases...)

	return merge.NewPhi(incoming...), merge, nil
}

// eqFnName names an equality glue function. Readability only — uniqueness comes from
// the counter, exactly as dropFnName does.
func (l *lowerer) eqFnName(key string) string {
	l.eqFnCount++
	return fmt.Sprintf("lyra_eq_%d_%s", l.eqFnCount, mangleTypeKey(key))
}

// lowerStructuralEquality lowers `a == b` / `a != b` on an aggregate to a call to that
// type's equality glue.
//
// A call rather than an inlined comparison, for the reason at the top of this file: a
// `data` value's equality branches, and a branching call site hands back a merge block
// the pending-temporaries machinery does not handle.
func (l *lowerer) lowerStructuralEquality(block *ir.Block, e *ast.BooleanBinaryOpExpr, left, right value.Value) (value.Value, error) {
	t, ok := l.recordedType(e.Left)
	if !ok {
		return nil, fmt.Errorf("llvm: no type recorded for the left operand of %s", e.Operator)
	}
	fn, err := l.eqFuncFor(t)
	if err != nil {
		return nil, err
	}
	eq := block.NewCall(fn, left, right)
	if e.Operator == ast.BooleanBinaryOpEq {
		return eq, nil
	}
	return block.NewXor(eq, constant.NewInt(lltypes.I1, 1)), nil
}
