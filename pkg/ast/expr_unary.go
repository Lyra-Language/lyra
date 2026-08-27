package ast

import "fmt"

type AwaitExpr struct {
	ExprBase
	Operand Expression
}

func (e *AwaitExpr) GetName() string {
	return "await " + e.Operand.GetName()
}

type NegationExpr struct {
	ExprBase
	Operand Expression
}

func (e *NegationExpr) GetName() string { return "negation_expr" }

// BitwiseNotExpr is `~x`, the bitwise complement. It is a separate node from
// NegationExpr rather than a flag on it because the two have different operand
// rules — `-` accepts floats, `~` is integers only — and different lowerings.
type BitwiseNotExpr struct {
	ExprBase
	Operand Expression
}

func (e *BitwiseNotExpr) GetName() string { return "bitwise_not_expr" }

type AddressOfExpr struct {
	ExprBase
	Operand Expression
	IsMut   bool
}

func (e *AddressOfExpr) GetName() string { return "address_of_expr" }

type DerefExpr struct {
	ExprBase
	Operand Expression
}

func (e *DerefExpr) GetName() string { return "deref_expr" }

// SpreadExpr is `...xs` inside an array literal — the elements of `xs` spliced in where
// it stands.
//
// **The operand is an expression, not a name.** It held a bare `string` until 08/27, which
// made `[...f(x), 1]` and `[...a.b, 1]` unrepresentable rather than unimplemented: no pass
// could have accepted them however it was written, and a `string` field is invisible to
// every walk (hazard 8), so nothing pointed at the hole either.
type SpreadExpr struct {
	ExprBase
	Value Expression
}

func (e *SpreadExpr) GetName() string { return "..." + exprName(e.Value) }

func (e *SpreadExpr) String() string { return "..." + exprName(e.Value) }

// exprName renders a spread's operand for a diagnostic. A Named operand gives its own
// name; anything else falls back to its String(), which every expression has.
func exprName(e Expression) string {
	if e == nil {
		return ""
	}
	if n, ok := e.(Named); ok {
		return n.GetName()
	}
	if s, ok := e.(fmt.Stringer); ok {
		return s.String()
	}
	return "expression"
}
