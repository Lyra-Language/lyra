package ast

type ForLoopStmt struct {
	AstBase
	Label     string
	Init      *VarDeclStmt
	Condition *Expression
	Post      *Expression
	Body      BlockExpr
}

func (t *ForLoopStmt) statementNode()  {}
func (t *ForLoopStmt) GetName() string { return "for_loop" }
