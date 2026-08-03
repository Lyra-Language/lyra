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

// This file lowers the `?` (try) postfix operator — error propagation.
//
// `x?` is a `match` in disguise, and lowers to exactly that shape:
//
//	match x {
//	  Ok(v)  => v,                 // the value of the whole expression
//	  Err(e) => return Err(e),     // rebuilt at the *enclosing* return type
//	}
//
// with `Some`/`None` in place of `Ok`/`Err` for a Maybe. The one thing that is not
// a plain match is the error arm: the propagated value is a **different LLVM type**
// from the operand. `?` on a `Result<i64, string>` inside a `-> Result<bool, string>`
// function cannot forward the operand's union — those are two distinct
// monomorphizations — so the error payload is extracted and a fresh `Err` of the
// enclosing Result is built around it.

// tryShape is the canonical Result/Maybe structure `?` needs from a data type:
// which variant carries the unwrapped value, and which one is propagated.
type tryShape struct {
	kind       string // "Result" or "Maybe"
	success    types.DataTypeConstructor
	successTag int
	failure    types.DataTypeConstructor
	failureTag int
}

// canonicalTryShape recognizes dt as a canonical Result or Maybe and returns its
// success/failure variants with their tags.
//
// It reads the shape by **constructor name**, which is not a guess:
// collector/canonical.go's canonicalShape pins a canonical Result to exactly
// `Ok(_) | Err(_)` and a Maybe to exactly `Some(_) | None`, and refuses the
// `@builtin` marker to any declaration that does not match. The *type* name is
// deliberately free (the marker confers the identity, not the spelling) — which is
// exactly why this reads the variants rather than the name, and why it does not add
// a third copy of the canonical-kind name resolution that the collector stamps and
// the checker/typechecker read.
//
// A dt that is neither shape is a loud error rather than a fallback: the front end
// has already established that both the operand (typechecker's inferTryExpr) and
// the enclosing return (checker's CheckTryOutsideResult) are canonical, so reaching
// here means that guarantee broke.
func canonicalTryShape(dt types.DataType) (tryShape, error) {
	if len(dt.Constructors) == 2 {
		if okC, okTag, hasOk := findConstructor(dt, "Ok"); hasOk {
			if errC, errTag, hasErr := findConstructor(dt, "Err"); hasErr {
				return tryShape{"Result", okC, okTag, errC, errTag}, nil
			}
		}
		if someC, someTag, hasSome := findConstructor(dt, "Some"); hasSome {
			if noneC, noneTag, hasNone := findConstructor(dt, "None"); hasNone {
				return tryShape{"Maybe", someC, someTag, noneC, noneTag}, nil
			}
		}
	}
	return tryShape{}, fmt.Errorf(
		"llvm: `?` needs a canonical Result (Ok/Err) or Maybe (Some/None), got %q", dt.Name)
}

// lowerTryExpr lowers `operand?`: test the operand's tag, continue with the
// unwrapped payload on success, and on failure return the error rebuilt at the
// enclosing function's return type.
//
// The expression's value is the success payload and control continues in the
// success block, so — like `if` and `match` — this returns a block different from
// the one it was handed, and the caller keeps lowering into that one.
func (l *lowerer) lowerTryExpr(block *ir.Block, e *ast.TryExpr) (value.Value, *ir.Block, error) {
	// Where a propagated error is rebuilt. checker.CheckTryOutsideResult has already
	// rejected a `?` in a function that is not Result/Maybe-returning, so a failure
	// on this path is a broken front-end guarantee, not user error.
	if l.retLyra == nil {
		return nil, nil, fmt.Errorf("llvm: `?` in a function with no declared return type")
	}
	retT := l.applyTypeSubst(l.retLyra)
	retDt, ok := l.resolveDataType(retT)
	if !ok {
		return nil, nil, fmt.Errorf("llvm: `?` in a function returning %s, which is not a Result or Maybe", retT)
	}
	retShape, err := canonicalTryShape(retDt)
	if err != nil {
		return nil, nil, err
	}

	opT, ok := l.recordedType(e.Operand)
	if !ok {
		return nil, nil, fmt.Errorf("llvm: no type recorded for the operand of `?`")
	}
	opDt, ok := l.resolveDataType(opT)
	if !ok {
		return nil, nil, fmt.Errorf("llvm: `?` operand is not a data type (%s)", opT)
	}
	opShape, err := canonicalTryShape(opDt)
	if err != nil {
		return nil, nil, err
	}
	// Same-kind propagation only; the typechecker enforces it (a Maybe cannot be
	// propagated out of a Result-returning function without an explicit conversion).
	if opShape.kind != retShape.kind {
		return nil, nil, fmt.Errorf(
			"llvm: cannot propagate a %s with `?` from a %s-returning function",
			opShape.kind, retShape.kind)
	}

	// The operand is a **borrow** for the duration of the test, exactly as a match
	// scrutinee is (ownership.go's TryExpr case), so what comes back may be an owned
	// temporary that the enclosing statement releases. A `shared` Result arrives as a
	// box pointer; unbox it so the tag and payload reads below see a first-class union.
	whole, block, err := l.lowerExpr(block, e.Operand)
	if err != nil {
		return nil, nil, err
	}
	scrut, err := l.unboxSharedData(block, whole)
	if err != nil {
		return nil, nil, err
	}
	unionTy, ok := scrut.Type().(*lltypes.StructType)
	if !ok {
		return nil, nil, fmt.Errorf("llvm: `?` operand did not lower to a struct (%s)", scrut.Type())
	}
	tagTy, ok := unionTy.Fields[0].(*lltypes.IntType)
	if !ok {
		return nil, nil, fmt.Errorf("llvm: `?` operand union has a non-integer tag (%s)", unionTy.Fields[0])
	}

	fn := block.Parent
	okBlock := fn.NewBlock("")
	errBlock := fn.NewBlock("")
	// Two variants, so a compare-and-branch says it more directly than a switch; the
	// tag is field 0 of the first-class union (DATA_LAYOUT.md).
	isSuccess := block.NewICmp(enum.IPredEQ,
		block.NewExtractValue(scrut, 0), constant.NewInt(tagTy, int64(opShape.successTag)))
	block.NewCondBr(isSuccess, okBlock, errBlock)

	// How the operand's *own* reference is disposed of on the failure path decides
	// whether the error payload needs a fresh +1 — see lowerTryPropagate.
	transferring := l.needsDrop(opT) && l.ownership().ShouldReleaseTemp(e.Operand)
	// `block` is where the tag test was emitted, so it is errBlock's predecessor and
	// dominates it — which is what lets the propagating path release the temporaries
	// produced there. `whole` is the operand's own value: when it is an owned
	// temporary its reference transfers into the rebuilt error, so it is the one
	// value that must *not* be released on that path.
	var transferred value.Value
	if transferring {
		transferred = whole
	}
	if err := l.lowerTryPropagate(errBlock, block, transferred, scrut, opShape, retDt, retShape, transferring); err != nil {
		return nil, nil, err
	}

	// Success: the expression's value is the payload, read out of the operand. Any
	// +1 it owes (the operand still owns its copy) is the Retain the ownership pass
	// recorded on this node, which lowerExpr's wrapper applies to the value returned
	// here — the same duplicate-never-move rule a match arm's binding follows.
	payload, err := l.extractDataPayload(okBlock, scrut, opShape.success)
	if err != nil {
		return nil, nil, err
	}
	return okBlock.NewExtractValue(payload, 0), okBlock, nil
}

// lowerTryPropagate emits the failure arm into block: rebuild the operand's error as
// a value of the enclosing function's return type and return it, sealing the block.
//
// For a Result the error payload is carried across; for a Maybe, `None` is nullary,
// so there is nothing to carry and nothing to retain.
//
// transferring says the operand is an owned temporary whose release this path
// suppresses (see the pendingBase note below), so its reference *moves* into the
// propagated error. That is the same transfer-vs-duplicate distinction the ownership
// pass draws everywhere else, and getting it wrong is a refcount bug in either
// direction: retaining a transferred reference leaks it, and moving a borrowed one
// hands the caller a payload the operand's owner will free.
func (l *lowerer) lowerTryPropagate(block, preBranch *ir.Block, transferred, scrut value.Value, opShape tryShape, retDt types.DataType, retShape tryShape, transferring bool) error {
	var fields []value.Value
	if fieldTypes := opShape.failure.FieldTypes(); len(fieldTypes) > 0 {
		payload, err := l.extractDataPayload(block, scrut, opShape.failure)
		if err != nil {
			return err
		}
		errVal := block.NewExtractValue(payload, 0)
		// A *borrowed* operand (a binding, a field) still owns its copy and will release
		// it — at scope exit, or via the releaseAllManagedFrames that emitReturn runs
		// just below — so the propagated error needs a reference of its own. This is the
		// same duplicate-never-move rule a match arm's binding follows (see the ownership
		// package doc). An owned temporary is the other case: nothing releases it on this
		// path, so its reference is what the propagated error carries, and a dup here
		// would be a leak rather than a fix.
		if !transferring {
			if err := l.deepRetain(block, errVal, fieldTypes[0]); err != nil {
				return err
			}
		}
		fields = append(fields, errVal)
	}

	propagated, err := l.buildDataValue(block, retDt, retShape.failureTag, retShape.failure, fields)
	if err != nil {
		return err
	}
	// A `shared`-flavored return type is a pointer to a ref-counted box.
	if types.AllocationOf(retDt) == types.Shared {
		stackDt, ok := types.WithAllocation(retDt, types.Stack).(types.DataType)
		if !ok {
			return fmt.Errorf("llvm: data type %q did not survive allocation stripping", retDt.Name)
		}
		if propagated, err = l.lowerBoxShared(block, propagated, stackDt); err != nil {
			return err
		}
	}

	// Release the temporaries that die on this path, here, before the return.
	//
	// They cannot be left to emitReturn: its flush releases each pending temporary in
	// *the block that produced it*, and the operand's temporaries were produced before
	// the branch above. A release emitted there would sit ahead of that block's own
	// terminator and so ahead of the tag test that reads them — an early free on the
	// **success** path too, not just this one. So this path releases them itself, into
	// its own block, and then holds them back from the return's flush.
	//
	// releaseTempsOnExit deliberately does not truncate the pending list: the success
	// path still reaches the enclosing statement's flush and must still release them
	// there. Each path releases exactly once.
	//
	// The operand's own temporary is the exception, passed as `transferred` — its
	// reference moves into the rebuilt error above (see `transferring`), so releasing
	// it here would hand the caller a payload that has already been freed.
	if err := l.releaseTempsOnExit(block, preBranch, transferred); err != nil {
		return err
	}
	savedBase := l.pendingBase
	l.pendingBase = len(l.pendingReleases)
	defer func() { l.pendingBase = savedBase }()

	return l.emitReturn(block, block, propagated)
}
