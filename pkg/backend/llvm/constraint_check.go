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

// Run-time enforcement of a newtype's `where` constraints (08/13).
//
// A constraint used to be a compile-time assertion and nothing else: it caught a
// literal, and whatever the value-range pass could pin to an interval, and silently
// accepted the rest. So on `newtype Percent = u8 where range(0..<=100)`,
//
//	let mk = (n: u8) -> Percent => Percent(n)
//	mk(200)      // built, ran, printed 200
//
// which leaves the language's own ladder — provable → compile error, otherwise →
// trap — with a first rung and no second, in the one construct whose entire purpose
// is to be checked. Worse, the values that reach a constrained newtype at run time
// are exactly the ones from outside the program, where a range or unit mistake
// actually lives.
//
// **Which sites get a check is the typechecker's answer, not this file's**
// (typetable.ConstraintTable): only it knows which values it verified statically, so
// a foldable constant is decided there and never reaches here. This file is the
// emission half — given a value and the newtype it is entering, produce the compare
// and the branch to `lyra_panic_constraint`.
//
// The cost is one branch per construction from a non-constant value, which is the
// same thing arithmetic already pays for overflow, and the optimizer removes it
// wherever the range is provable by other means.

// emitConstraintChecks lowers every runtime-checkable constraint ct declares against
// the already-lowered val, returning the block lowering continues in. The checks are
// emitted in declaration order and each is an independent trap, so the first
// violated one stops the program.
func (l *lowerer) emitConstraintChecks(block *ir.Block, val value.Value, ct *types.ConstrainedType) (*ir.Block, error) {
	base, ok := l.stripNewtype(ct).(types.PrimitiveType)
	if !ok {
		return block, nil
	}
	signed := IsSignedInt(base.Name)
	isFloat := isFloatPrimitive(base.Name)
	if !isFloat && !isAnyConcreteIntName(base.Name) {
		return block, nil
	}

	for _, c := range ct.Constraints {
		var err error
		switch con := c.(type) {
		case *types.RangeConstraint:
			block, err = l.emitRangeCheck(block, val, con, signed, isFloat)
		case *types.LiteralUnionConstraint:
			block, err = l.emitValuesCheck(block, val, con, isFloat)
		case *types.StepConstraint:
			block, err = l.emitStepCheck(block, val, ct, con, signed, isFloat)
		}
		if err != nil {
			return nil, err
		}
	}
	return block, nil
}

// emitRangeCheck traps when val falls outside `range(start..end)`. Each bound is
// independent — an open bound (`0..`, `..<=100`) simply emits no test for that side
// — and the end's comparison follows the range's own comparator, so `..<` excludes
// its bound and `..<=` includes it.
//
// The integer comparison is **signedness-correct**: an unsigned base uses ULT/UGT, so
// a `u8` never compares as if it could be negative. That matters here more than
// usual, since a constraint's whole job is to be exact about a boundary.
func (l *lowerer) emitRangeCheck(block *ir.Block, val value.Value, rc *types.RangeConstraint, signed, isFloat bool) (*ir.Block, error) {
	if rc.Start != nil {
		if cond, ok, err := l.constraintCompare(block, val, rc.Start, "below", signed, isFloat, false); err != nil {
			return nil, err
		} else if ok {
			block = l.emitTrapIf(block, cond, l.panicConstraintFunc())
		}
	}
	if rc.End != nil {
		exclusive := rc.Comparator == "<"
		if cond, ok, err := l.constraintCompare(block, val, rc.End, "above", signed, isFloat, exclusive); err != nil {
			return nil, err
		} else if ok {
			block = l.emitTrapIf(block, cond, l.panicConstraintFunc())
		}
	}
	return block, nil
}

// constraintCompare builds the "is val on the wrong side of this bound" test. It
// answers ok=false for a bound that does not fold to a constant — a named constant
// or an expression the constraint machinery cannot evaluate — leaving that side
// unenforced rather than guessed at, which is the same conservatism the compile-time
// checks apply to an unfoldable bound.
func (l *lowerer) constraintCompare(block *ir.Block, val value.Value, bound types.MathConstraintExpr,
	side string, signed, isFloat, exclusive bool) (value.Value, bool, error) {
	if isFloat {
		f, ok := foldConstraintFloatValue(bound)
		if !ok {
			return nil, false, nil
		}
		ft, isFT := val.Type().(*lltypes.FloatType)
		if !isFT {
			return nil, false, fmt.Errorf("llvm: float constraint on a non-float value (%s)", val.Type())
		}
		c := floatConst(ft, f)
		if side == "below" {
			return block.NewFCmp(enum.FPredOLT, val, c), true, nil
		}
		if exclusive {
			return block.NewFCmp(enum.FPredOGE, val, c), true, nil
		}
		return block.NewFCmp(enum.FPredOGT, val, c), true, nil
	}

	n, ok := foldConstraintIntValue(bound)
	if !ok {
		return nil, false, nil
	}
	it, isIT := val.Type().(*lltypes.IntType)
	if !isIT {
		return nil, false, fmt.Errorf("llvm: integer constraint on a non-integer value (%s)", val.Type())
	}
	c := constant.NewInt(it, n)
	if side == "below" {
		if signed {
			return block.NewICmp(enum.IPredSLT, val, c), true, nil
		}
		return block.NewICmp(enum.IPredULT, val, c), true, nil
	}
	switch {
	case exclusive && signed:
		return block.NewICmp(enum.IPredSGE, val, c), true, nil
	case exclusive:
		return block.NewICmp(enum.IPredUGE, val, c), true, nil
	case signed:
		return block.NewICmp(enum.IPredSGT, val, c), true, nil
	default:
		return block.NewICmp(enum.IPredUGT, val, c), true, nil
	}
}

// emitValuesCheck traps when val equals none of a `values(...)` set. The test is
// built as a conjunction of "differs from this one" so a single trap covers the
// whole set, which keeps the emitted shape flat rather than a chain of blocks.
func (l *lowerer) emitValuesCheck(block *ir.Block, val value.Value, lu *types.LiteralUnionConstraint, isFloat bool) (*ir.Block, error) {
	var differs value.Value
	for _, av := range lu.Values {
		expr, isExpr := av.(ast.Expression)
		if !isExpr {
			continue
		}
		var ne value.Value
		if isFloat {
			f, ok := literalFloatOf(expr)
			if !ok {
				continue
			}
			ft, isFT := val.Type().(*lltypes.FloatType)
			if !isFT {
				return nil, fmt.Errorf("llvm: float `values` constraint on a non-float value (%s)", val.Type())
			}
			// Unordered so a NaN counts as differing from every member, which is what
			// makes a NaN fail a `values(...)` constraint rather than pass it.
			ne = block.NewFCmp(enum.FPredUNE, val, floatConst(ft, f))
		} else {
			n, ok := literalIntOf(expr)
			if !ok {
				continue
			}
			it, isIT := val.Type().(*lltypes.IntType)
			if !isIT {
				return nil, fmt.Errorf("llvm: integer `values` constraint on a non-integer value (%s)", val.Type())
			}
			ne = block.NewICmp(enum.IPredNE, val, constant.NewInt(it, n))
		}
		if differs == nil {
			differs = ne
		} else {
			differs = block.NewAnd(differs, ne)
		}
	}
	if differs == nil {
		return block, nil // no foldable members; nothing to enforce
	}
	return l.emitTrapIf(block, differs, l.panicConstraintFunc()), nil
}

// emitStepCheck traps when val is off the grid a `step(...)` describes — the values
// covered are `start, start+step, start+2*step, …` (types/step.go fixes that meaning
// for both spellings), so the test is `(val - start) % step != 0` with `start` from
// the newtype's `range(...)` when it has one and 0 otherwise.
//
// The remainder is `srem`/`urem` by signedness for integers and `fmod` for floats.
// A float step that the base cannot hold exactly (`step(0.1)`) will reject values
// that look like multiples of it; that is what the constraint says rather than a
// defect here, and it matches the compile-time check exactly, which is the property
// that matters — the two rungs must agree.
func (l *lowerer) emitStepCheck(block *ir.Block, val value.Value, ct *types.ConstrainedType,
	sc *types.StepConstraint, signed, isFloat bool) (*ir.Block, error) {
	if isFloat {
		step, ok := foldConstraintFloatValue(sc.Value)
		if !ok || step == 0 {
			return block, nil
		}
		ft, isFT := val.Type().(*lltypes.FloatType)
		if !isFT {
			return nil, fmt.Errorf("llvm: float step constraint on a non-float value (%s)", val.Type())
		}
		origin := constraintRangeOriginFloat(ct)
		shifted := value.Value(val)
		if origin != 0 {
			shifted = block.NewFSub(val, floatConst(ft, origin))
		}
		// libc fmod takes and returns double; widen and narrow around it, which is
		// exact for every f16/f32 (both are subsets of double).
		wide := coerceFloatWidth(block, shifted, lltypes.Double)
		rem := block.NewCall(l.fmodFunc(), wide, constant.NewFloat(lltypes.Double, step))
		off := block.NewFCmp(enum.FPredUNE, rem, constant.NewFloat(lltypes.Double, 0))
		return l.emitTrapIf(block, off, l.panicConstraintFunc()), nil
	}

	step, ok := foldConstraintIntValue(sc.Value)
	if !ok || step == 0 {
		return block, nil
	}
	it, isIT := val.Type().(*lltypes.IntType)
	if !isIT {
		return nil, fmt.Errorf("llvm: integer step constraint on a non-integer value (%s)", val.Type())
	}
	origin := constraintRangeOriginInt(ct)
	shifted := value.Value(val)
	if origin != 0 {
		shifted = block.NewSub(val, constant.NewInt(it, origin))
	}
	var rem value.Value
	if signed {
		rem = block.NewSRem(shifted, constant.NewInt(it, step))
	} else {
		rem = block.NewURem(shifted, constant.NewInt(it, step))
	}
	off := block.NewICmp(enum.IPredNE, rem, constant.NewInt(it, 0))
	return l.emitTrapIf(block, off, l.panicConstraintFunc()), nil
}

// fmodFunc lazily declares libc's `double @fmod(double, double)`, the float
// remainder the step check needs.
func (l *lowerer) fmodFunc() *ir.Func {
	if l.fmod == nil {
		l.fmod = l.module.NewFunc("fmod", lltypes.Double,
			ir.NewParam("", lltypes.Double), ir.NewParam("", lltypes.Double))
	}
	return l.fmod
}

// constraintRangeOriginInt / Float give the value a step grid is measured from: the
// start of the newtype's range when it declares one with a foldable start, else 0.
// They mirror the typechecker's constraintGridOrigin — the two must agree, or a
// value would pass one rung and fail the other.
func constraintRangeOriginInt(ct *types.ConstrainedType) int64 {
	for _, c := range ct.Constraints {
		if rc, ok := c.(*types.RangeConstraint); ok && rc.Start != nil {
			if n, ok := foldConstraintIntValue(rc.Start); ok {
				return n
			}
		}
	}
	return 0
}

func constraintRangeOriginFloat(ct *types.ConstrainedType) float64 {
	for _, c := range ct.Constraints {
		if rc, ok := c.(*types.RangeConstraint); ok && rc.Start != nil {
			if f, ok := foldConstraintFloatValue(rc.Start); ok {
				return f
			}
		}
	}
	return 0
}

// foldConstraintIntValue / foldConstraintFloatValue read a constraint bound's
// constant value. They are the backend's copy of the typechecker's folding for the
// same expressions; both only ever see the literal and negated-literal forms the
// grammar admits in a constraint.
func foldConstraintIntValue(e types.MathConstraintExpr) (int64, bool) {
	lit, ok := e.(*types.MathConstraintLiteralExpr)
	if !ok || lit.Value == nil {
		return 0, false
	}
	return lit.Value.Int64()
}

func foldConstraintFloatValue(e types.MathConstraintExpr) (float64, bool) {
	lit, ok := e.(*types.MathConstraintLiteralExpr)
	if !ok || lit.Value == nil {
		return 0, false
	}
	if f, ok := lit.Value.Float64(); ok {
		return f, true
	}
	if n, ok := lit.Value.Int64(); ok {
		return float64(n), true
	}
	return 0, false
}

// literalIntOf / literalFloatOf read a `values(...)` member's constant.
func literalIntOf(e ast.Expression) (int64, bool) {
	switch v := e.(type) {
	case *ast.IntegerLiteralExpr:
		return v.Value, true
	case *ast.NegationExpr:
		if n, ok := literalIntOf(v.Operand); ok {
			return -n, true
		}
	}
	return 0, false
}

func literalFloatOf(e ast.Expression) (float64, bool) {
	switch v := e.(type) {
	case *ast.FloatLiteralExpr:
		return v.Value, true
	case *ast.IntegerLiteralExpr:
		return float64(v.Value), true
	case *ast.NegationExpr:
		if f, ok := literalFloatOf(v.Operand); ok {
			return -f, true
		}
	}
	return 0, false
}

// isFloatPrimitive / isAnyConcreteIntName are the backend's local spellings of the
// two width questions the constraint emission asks.
func isFloatPrimitive(n types.PrimitiveTypeName) bool {
	switch n {
	case types.Float16, types.Float32, types.Float64:
		return true
	}
	return false
}

func isAnyConcreteIntName(n types.PrimitiveTypeName) bool {
	switch n {
	case types.Int8, types.Int16, types.Int32, types.Int64, types.Int128,
		types.UInt8, types.UInt16, types.UInt32, types.UInt64, types.UInt128:
		return true
	}
	return false
}
