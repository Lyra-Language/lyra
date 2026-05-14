package ast

type ForLoopExpr struct {
	ExprBase
	Label     string
	Init      *VarDeclStmt
	Condition *Expression
	Post      *Expression
	Body      BlockExpr
}

func (t *ForLoopExpr) GetName() string { return "for_loop" }

type ForInLoopExpr struct {
	ExprBase
	Label    string
	Key      string
	Value    string
	Iterable Expression
	Body     BlockExpr
}

func (t *ForInLoopExpr) GetName() string { return "for_in_loop" }
