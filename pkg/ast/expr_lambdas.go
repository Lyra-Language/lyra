package ast

type LambdaExpr struct {
	ExprBase
	FunctionClause FunctionClause
	IsAsync bool
	IsPure bool
}

func (e *LambdaExpr) exprNode() {}

func (e *LambdaExpr) GetName() string {
	return "lambda"
}
