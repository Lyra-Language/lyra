package ast

type BlockExpr struct {
	ExprBase
	Statements []Statement
}

func (b *BlockExpr) GetName() string {
	return "block_expr"
}
