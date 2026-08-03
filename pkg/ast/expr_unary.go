package ast

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

type SpreadExpr struct {
	ExprBase
	Name string
}

func (e *SpreadExpr) GetName() string { return "..." + e.Name }

func (e *SpreadExpr) String() string {
	return "..." + e.Name
}
