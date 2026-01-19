package ast

import "fmt"

type IfThenExpr struct {
	ExprBase
	Condition Expression
	Then      Expression
	Else      Expression // nil if no else
}

func (i *IfThenExpr) GetName() string {
	return fmt.Sprintf("if %s then %s else %s", i.Condition.GetName(), i.Then.GetName(), i.Else.GetName())
}

func (i *IfThenExpr) Print(indent string) {
	fmt.Printf("%sIfThenExpr(%s)\n", indent, i.Condition.GetName())
	fmt.Printf("%s  Then: {\n", indent)
	i.Then.Print(indent + "    ")
	fmt.Printf("%s  }\n", indent)
	fmt.Printf("%s  Else: {\n", indent)
	i.Else.Print(indent + "    ")
	fmt.Printf("%s  }\n", indent)
}

type IfBlockExpr struct {
	ExprBase
	Condition Expression
	Then      Expression
	Else      Expression // nil if no else
}

func (i *IfBlockExpr) GetName() string {
	return fmt.Sprintf("if %s { %s } else { %s }", i.Condition.GetName(), i.Then.GetName(), i.Else.GetName())
}

func (i *IfBlockExpr) Print(indent string) {
	fmt.Printf("%sIfBlockExpr(%s)\n", indent, i.Condition.GetName())
	fmt.Printf("%s  Then: {\n", indent)
	i.Then.Print(indent + "    ")
	fmt.Printf("%s  }\n", indent)
	fmt.Printf("%s  Else: {\n", indent)
	i.Else.Print(indent + "    ")
	fmt.Printf("%s  }\n", indent)
}
