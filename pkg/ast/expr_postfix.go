package ast

import (
	"fmt"

	"github.com/Lyra-Language/lyra/pkg/types"
)

type FunctionCallExpr struct {
	ExprBase
	Function         Expression
	GenericArguments []types.Type
	Arguments        []Expression
}

func (f *FunctionCallExpr) GetName() string {
	return "function_call_expr"
}

type MemberExpr struct {
	ExprBase
	Object   Expression
	Property IdentifierExpr
	Optional bool
}

func (m *MemberExpr) GetName() string {
	return fmt.Sprintf("MemberExpr(%s.%s)", m.Object.GetName(), m.Property.GetName())
}

type IndexExpr struct {
	ExprBase
	Object   Expression
	Index    Expression
	Optional bool
}

func (i *IndexExpr) GetName() string {
	return fmt.Sprintf("IndexExpr(%s[%s])", i.Object.GetName(), i.Index.GetName())
}

type TryExpr struct {
	ExprBase
	Operand Expression
}

func (t *TryExpr) GetName() string {
	return fmt.Sprintf("TryExpr(%s)", t.Operand.GetName())
}
