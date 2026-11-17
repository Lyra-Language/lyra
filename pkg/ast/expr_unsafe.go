package ast

type UnsafeBlockExpr struct {
	ExprBase
	// Body is a *pointer* so the block keeps the identity the collector gave it. Stored
	// by value it was a *copy*, with a different address from the node the scope table
	// was keyed on — so a binding declared inside an `unsafe` block resolved nowhere and
	// every later reference to it reported "undefined identifier". Invisible for as long
	// as the block was refused before anything looked inside it.
	Body *BlockExpr
}

func (u *UnsafeBlockExpr) GetName() string { return "unsafe_block" }

// UnsafeBlockValue is e with any `unsafe { … }` holding nothing but one expression looked
// through: `unsafe { slot^.texture }` is the read `slot^.texture` is. Anything else is e.
func UnsafeBlockValue(e Expression) Expression {
	for {
		u, ok := e.(*UnsafeBlockExpr)
		if !ok || u.Body == nil || len(u.Body.Statements) != 1 {
			return e
		}
		s, ok := u.Body.Statements[0].(*ExpressionStmt)
		if !ok || s.Expression == nil {
			return e
		}
		e = s.Expression
	}
}
