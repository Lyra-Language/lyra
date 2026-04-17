package ast

import (
	"fmt"
)

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

func (e *LambdaExpr) Print(indent string) {
	fmt.Printf("%sLambdaExpr {\n", indent)
	if e.IsAsync {
		fmt.Printf("%s\tIsAsync: true\n", indent)
	}
	if e.IsPure {
		fmt.Printf("%s\tIsPure: true\n", indent)
	}
	fmt.Printf("%s\tFunctionClause: {\n", indent)
	e.FunctionClause.Print(indent + "\t\t")
	fmt.Printf("%s\t}\n", indent)
	fmt.Printf("%s}\n", indent)
}