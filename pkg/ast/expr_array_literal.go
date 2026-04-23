package ast

import (

	"github.com/Lyra-Language/lyra/pkg/types"
)

type ArrayLiteralExpr struct {
	ExprBase
	Elements []Expression
}

func (a *ArrayLiteralExpr) exprNode() {}

func (a *ArrayLiteralExpr) GetType() types.Type {
	return a.ExprBase.Type // Returns DynamicArrayType or StaticArrayType
}

func (a *ArrayLiteralExpr) GetName() string {
	return "array_literal_expr"
}

func (a *ArrayLiteralExpr) GetElementType() types.Type {
	if arrType, ok := a.ExprBase.Type.(types.DynamicArrayType); ok {
		return arrType.ElementType
	}

	if arrType, ok := a.ExprBase.Type.(types.StaticArrayType); ok {
		return arrType.ElementType
	}

	return nil
}
