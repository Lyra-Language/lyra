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
	fmt.Printf("%s\tArguments: {\n", indent)
	f.Arguments.Print(indent + "\t\t")
	fmt.Printf("%s\t}\n", indent)
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
	return fmt.Sprintf("%s", strings.Join(argumentNames, ", "))
}

func (a *ArgumentList) Print(indent string) {
	fmt.Printf("%sArgumentList(%s) {\n", indent, a.GetName())
	for _, argument := range a.Arguments {
		argument.Print(indent + "\t")
	}
	fmt.Printf("%s}\n", indent)
}
