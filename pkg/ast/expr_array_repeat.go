package ast

import (
	"fmt"
	"math"
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
	return foldInt(expr, nil, nil)
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
	return foldInt(expr, constInit, nil)
}

func foldInt(expr Expression, constInit func(name string) (Expression, bool), seen map[string]bool) (int64, bool) {
	switch e := expr.(type) {
	case *IntegerLiteralExpr:
		return e.Value, true
	case *IdentifierExpr:
		if constInit == nil {
			return 0, false
		}
		if seen[e.Name] {
			return 0, false // a const defined in terms of itself
		}
		init, ok := constInit(e.Name)
		if !ok {
			return 0, false
		}
		if seen == nil {
			seen = map[string]bool{}
		}
		seen[e.Name] = true
		return foldInt(init, constInit, seen)
	case *NegationExpr:
		if inner, ok := foldInt(e.Operand, constInit, seen); ok {
			return -inner, true
		}
	case *MathBinaryOpExpr:
		left, lok := foldInt(e.Left, constInit, seen)
		right, rok := foldInt(e.Right, constInit, seen)
		if !lok || !rok {
			return 0, false
		}
		switch e.Operator {
		case MathBinaryOpAdd:
			return foldAddInt64(left, right)
		case MathBinaryOpSub:
			return foldSubInt64(left, right)
		case MathBinaryOpMul:
			return foldMulInt64(left, right)
		}
	}
	return 0, false
}

// foldAddInt64 / foldSubInt64 / foldMulInt64 perform the operation in int64, returning
// ok=false if the result overflows the int64 range. Moved here verbatim from the
// typechecker (08/08) rather than rewritten — the multiply's MinInt64 case below is
// exactly the sort of thing a fresh implementation drops.
func foldAddInt64(a, b int64) (int64, bool) {
	sum := a + b
	if (a > 0 && b > 0 && sum < 0) || (a < 0 && b < 0 && sum >= 0) {
		return 0, false
	}
	return sum, true
}

func foldSubInt64(a, b int64) (int64, bool) {
	diff := a - b
	if (b < 0 && diff < a) || (b > 0 && diff > a) {
		return 0, false
	}
	return diff, true
}

func foldMulInt64(a, b int64) (int64, bool) {
	if a == 0 || b == 0 {
		return 0, true
	}
	// MinInt64 * -1 overflows, but the product/b check below misses it: the
	// wrapped product is MinInt64 and MinInt64 / -1 == MinInt64 (two's-complement,
	// no panic in Go), so product/b == a would wrongly report no overflow.
	if (a == math.MinInt64 && b == -1) || (b == math.MinInt64 && a == -1) {
		return 0, false
	}
	product := a * b
	if product/b != a {
		return 0, false
	}
	return product, true
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
