package ast

type WithStmt struct {
	AstBase
	Name  string
	Arena Expression
	Body  BlockExpr
}

func (w *WithStmt) statementNode()  {}
func (w *WithStmt) GetName() string { return "with_statement" }
