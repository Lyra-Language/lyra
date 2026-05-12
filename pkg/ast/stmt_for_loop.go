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

type ForInLoopStmt struct {
	AstBase
	Label    string
	Key      string
	Value    string
	Iterable Expression
	Body     BlockExpr
}

func (t *ForInLoopStmt) statementNode()  {}
func (t *ForInLoopStmt) GetName() string { return "for_in_loop" }
