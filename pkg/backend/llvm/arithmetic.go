package llvm

import (
	"fmt"
	"math/big"

	"github.com/llir/llvm/ir"
	"github.com/llir/llvm/ir/constant"
	"github.com/llir/llvm/ir/enum"
	lltypes "github.com/llir/llvm/ir/types"
	"github.com/llir/llvm/ir/value"

	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/types"
)

// literalIntType returns the LLVM integer type an integer literal should lower
// at: the concrete width the typechecker recorded for it (via context-directed
// literal-width inference), or i64 when the literal has no resolved context (an
// unannotated binding, or a literal whose value didn't fit the context width so
// the typechecker deliberately left it untyped — see propagateLiteralType).
func (l *lowerer) literalIntType(e ast.Expression) *lltypes.IntType {
	if t, ok := l.recordedType(e); ok {
		if p, ok := t.(types.PrimitiveType); ok {
			if ll, ok := LLVMPrimitive(p.Name); ok {
				if it, ok := ll.(*lltypes.IntType); ok {
					return it
				}
			}
		}
	}
	return lltypes.I64
}

// literalFloatType returns the LLVM float type a float literal should lower at:
// the concrete width the typechecker recorded for it (via context-directed
// literal-width inference), or double (f64) when the literal has no resolved
// context — matching the language's untyped-float default. The integer analogue
// is literalIntType.
func (l *lowerer) literalFloatType(e ast.Expression) *lltypes.FloatType {
	if t, ok := l.recordedType(e); ok {
		if p, ok := t.(types.PrimitiveType); ok {
			if ll, ok := LLVMPrimitive(p.Name); ok {
				if ft, ok := ll.(*lltypes.FloatType); ok {
					return ft
				}
			}
		}
	}
	return lltypes.Double
}

// literalRecordedFloatType reports the float type the typechecker recorded for
// an expression, if it recorded one — how an integer literal that adapted to a
// float context (`let x: f64 = 5`) is recognized at lowering time. Unlike
// literalFloatType there is no default: an int literal with no float context
// lowers as an integer.
func (l *lowerer) literalRecordedFloatType(e ast.Expression) (*lltypes.FloatType, bool) {
	if t, ok := l.recordedType(e); ok {
		if p, ok := t.(types.PrimitiveType); ok {
			if ll, ok := LLVMPrimitive(p.Name); ok {
				if ft, ok := ll.(*lltypes.FloatType); ok {
					return ft, true
				}
			}
		}
	}
	return nil, false
}

// lowerNotBooleanExpr lowers `!x` as `xor i1 x, true`, which is the canonical
// i1 complement — not `icmp eq x, false`, which is the same thing one instruction
// later and gives InstCombine something to undo.
//
// It is deliberately *not* routed through the short-circuit machinery that `&&`
// and `||` use: those exist because their right operand must not be evaluated, and
// `!` has one operand which is always evaluated. So there is no new block here,
// and the caller's insertion point is whatever lowering the operand left behind.
//
// The typechecker has already established the operand is `bool`
// (checkNotBooleanExpr), so a non-i1 here means that guarantee broke and is a loud
// error rather than a silent truncation — backend rule 5.
func (l *lowerer) lowerNotBooleanExpr(block *ir.Block, e *ast.NotBooleanExpr) (value.Value, *ir.Block, error) {
	operand, block, err := l.lowerExpr(block, e.Expression)
	if err != nil {
		return nil, nil, err
	}
	it, ok := operand.Type().(*lltypes.IntType)
	if !ok || it.BitSize != 1 {
		return nil, nil, fmt.Errorf("llvm: `!` expects a bool operand, got %s", operand.Type())
	}
	return block.NewXor(operand, constant.True), block, nil
}

func (l *lowerer) lowerBooleanBinaryOpExpr(block *ir.Block, e *ast.BooleanBinaryOpExpr) (value.Value, *ir.Block, error) {
	left, block, err := l.lowerExpr(block, e.Left)
	if err != nil {
		return nil, nil, err
	}

	switch e.Operator {
	case ast.BooleanBinaryOpAnd:
		return l.lowerBooleanAnd(block, left, e.Right)
	case ast.BooleanBinaryOpOr:
		return l.lowerBooleanOr(block, left, e.Right)
	}

	right, block, err := l.lowerExpr(block, e.Right)
	if err != nil {
		return nil, nil, err
	}
	// A user type ordered through the prelude's `Ord`: the typechecker published the
	// impl (MethodTable.SetOperatorResolution) because `<=>` is its own node and has no
	// call for the ordinary trait path to key on. Checked before the primitive paths,
	// which it can never shadow — dispatchOrdCompare refuses a numeric or rune operand,
	// so `1 < 2` stays an `icmp` and an impl cannot change what the operators mean on
	// the built-in types.
	if res, ok := l.res.MethodTable.OperatorResolution(e); ok {
		return l.lowerOrdComparison(block, e, res, left, right)
	}
	// The operand was a type variable at check time, so the impl could not be named
	// then; this specialization has fixed it. Looked up by the *substituted* operand
	// type — without this an `Eq` impl applied outside a generic and not inside one.
	if lt, ok := l.recordedType(e.Left); ok {
		if res, ok := l.res.MethodTable.OperatorCandidate(e, lt.String()); ok {
			return l.lowerOrdComparison(block, e, res, left, right)
		}
	}
	if _, isFloat := left.Type().(*lltypes.FloatType); isFloat {
		v, err := l.lowerFloatComparison(block, e.Operator, left, right)
		return v, block, err
	}
	if isStringLLVMType(left.Type()) {
		v, err := l.lowerStringComparison(block, e.Operator, left, right)
		return v, block, err
	}
	// Integer comparisons (bool `==`/`!=` included — i1 is an integer type).
	// `icmp` requires both operands to have the same integer type, so a width
	// mismatch is reported explicitly rather than emitting invalid IR clang would
	// reject. With context-directed literal-width inference a literal sibling takes
	// the concrete operand's width (`i8(x) < 3` → both i8), so the width guard is
	// defensive: it fires only when a literal is too large for that width and was
	// left untyped (`i8(x) < 300`), lowering to the i64 default — loud, not
	// miscompiled.
	lt, lok := left.Type().(*lltypes.IntType)
	rt, rok := right.Type().(*lltypes.IntType)
	if !lok || !rok {
		// An aggregate — struct, tuple, `data`, inline array — compares **structurally**,
		// field by field. The typechecker has always accepted this and the backend always
		// refused it, which is hazard 5 inverted; equality.go builds the per-type glue.
		// Only `==`/`!=` are structural: ordering an aggregate needs `Ord`, which the
		// typechecker resolved earlier if it applied.
		if e.Operator == ast.BooleanBinaryOpEq || e.Operator == ast.BooleanBinaryOpNEq {
			v, err := l.lowerStructuralEquality(block, e, left, right)
			return v, block, err
		}
		return nil, nil, fmt.Errorf("llvm: comparison of non-integer operands not implemented (%s, %s)", left.Type(), right.Type())
	}
	if lt.BitSize != rt.BitSize {
		return nil, nil, fmt.Errorf("llvm: comparison of mismatched integer widths not implemented (%s vs %s)", left.Type(), right.Type())
	}
	signed, err := l.getIntSignedness(e.Left)
	if err != nil {
		return nil, nil, err
	}
	// `<=>` yields the prelude's `Ordering`, not an i1, so it leaves before the
	// icmp-predicate table below.
	if e.Operator == ast.BooleanBinaryOpSpaceship {
		v, err := l.lowerSpaceship(block, e, left, right, signed)
		return v, block, err
	}
	var cmpOp enum.IPred
	switch e.Operator {
	case ast.BooleanBinaryOpEq:
		cmpOp = enum.IPredEQ
	case ast.BooleanBinaryOpNEq:
		cmpOp = enum.IPredNE
	case ast.BooleanBinaryOpLT:
		if signed {
			cmpOp = enum.IPredSLT
		} else {
			cmpOp = enum.IPredULT
		}
	case ast.BooleanBinaryOpLTE:
		if signed {
			cmpOp = enum.IPredSLE
		} else {
			cmpOp = enum.IPredULE
		}
	case ast.BooleanBinaryOpGT:
		if signed {
			cmpOp = enum.IPredSGT
		} else {
			cmpOp = enum.IPredUGT
		}
	case ast.BooleanBinaryOpGTE:
		if signed {
			cmpOp = enum.IPredSGE
		} else {
			cmpOp = enum.IPredUGE
		}
	default:
		return nil, nil, fmt.Errorf("llvm: boolean operator %v not implemented", e.Operator)
	}
	return block.NewICmp(cmpOp, left, right), block, nil
}

// lowerFloatComparison lowers a comparison of two already-lowered same-type float
// values to an `fcmp`. Relational ops and `==` use the *ordered* predicates
// (false when either operand is NaN — the C `<`/`==` semantics); `!=` uses the
// *unordered* `une` (true when either is NaN), so `x != x` is true for a NaN, as
// IEEE requires. (The typechecker already warns that float `==`/`!=` is precision-
// sensitive.)
func (l *lowerer) lowerFloatComparison(block *ir.Block, op ast.BooleanBinaryOp, left, right value.Value) (value.Value, error) {
	var pred enum.FPred
	switch op {
	case ast.BooleanBinaryOpEq:
		pred = enum.FPredOEQ
	case ast.BooleanBinaryOpNEq:
		pred = enum.FPredUNE
	case ast.BooleanBinaryOpLT:
		pred = enum.FPredOLT
	case ast.BooleanBinaryOpLTE:
		pred = enum.FPredOLE
	case ast.BooleanBinaryOpGT:
		pred = enum.FPredOGT
	case ast.BooleanBinaryOpGTE:
		pred = enum.FPredOGE
	default:
		return nil, fmt.Errorf("llvm: float comparison operator %v not implemented", op)
	}
	return block.NewFCmp(pred, left, right), nil
}

// lowerBooleanAnd lowers `a && b` with short-circuit semantics: b is evaluated
// only when a is true. It's `if a { b } else { false }` (lowerIf's diamond),
// with one simplification — the `else { false }` branch is *virtual*: it needs
// no block of its own, because there's nothing to compute. The cond-br's false
// edge points straight at the merge block, and the phi supplies the constant
// `false` for that predecessor. So only rhsBlock (where b is evaluated) plus
// merge are created, not two branch blocks.
//
// leftVal is a's already-lowered i1 value; block is where a *finished* (a may
// itself have branched). rightExpr is b's AST node, lowered lazily inside
// rhsBlock so it doesn't run when a is false. The phi's predecessors are `block`
// (a-false edge → false) and rhsEnd (a-true edge → b's value) — rhsEnd, not
// rhsBlock, since b can itself contain an `if` that moves control onward.
func (l *lowerer) lowerBooleanAnd(block *ir.Block, leftVal value.Value, rightExpr ast.Expression) (value.Value, *ir.Block, error) {
	fn := block.Parent
	rhsBlock := fn.NewBlock("")
	mergeBlock := fn.NewBlock("")
	block.NewCondBr(leftVal, rhsBlock, mergeBlock) // a true → eval b; a false → skip to merge
	rightVal, rhsEnd, err := l.lowerExpr(rhsBlock, rightExpr)
	if err != nil {
		return nil, nil, err
	}
	rhsEnd.NewBr(mergeBlock)

	phi := mergeBlock.NewPhi(
		ir.NewIncoming(constant.NewInt(lltypes.I1, 0), block), // a was false ⇒ false
		ir.NewIncoming(rightVal, rhsEnd),                      // a was true  ⇒ b's value
	)
	return phi, mergeBlock, nil
}

// lowerBooleanOr lowers `a || b` with short-circuit semantics: b is evaluated
// only when a is false. The mirror of lowerBooleanAnd — it's
// `if a { true } else { b }`, so the cond-br targets are swapped (a true skips
// straight to merge; a false evaluates b) and the virtual constant branch
// supplies `true`. See lowerBooleanAnd's comment for the block/phi reasoning.
func (l *lowerer) lowerBooleanOr(block *ir.Block, leftVal value.Value, rightExpr ast.Expression) (value.Value, *ir.Block, error) {
	fn := block.Parent
	rhsBlock := fn.NewBlock("")
	mergeBlock := fn.NewBlock("")
	block.NewCondBr(leftVal, mergeBlock, rhsBlock) // a true → skip to merge; a false → eval b
	rightVal, rhsEnd, err := l.lowerExpr(rhsBlock, rightExpr)
	if err != nil {
		return nil, nil, err
	}
	rhsEnd.NewBr(mergeBlock)

	phi := mergeBlock.NewPhi(
		ir.NewIncoming(constant.NewInt(lltypes.I1, 1), block), // a was true  ⇒ true
		ir.NewIncoming(rightVal, rhsEnd),                      // a was false ⇒ b's value
	)
	return phi, mergeBlock, nil
}

func (l *lowerer) lowerMathBinaryOpExpr(block *ir.Block, e *ast.MathBinaryOpExpr) (value.Value, *ir.Block, error) {
	left, block, err := l.lowerExpr(block, e.Left)
	if err != nil {
		return nil, nil, err
	}
	right, block, err := l.lowerExpr(block, e.Right)
	if err != nil {
		return nil, nil, err
	}
	// A user type that gave the operator a meaning: the typechecker resolved the
	// `(_+_)` method and published it, since an operator is its own node and has no
	// call for the ordinary trait path to key on. Checked before the primitive paths,
	// which it can never shadow — dispatch refuses a primitive receiver, so `1 + 1` is
	// a machine add whatever impls exist.
	if res, ok := l.res.MethodTable.OperatorResolution(e); ok {
		return l.lowerOperatorImplCall(block, res, left, right)
	}
	// The operand was a type *variable* at check time — `a + b` inside
	// `let sum<t> where t: Add` — so no single impl could be named then. This
	// specialization has fixed it, and the lookup is by the **substituted** operand type,
	// which is what recordedType hands back inside a specialization. The comparison
	// operators have taken exactly this fallback since `Eq` landed; without it an impl
	// would apply outside a generic and not inside one.
	if res, ok := l.operatorCandidate(e, e.Left); ok {
		return l.lowerOperatorImplCall(block, res, left, right)
	}
	if _, isFloat := left.Type().(*lltypes.FloatType); isFloat {
		v, err := l.applyFloatMathOp(block, e.Operator, left, right)
		return v, block, err
	}
	signed, err := l.getIntSignedness(e.Left)
	if err != nil {
		return nil, nil, err
	}
	return l.applyIntMathOp(block, e.Operator, left, right, signed, e)
}

// applyFloatMathOp emits a binary floating-point arithmetic op on two
// already-lowered same-type float values — the float counterpart to
// applyIntMathOp, shared by lowerMathBinaryOpExpr and lowerMathAssignOp. `%` (Mod)
// is `frem` (LLVM's frem matches C fmod — the result's sign follows the dividend,
// the truncated form, exactly mirroring the integer `%`); `%%` (Remainder,
// floored) applies the sign-of-divisor fixup on top, mirroring the integer `%%`.
func (l *lowerer) applyFloatMathOp(block *ir.Block, op ast.MathBinaryOp, left, right value.Value) (value.Value, error) {
	switch op {
	case ast.MathBinaryOpAdd:
		return block.NewFAdd(left, right), nil
	case ast.MathBinaryOpSub:
		return block.NewFSub(left, right), nil
	case ast.MathBinaryOpMul:
		return block.NewFMul(left, right), nil
	case ast.MathBinaryOpDiv:
		return block.NewFDiv(left, right), nil
	case ast.MathBinaryOpMod:
		return block.NewFRem(left, right), nil
	case ast.MathBinaryOpRemainder:
		return l.lowerFlooredFRem(block, left, right), nil
	default:
		return nil, fmt.Errorf("llvm: float math binary op lowering not implemented for %v", op)
	}
}

// lowerFlooredFRem computes the floored-division remainder of two floats — Odin's
// "remainder (floored)" (%%), sign following the divisor — the float analogue of
// lowerFlooredSRem. Take the truncated remainder (`frem`, sign of the dividend);
// if it's non-zero and its sign disagrees with the divisor's, add the divisor
// back. Built branchlessly with `select`, since it sits inside one expression.
func (l *lowerer) lowerFlooredFRem(block *ir.Block, left, right value.Value) value.Value {
	zero := constant.NewFloat(left.Type().(*lltypes.FloatType), 0)
	r := block.NewFRem(left, right)
	rNeg := block.NewFCmp(enum.FPredOLT, r, zero)
	divisorNeg := block.NewFCmp(enum.FPredOLT, right, zero)
	signsDiffer := block.NewXor(rNeg, divisorNeg)
	nonZero := block.NewFCmp(enum.FPredONE, r, zero)
	needsFixup := block.NewAnd(nonZero, signsDiffer)
	fixed := block.NewFAdd(r, right)
	return block.NewSelect(needsFixup, fixed, r)
}

// applyIntMathOp emits the instruction(s) for a binary integer arithmetic op on
// two already-lowered same-width values. Shared by lowerMathBinaryOpExpr and
// lowerMathAssignOp (`i += x` is `i = i <op> x`). signed selects sdiv/srem vs
// udiv/urem and the floored-remainder fixup; add/sub/mul are signedness-agnostic
// *in their bit pattern* but not in their overflow condition.
//
// `+`/`-`/`*` are **checked** (Pit-of-Success #2): each lowers to the matching
// `llvm.{s,u}{add,sub,mul}.with.overflow` intrinsic and traps on overflow
// (trap.go). Because the check splits the block, this returns the block control
// ends up in — a plain op returns the same block, a checked op returns the
// fall-through (no-overflow) block.
//
// e is the arithmetic expression node, used to consult the value-range analysis's
// SafetyTable (res.RangeSafety): a proven-safe op has its runtime check **elided**
// — `+`/`-`/`*` whose result fits the type become the plain instruction (no
// `with.overflow`+trap, via emitWrappingOp), and a `/`/`%`/`%%` with a proven-
// nonzero divisor / no INT_MIN-÷-1 drops the corresponding trap (emitCheckedDivOp).
// An absent entry (or a nil table) keeps every check, so elision only ever removes
// a provably-dead trap.
func (l *lowerer) applyIntMathOp(block *ir.Block, op ast.MathBinaryOp, left, right value.Value, signed bool, e ast.Expression) (value.Value, *ir.Block, error) {
	switch op {
	case ast.MathBinaryOpAdd:
		if l.res.RangeSafety.NoOverflow(e) {
			return l.emitWrappingOp(block, "add", left, right)
		}
		return l.emitCheckedIntOp(block, "add", left, right, signed)
	case ast.MathBinaryOpSub:
		if l.res.RangeSafety.NoOverflow(e) {
			return l.emitWrappingOp(block, "sub", left, right)
		}
		return l.emitCheckedIntOp(block, "sub", left, right, signed)
	case ast.MathBinaryOpMul:
		if l.res.RangeSafety.NoOverflow(e) {
			return l.emitWrappingOp(block, "mul", left, right)
		}
		return l.emitCheckedIntOp(block, "mul", left, right, signed)
	case ast.MathBinaryOpDiv, ast.MathBinaryOpMod, ast.MathBinaryOpRemainder:
		return l.emitCheckedDivOp(block, op, left, right, signed, e)
	case ast.MathBinaryOpBitAnd:
		return block.NewAnd(left, right), block, nil
	case ast.MathBinaryOpBitOr:
		return block.NewOr(left, right), block, nil
	case ast.MathBinaryOpBitXor:
		return block.NewXor(left, right), block, nil
	case ast.MathBinaryOpShl, ast.MathBinaryOpShr:
		return l.emitShiftOp(block, op, left, right, signed, e)
	default:
		return nil, nil, fmt.Errorf("llvm: math binary op lowering not implemented for %v", op)
	}
}

// emitShiftOp lowers `<<` / `>>` with a checked shift amount.
//
// Three things are going on:
//
//   - **Width.** A shift's count is typed independently of the shifted value
//     (inferMathBinaryExpr), so `u8 << i64` is well-typed while LLVM requires both
//     operands of `shl` to be the same type. The count is coerced to the shifted
//     width here, which is also where an over-wide count would be *truncated* into
//     range — hence the check below runs on the original value, before coercion.
//
//   - **Signedness of `>>`.** A signed operand shifts arithmetically (`ashr`,
//     sign-filling), an unsigned one logically (`lshr`, zero-filling). This is the
//     one place the two spellings of `>>` differ, and getting it backwards is a
//     wrong answer rather than a crash.
//
//   - **The amount check.** `shl`/`lshr`/`ashr` are undefined behavior when the
//     amount is >= the operand width, so an out-of-range count traps rather than
//     producing whatever the target hardware happens to do (x86 masks, ARM
//     saturates — precisely the divergence fixed-width primitives exist to avoid).
//     The comparison is **unsigned**, which catches a negative signed count in the
//     same instruction: as a two's-complement bit pattern, -1 is huge.
//
// The check is **elided** two ways. A compile-time constant already in range needs
// no analysis at all (`constShiftInRange`), which covers the overwhelmingly common
// `x << 3`; a *variable* count drops it when the value-range pass proved the count
// within [0, width) (`NoShiftOverflow`, keyed by e — the same arrangement
// `NoDivZero` uses for the divisor). An absent entry keeps the check, so a count the
// analysis could not bound always still traps.
func (l *lowerer) emitShiftOp(block *ir.Block, op ast.MathBinaryOp, left, right value.Value, signed bool, e ast.Expression) (value.Value, *ir.Block, error) {
	intTy, ok := left.Type().(*lltypes.IntType)
	if !ok {
		return nil, nil, fmt.Errorf("llvm: shift on a non-integer operand (%s)", left.Type())
	}
	width := int64(intTy.BitSize)

	if !constShiftInRange(right, width) && !l.res.RangeSafety.NoShiftOverflow(e) {
		// Compare at the count's own width: coercing first could truncate an
		// out-of-range count into range and hide the very thing being checked.
		countTy, ok := right.Type().(*lltypes.IntType)
		if !ok {
			return nil, nil, fmt.Errorf("llvm: shift amount is not an integer (%s)", right.Type())
		}
		tooWide := block.NewICmp(enum.IPredUGE, right, constant.NewInt(countTy, width))
		block = l.emitTrapIf(block, tooWide, l.panicShiftOverflowFunc())
	}

	amount := coerceIntWidth(block, right, false, intTy)
	if op == ast.MathBinaryOpShl {
		return block.NewShl(left, amount), block, nil
	}
	if signed {
		return block.NewAShr(left, amount), block, nil
	}
	return block.NewLShr(left, amount), block, nil
}

// constShiftInRange reports whether v is a constant shift amount already known to
// be within [0, width) — the case where the trap can be skipped outright. Read as
// unsigned so a negative constant is correctly *not* in range.
func constShiftInRange(v value.Value, width int64) bool {
	c, ok := v.(*constant.Int)
	if !ok {
		return false
	}
	return c.X.Sign() >= 0 && c.X.Cmp(big.NewInt(width)) < 0
}

// lowerBitwiseNotExpr lowers `~x`. LLVM IR has no dedicated complement
// instruction; `xor x, -1` is the standard idiom (an all-ones mask at the
// operand's width, which is what `-1` is as a two's-complement constant), and it
// is what clang emits for C's `~`. Unlike negation there is nothing to trap: every
// bit pattern has a complement, at every width and either signedness.
func (l *lowerer) lowerBitwiseNotExpr(block *ir.Block, e *ast.BitwiseNotExpr) (value.Value, *ir.Block, error) {
	operand, block, err := l.lowerExpr(block, e.Operand)
	if err != nil {
		return nil, nil, err
	}
	if res, ok := l.res.MethodTable.OperatorResolution(e); ok {
		return l.lowerOperatorImplCall(block, res, operand)
	}
	if res, ok := l.operatorCandidate(e, e.Operand); ok {
		return l.lowerOperatorImplCall(block, res, operand)
	}
	intTy, ok := operand.Type().(*lltypes.IntType)
	if !ok {
		return nil, nil, fmt.Errorf("llvm: `~` on a non-integer operand (%s)", operand.Type())
	}
	return block.NewXor(operand, constant.NewInt(intTy, -1)), block, nil
}

// emitCheckedDivOp lowers a checked division-family op (`/`, `%`, `%%`). LLVM's
// div/rem are *undefined behavior* on the two bad inputs, so both are trapped
// before the op runs:
//
//   - divisor == 0 → divide-by-zero trap (any signedness);
//   - signed only: dividend == INT_MIN && divisor == -1 → overflow trap (the one
//     signed division that overflows — the true quotient INT_MAX+1 doesn't fit;
//     `srem` is UB on the same inputs even though the math remainder is 0).
//
// Unsigned division never overflows, so it gets only the zero check. Each check
// splits the block, so this returns the continuation block the actual op runs in.
//
// Either check is **elided** when the value-range analysis proved it unnecessary
// (res.RangeSafety, keyed by e): a provably-nonzero divisor drops the zero check,
// and a divisor/dividend that can't be -1/INT_MIN drops the signed-overflow check.
// An absent entry keeps the check, so a real fault always still traps.
func (l *lowerer) emitCheckedDivOp(block *ir.Block, op ast.MathBinaryOp, left, right value.Value, signed bool, e ast.Expression) (value.Value, *ir.Block, error) {
	intTy, ok := right.Type().(*lltypes.IntType)
	if !ok {
		return nil, nil, fmt.Errorf("llvm: checked division on a non-integer operand (%s)", right.Type())
	}
	if !l.res.RangeSafety.NoDivZero(e) {
		isZero := block.NewICmp(enum.IPredEQ, right, constant.NewInt(intTy, 0))
		block = l.emitTrapIf(block, isZero, l.panicDivideByZeroFunc())
	}

	if signed && !l.res.RangeSafety.NoDivOverflow(e) {
		isMin := block.NewICmp(enum.IPredEQ, left, intMinConst(intTy))
		isNegOne := block.NewICmp(enum.IPredEQ, right, constant.NewInt(intTy, -1))
		overflow := block.NewAnd(isMin, isNegOne)
		block = l.emitTrapIf(block, overflow, l.panicOverflowFunc())
	}

	switch op {
	case ast.MathBinaryOpDiv:
		if signed {
			return block.NewSDiv(left, right), block, nil
		}
		return block.NewUDiv(left, right), block, nil
	case ast.MathBinaryOpMod:
		// Mod (%): Odin's "modulo (truncated)" — sign follows the
		// dividend, exactly what LLVM's srem/urem give natively.
		// 11 % -3 = 2.
		if signed {
			return block.NewSRem(left, right), block, nil
		}
		return block.NewURem(left, right), block, nil
	default: // ast.MathBinaryOpRemainder
		// Remainder (%%): Odin's "remainder (floored)" — sign follows the
		// divisor, distinct from Mod above. 11 %% -3 = -1 (vs Mod's 2).
		// Unsigned floored remainder is identical to truncated (every
		// value is non-negative, so there's nothing to floor), hence
		// urem directly; the signed case needs lowerFlooredSRem's fixup.
		if signed {
			return l.lowerFlooredSRem(block, left, right), block, nil
		}
		return block.NewURem(left, right), block, nil
	}
}

// The compound-assignment → binary-operator mapping lives on the AST type
// (ast.MathAssignOp.BinaryOp), not here. It used to be a table in this file, which
// meant adding an operator required editing two lists that nothing checked against
// each other — and the typechecker needs the same mapping to carry the binary
// operand rules onto a compound assignment (checkMathAssignOp's integers-only
// check). One accessor, so the two cannot drift.

// lowerMathAssignOp lowers a compound assignment (`i += x`) to load / op / store
// against the target's alloca: load the current value, apply the binary op with
// the lowered RHS, store the result back. The target is always a local binding
// (an lvalue name), so its slot is in l.locals. Signedness comes from the RHS,
// whose width the typechecker propagated to match the target (checkMathAssignOp),
// so both operands share a width for the op.
func (l *lowerer) lowerMathAssignOp(block *ir.Block, e *ast.MathAssignOpExpr) (value.Value, *ir.Block, error) {
	slot, ok := l.locals[e.Left.Name]
	if !ok {
		return nil, nil, fmt.Errorf("llvm: compound assignment to unbound identifier %q", e.Left.Name)
	}
	binOp, ok := e.Operator.BinaryOp()
	if !ok {
		return nil, nil, fmt.Errorf("llvm: compound assignment %q not implemented", e.Operator)
	}
	elem, err := slotElemType(slot)
	if err != nil {
		return nil, nil, err
	}
	cur := block.NewLoad(elem, slot)
	rhs, block, err := l.lowerExpr(block, e.Right)
	if err != nil {
		return nil, nil, err
	}
	// An overloaded operator: `a += b` calls the same `(_+_)` impl `a + b` does and
	// stores the answer back. The load/store shape is the compound assignment's own —
	// only the middle step changes.
	res, ok := l.res.MethodTable.OperatorResolution(e)
	if !ok {
		res, ok = l.operatorCandidate(e, &e.Left)
	}
	if ok {
		result, block, err := l.lowerOperatorImplCall(block, res, cur, rhs)
		if err != nil {
			return nil, nil, err
		}
		block.NewStore(result, slot)
		return result, block, nil
	}
	var result value.Value
	if _, isFloat := cur.Type().(*lltypes.FloatType); isFloat {
		result, err = l.applyFloatMathOp(block, binOp, cur, rhs)
	} else {
		var signed bool
		if signed, err = l.getIntSignedness(e.Right); err == nil {
			// The checked int op may split the block (overflow/divide trap); keep
			// lowering (the store) into the block it returns.
			result, block, err = l.applyIntMathOp(block, binOp, cur, rhs, signed, e)
		}
	}
	if err != nil {
		return nil, nil, err
	}
	block.NewStore(result, slot)
	// A compound assignment yields no value (see the typechecker's void result).
	return nil, block, nil
}

func (l *lowerer) lowerNegationExpr(block *ir.Block, e *ast.NegationExpr) (value.Value, *ir.Block, error) {
	operand, block, err := l.lowerExpr(block, e.Operand)
	if err != nil {
		return nil, nil, err
	}
	if res, ok := l.res.MethodTable.OperatorResolution(e); ok {
		return l.lowerOperatorImplCall(block, res, operand)
	}
	if res, ok := l.operatorCandidate(e, e.Operand); ok {
		return l.lowerOperatorImplCall(block, res, operand)
	}
	// Branch on the already-lowered value's own LLVM type rather than a
	// second TypeTable lookup: the typechecker (inferNegationExpr) already
	// rejects a non-numeric or unsigned operand, so by the time a
	// well-typed program reaches here the operand is always a signed int
	// or a float.
	switch t := operand.Type().(type) {
	case *lltypes.IntType:
		// Negation overflows for exactly one value: `-INT_MIN` has no positive
		// counterpart in a signed type (the operand is always signed — the
		// typechecker rejects negating an unsigned value). Trap on it at run time,
		// matching checked arithmetic (todo #2) — but only for a *non-literal*
		// operand. A negated literal is already range-checked by the typechecker
		// (`inferNegationExpr`/`checkIntegerLiteralRange`), and the canonical way to
		// *write* INT_MIN is exactly `-<2^(w-1)>` (e.g. `-9223372036854775808`),
		// which lowers to `sub 0, INT_MIN_bits` == INT_MIN and must not trap.
		if _, isLiteral := e.Operand.(*ast.IntegerLiteralExpr); !isLiteral {
			isMin := block.NewICmp(enum.IPredEQ, operand, intMinConst(t))
			block = l.emitTrapIf(block, isMin, l.panicOverflowFunc())
		}
		// LLVM IR has no dedicated integer negate; `sub 0, x` is the standard
		// idiom (what clang emits for unary minus on an int). Deliberately plain
		// `sub`, not `sub nsw`: the overflow case is handled by the check above.
		return block.NewSub(constant.NewInt(t, 0), operand), block, nil
	case *lltypes.FloatType:
		return block.NewFNeg(operand), block, nil
	default:
		return nil, nil, fmt.Errorf("llvm: negation lowering not implemented for operand type %s", operand.Type())
	}
}

// lowerNumericConversion lowers a Lyra type-conversion call (`i8(x)`,
// `u32(x)`, … — Pit-of-Success #5's one conversion syntax) to the matching
// LLVM conversion instruction: identity (same width), `trunc` (narrowing), or
// `sext`/`zext` (widening, picked from the *source* Lyra type's signedness —
// LLVM's integer types don't carry that themselves; width/kind come from the
// already-lowered argument's own LLVM type).
//
// Covers the conversions the typechecker admits (Pit-of-Success #5): int→int
// (trunc/sext/zext), int→float (sitofp/uitofp, from the source's signedness),
// and float→float *widening* (fpext — narrowing is a typecheck error directing
// to floor/ceil/round). float→int is not a language operation (same rejection),
// so its arm here is defensive only.
func (l *lowerer) lowerNumericConversion(block *ir.Block, call *ast.FunctionCallExpr, targetName types.PrimitiveTypeName) (value.Value, *ir.Block, error) {
	if len(call.Arguments) != 1 {
		return nil, nil, fmt.Errorf("llvm: type conversion %q expects 1 argument, got %d", targetName, len(call.Arguments))
	}
	arg, block, err := l.lowerExpr(block, call.Arguments[0])
	if err != nil {
		return nil, nil, err
	}
	// `u8(panic("…"))`, or `u8(f(panic("…")))` — the operand diverged, so there is
	// nothing to convert and the conversion cannot be reached. See diverged (trap.go).
	if diverged(arg, block) {
		return nil, block, nil
	}
	srcT, ok := l.recordedType(call.Arguments[0])
	if !ok {
		return nil, nil, fmt.Errorf("llvm: type not found for %T", call.Arguments[0])
	}
	srcP, ok := srcT.(types.PrimitiveType)
	if !ok {
		return nil, nil, fmt.Errorf("llvm: type not found for %T", call.Arguments[0])
	}
	dstLL, ok := LLVMPrimitive(targetName)
	if !ok {
		return nil, nil, fmt.Errorf("llvm: no LLVM representation for %q", targetName)
	}
	srcSigned := IsSignedInt(srcP.Name)

	if dstFloat, ok := dstLL.(*lltypes.FloatType); ok {
		switch arg.Type().(type) {
		case *lltypes.FloatType:
			// float→float: only widening reaches here (narrowing is a typecheck error).
			return coerceFloatWidth(block, arg, dstFloat), block, nil
		case *lltypes.IntType:
			// int→float: signed vs unsigned source picks sitofp vs uitofp.
			if srcSigned {
				return block.NewSIToFP(arg, dstFloat), block, nil
			}
			return block.NewUIToFP(arg, dstFloat), block, nil
		}
		return nil, nil, fmt.Errorf("llvm: conversion from %s to %s not implemented", arg.Type(), targetName)
	}

	dst, ok := dstLL.(*lltypes.IntType)
	if !ok {
		return nil, nil, fmt.Errorf("llvm: no integer/float LLVM representation for %q", targetName)
	}
	if _, ok := arg.Type().(*lltypes.IntType); !ok {
		// A float→int target reaches here only if the typechecker's rejection
		// (use floor/ceil/round) were bypassed — defensive, not a real path.
		return nil, nil, fmt.Errorf("llvm: conversion from %s to %s not implemented (float→int is not a conversion; use floor/ceil/round)", arg.Type(), targetName)
	}
	return coerceIntWidth(block, arg, srcSigned, dst), block, nil
}

// coerceIntWidth adjusts v (an integer value) to dst's bit width: unchanged if
// already that width, `trunc` if narrower, or `sext`/`zext` if wider — the
// widening choice comes from srcSigned (v's *source* Lyra type's signedness;
// LLVM integers don't carry that themselves). Shared by lowerNumericConversion
// (an explicit `i8(x)`-style conversion call) and lowerEntry (coercing the
// entry body's value to the declared return width — see its doc comment for
// why that coercion is needed at all).
func coerceIntWidth(block *ir.Block, v value.Value, srcSigned bool, dst *lltypes.IntType) value.Value {
	src := v.Type().(*lltypes.IntType)
	switch {
	case dst.BitSize == src.BitSize:
		return v
	case dst.BitSize < src.BitSize:
		return block.NewTrunc(v, dst)
	case srcSigned:
		return block.NewSExt(v, dst)
	default:
		return block.NewZExt(v, dst)
	}
}

// coerceFloatWidth adapts an already-lowered float value to the destination float
// type (fptrunc to narrow, fpext to widen, identity when equal) — the float
// analogue of coerceIntWidth. In a well-typed program the widths already match
// (the typechecker propagates the target width onto untyped literal leaves and
// rejects implicit float widening otherwise), so this is normally the identity;
// the trunc/ext arms keep a residual mismatch from panicking llir's NewRet.
func coerceFloatWidth(block *ir.Block, v value.Value, dst *lltypes.FloatType) value.Value {
	src := v.Type().(*lltypes.FloatType)
	switch {
	case src.Kind == dst.Kind:
		return v
	case floatKindBits(dst.Kind) < floatKindBits(src.Kind):
		return block.NewFPTrunc(v, dst)
	default:
		return block.NewFPExt(v, dst)
	}
}

// floatKindBits maps the float kinds Lyra lowers (f16/f32/f64) to their bit
// widths, for ordering fptrunc vs fpext. An unexpected kind returns 0.
func floatKindBits(k lltypes.FloatKind) int {
	switch k {
	case lltypes.FloatKindHalf:
		return 16
	case lltypes.FloatKindFloat:
		return 32
	case lltypes.FloatKindDouble:
		return 64
	}
	return 0
}

// lowerFlooredSRem computes the floored-division remainder of two signed
// integers of the same LLVM type — Odin's "remainder (floored)" (%%), sign
// follows the divisor, distinct from the truncated remainder LLVM's srem
// gives natively (sign follows the dividend, which is what Lyra's `%` uses
// directly via a plain block.NewSRem).
//
// The fixup is the standard branchless idiom: take the truncated remainder,
// and if it's non-zero and its sign disagrees with the divisor's, add the
// divisor back (this is exactly what floors it — the same correction
// CPython's `%` applies internally). Built with `select` rather than extra
// basic blocks/branches, since this sits inside a single expression.
func (l *lowerer) lowerFlooredSRem(block *ir.Block, left, right value.Value) value.Value {
	zero := constant.NewInt(left.Type().(*lltypes.IntType), 0)
	r := block.NewSRem(left, right)
	rNeg := block.NewICmp(enum.IPredSLT, r, zero)
	divisorNeg := block.NewICmp(enum.IPredSLT, right, zero)
	signsDiffer := block.NewXor(rNeg, divisorNeg)
	nonZero := block.NewICmp(enum.IPredNE, r, zero)
	needsFixup := block.NewAnd(nonZero, signsDiffer)
	fixed := block.NewAdd(r, right)
	return block.NewSelect(needsFixup, fixed, r)
}

func (l *lowerer) getIntSignedness(e ast.Expression) (bool, error) {
	t, ok := l.recordedType(e)
	if !ok {
		return false, fmt.Errorf("llvm: type not found for %T", e)
	}
	pt, ok := t.(types.PrimitiveType) // assert it's a primitive
	if !ok {
		return false, fmt.Errorf("llvm: type not found for %T", e)
	}
	return IsSignedInt(pt.Name), nil
}

// lowerSpaceship lowers `a <=> b` to the prelude's `Ordering` — `Less`, `Equal` or
// `Greater`.
//
// **Branchless, and that is the reason it is written by hand rather than as three
// buildDataValue calls behind a phi.** Every `Ordering` variant is nullary, so the
// three values differ in exactly one field: the tag. So the tag is computed with
// two `select`s and inserted into an undef union, and the call site never leaves
// the block it was given — no new blocks, no merge, and none of the temp-release
// hazard that a merge block carries (input.go's `lowerReadLineCall` records what
// that cost when `read_line` branched at its call site).
//
// The payload blob is left undef, which is what DATA_LAYOUT.md already specifies
// for a nullary variant ("store the tag; leave the payload undefined") — nothing
// reads a nullary variant's payload, and `match` dispatches on the tag alone.
//
// Tags come from the declaration order in the prelude via findConstructor rather
// than being hard-coded 0/1/2: the tag is a layout fact owned by the type, and
// assuming it here would silently miscompile every three-way match the day someone
// reorders the variants.
func (l *lowerer) lowerSpaceship(block *ir.Block, e *ast.BooleanBinaryOpExpr, left, right value.Value, signed bool) (value.Value, error) {
	recorded, ok := l.recordedType(e)
	if !ok {
		return nil, fmt.Errorf("llvm: `<=>` has no recorded type")
	}
	dt, ok := recorded.(types.DataType)
	if !ok {
		return nil, fmt.Errorf("llvm: `<=>` must produce an Ordering, got %s", recorded)
	}
	lessC, lessTag, hasLess := findConstructor(dt, "Less")
	equalC, equalTag, hasEqual := findConstructor(dt, "Equal")
	greaterC, greaterTag, hasGreater := findConstructor(dt, "Greater")
	if !hasLess || !hasEqual || !hasGreater {
		return nil, fmt.Errorf("llvm: `<=>` result type %q is not an Ordering (needs Less, Equal and Greater)", dt.Name)
	}
	// All three are nullary — a payload would have to be built per variant, and the
	// branchless form could not be used.
	for _, c := range []types.DataTypeConstructor{lessC, equalC, greaterC} {
		if len(c.FieldTypes()) != 0 {
			return nil, fmt.Errorf("llvm: `<=>` result type %q has a non-nullary variant %q", dt.Name, c.Name)
		}
	}

	llType, err := l.lowerType(dt)
	if err != nil {
		return nil, err
	}
	unionTy, ok := llType.(*lltypes.StructType)
	if !ok {
		return nil, fmt.Errorf("llvm: Ordering did not lower to a struct (%s)", llType)
	}
	tagTy, ok := unionTy.Fields[0].(*lltypes.IntType)
	if !ok {
		return nil, fmt.Errorf("llvm: Ordering has a non-integer tag (%s)", unionTy.Fields[0])
	}

	ltPred, gtPred := enum.IPredSLT, enum.IPredSGT
	if !signed {
		ltPred, gtPred = enum.IPredULT, enum.IPredUGT
	}
	isLess := block.NewICmp(ltPred, left, right)
	isGreater := block.NewICmp(gtPred, left, right)
	tag := block.NewSelect(isLess,
		constant.NewInt(tagTy, int64(lessTag)),
		block.NewSelect(isGreater,
			constant.NewInt(tagTy, int64(greaterTag)),
			constant.NewInt(tagTy, int64(equalTag))))
	return block.NewInsertValue(constant.NewUndef(unionTy), tag, 0), nil
}
