package ast

import (
	"fmt"
	"strings"

	"github.com/Lyra-Language/lyra/pkg/types"
)

type FunctionCallExpr struct {
	ExprBase
	Function         Expression
	GenericArguments []types.Type
	Arguments        ArgumentList
}

func (f *FunctionCallExpr) GetName() string {
	return "function_call_expr"
}

func (f *FunctionCallExpr) Print(indent string) {
	fmt.Printf("%sFunctionCallExpr {\n", indent)
	fmt.Printf("%s\tFunction: {\n", indent)
	f.Function.Print(indent + "\t\t")
	fmt.Printf("%s\t}\n", indent)
	if f.GenericArguments != nil {
		fmt.Printf("%s\tGenericArguments: {\n", indent)
		for _, genericArgument := range f.GenericArguments {
			genericArgument.Print(indent + "\t")
		}
		fmt.Printf("%s\t}\n", indent)
	}
	if len(f.Arguments.Arguments) > 0 {
		fmt.Printf("%s\tArguments: {\n", indent)
		f.Arguments.Print(indent + "\t\t")
		fmt.Printf("%s\t}\n", indent)
	}
	fmt.Printf("%s}\n", indent)
}

type ArgumentList struct {
	Arguments []Expression
}

func (a *ArgumentList) GetName() string {
	argumentNames := make([]string, len(a.Arguments))
	for i, argument := range a.Arguments {
		argumentNames[i] = argument.GetName()
	}
	return strings.Join(argumentNames, ", ")
}

func (a *ArgumentList) Print(indent string) {
	fmt.Printf("%sArgumentList(%s) {\n", indent, a.GetName())
	for _, argument := range a.Arguments {
		argument.Print(indent + "\t")
	}
	fmt.Printf("%s}\n", indent)
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

func (m *MemberExpr) Print(indent string) {
	fmt.Printf("%sMemberExpr {\n", indent)
	fmt.Printf("%s\tObject: {\n", indent)
	m.Object.Print(indent + "\t\t")
	fmt.Printf("%s\t}\n", indent)
	fmt.Printf("%s\tProperty: {\n", indent)
	m.Property.Print(indent + "\t\t")
	fmt.Printf("%s\t}\n", indent)
	fmt.Printf("%s\tOptional: %t\n", indent, m.Optional)
	fmt.Printf("%s}\n", indent)
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

func (i *IndexExpr) Print(indent string) {
	fmt.Printf("%sIndexExpr {\n", indent)
	fmt.Printf("%s\tObject: {\n", indent)
	i.Object.Print(indent + "\t\t")
	fmt.Printf("%s\t}\n", indent)
	fmt.Printf("%s\tIndex: {\n", indent)
	i.Index.Print(indent + "\t\t")
	fmt.Printf("%s\t}\n", indent)
	fmt.Printf("%s\tOptional: %t\n", indent, i.Optional)
	fmt.Printf("%s}\n", indent)
}

type TryExpr struct {
	ExprBase
	Operand Expression
}

func (t *TryExpr) GetName() string {
	return fmt.Sprintf("TryExpr(%s)", t.Operand.GetName())
}

func (t *TryExpr) Print(indent string) {
	fmt.Printf("%sTryExpr {\n", indent)
	fmt.Printf("%s\tOperand: {\n", indent)
	t.Operand.Print(indent + "\t\t")
	fmt.Printf("%s\t}\n", indent)
	fmt.Printf("%s}\n", indent)
}