package ast

type ArrayLiteralExpr struct {
	ExprBase
	Elements []Expression
	// Fixed is the `#[…]` spelling: a `[N]T` with inline storage. Without it the literal is a
	// dynamic `[]T`. The spelling is the whole of the flavor — nothing infers it.
	Fixed bool
	// Banked is `@banked` on the const this initializes: it goes in a ROM bank on a console
	// that has them (LANGUAGE.md), and is an ordinary table elsewhere.
	Banked bool
}

func (a *ArrayLiteralExpr) exprNode() {}

func (a *ArrayLiteralExpr) GetName() string {
	return "array_literal_expr"
}
