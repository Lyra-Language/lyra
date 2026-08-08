package ast

import (
	"fmt"
	"math/big"
)

type ArrayRepeatExpr struct {
	ExprBase
	Value Expression // The value to repeat
	Count Expression // The count (compile-time constant)
}

func (a *ArrayRepeatExpr) exprNode() {}

func (a *ArrayRepeatExpr) GetName() string {
	return fmt.Sprintf("[%s; %s]", a.Value.GetName(), a.Count.GetName())
}

// FoldIntExpr folds expr to a compile-time integer, handling a literal, a negation,
// and `+`/`-`/`*` over two folded operands. Overflow of int64 answers false rather than
// a wrapped value.
//
// It lives here, in the AST, because **two passes need the same answer**: the
// typechecker computes an array-repeat's size (and reports when it cannot), and the
// backend emits that many elements. A second copy of the arithmetic is the kind of
// divergence that shows up as a program whose array is one length to the checker and
// another to codegen.
func FoldIntExpr(expr Expression) (int64, bool) {
	big, ok := FoldBigExpr(expr, nil)
	if !ok || !big.IsInt64() {
		return 0, false
	}
	return big.Int64(), true
}

// FoldBigExpr folds expr at **arbitrary precision**, which is what a 128-bit constant
// needs: `10^20 + 1` has no int64 to fold through, so the int64 walk declined and
// `let d: u8 = 10^20 + 1` reached the backend unchecked — as invalid IR, since the
// operand had been narrowed to a width it does not fit.
//
// Folding wide and *narrowing at the end* is the arrangement that keeps both callers
// honest: FoldIntExpr above answers only when the result genuinely fits an int64, so a
// consumer that cannot handle more still gets ok=false rather than a wrapped value,
// while the range check gets the true magnitude to report.
//
// `constInit` resolves a `const` reference, as in FoldIntExprWith; nil declines names.
func FoldBigExpr(expr Expression, constInit func(name string) (Expression, bool)) (*big.Int, bool) {
	return foldBig(expr, constInit, nil)
}

// foldBigLimit caps an intermediate's magnitude at 2^512. Nothing a program can write
// approaches it — literals are at most 128 bits and folding is `+ - *` over a handful of
// them — so it is a guard against a pathological chain turning a compile into an
// arbitrary-precision workout, not a semantic bound. A value beyond it is refused rather
// than folded, which loses only the diagnostic, never correctness.
var foldBigLimit = new(big.Int).Lsh(big.NewInt(1), 512)

func foldBig(expr Expression, constInit func(name string) (Expression, bool), seen map[string]bool) (*big.Int, bool) {
	switch e := expr.(type) {
	case *IntegerLiteralExpr:
		return e.BigValue(), true
	case *IdentifierExpr:
		if constInit == nil {
			return nil, false
		}
		if seen[e.Name] {
			return nil, false // a const defined in terms of itself
		}
		init, ok := constInit(e.Name)
		if !ok {
			return nil, false
		}
		if seen == nil {
			seen = map[string]bool{}
		}
		seen[e.Name] = true
		return foldBig(init, constInit, seen)
	case *NegationExpr:
		if inner, ok := foldBig(e.Operand, constInit, seen); ok {
			return new(big.Int).Neg(inner), true
		}
	case *MathBinaryOpExpr:
		left, lok := foldBig(e.Left, constInit, seen)
		right, rok := foldBig(e.Right, constInit, seen)
		if !lok || !rok {
			return nil, false
		}
		var out *big.Int
		switch e.Operator {
		case MathBinaryOpAdd:
			out = new(big.Int).Add(left, right)
		case MathBinaryOpSub:
			out = new(big.Int).Sub(left, right)
		case MathBinaryOpMul:
			out = new(big.Int).Mul(left, right)
		default:
			// Division and the remainders are not folded, for the reason the int64 walk
			// gave: they cannot increase magnitude, so they cannot cause the overflow
			// this is looking for, and Lyra's two remainder operators should not be
			// guessed at here.
			return nil, false
		}
		if new(big.Int).Abs(out).Cmp(foldBigLimit) > 0 {
			return nil, false
		}
		return out, true
	}
	return nil, false
}

// FoldIntExprWith is FoldIntExpr with **constant references resolved**: `constInit` maps
// a name to that `const`'s initializer, so `const N = 3` / `const M = N * 2` folds to 6.
// A const chain that refers to itself terminates rather than recurring, answering false.
//
// The two entry points are separate because their callers want different things. The
// overflow checks fold a *literal expression* and must not start resolving names — a
// binding's value is not a literal, and treating it as one would change which literals
// they report. A repeat count is the opposite: the grammar admits a `const_identifier`
// precisely so a size can be named.
func FoldIntExprWith(expr Expression, constInit func(name string) (Expression, bool)) (int64, bool) {
	big, ok := FoldBigExpr(expr, constInit)
	if !ok || !big.IsInt64() {
		return 0, false
	}
	return big.Int64(), true
}

// ArrayRepeatCount folds a repeat literal's count. The grammar admits a number literal
// or a `const_identifier`, so `constInit` resolves the latter to the const's own
// initializer — supplied by the caller, since only it has a symbol table.
//
// `ok=false` means "not a compile-time integer"; the *reporting* is the typechecker's,
// which is why nothing is said here.
func ArrayRepeatCount(count Expression, constInit func(name string) (Expression, bool)) (int64, bool) {
	if count == nil {
		return 0, false
	}
	return FoldIntExprWith(count, constInit)
}
