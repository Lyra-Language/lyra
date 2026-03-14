package ast

import "fmt"

type IfExpr struct {
	ExprBase
	Condition Expression
	Then      Expression // nil if no then
	Else      Expression // nil if no else
}

func (i *IfExpr) GetName() string {
	return "if_expr"
}

func (i *IfExpr) Print(indent string) {
	fmt.Printf("%sIfExpr {\n", indent)
	fmt.Printf("%s  Condition: {\n", indent)
	i.Condition.Print(indent + "    ")
	fmt.Printf("%s  }\n", indent)
	fmt.Printf("%s  Then: {\n", indent)
	i.Then.Print(indent + "    ")
	fmt.Printf("%s  }\n", indent)
	if i.Else != nil {
		fmt.Printf("%s  Else: {\n", indent)
		i.Else.Print(indent + "    ")
		fmt.Printf("%s  }\n", indent)
	}
	fmt.Printf("%s}\n", indent)
}
