package ast

import (
	"fmt"

	"github.com/Lyra-Language/lyra/pkg/types"
)

type ArrayCompExpr struct {
	ExprBase
	Generators []Generator
	Guards     []Expression
	Result     Expression
}

func (a *ArrayCompExpr) exprNode() {}

func (a *ArrayCompExpr) GetName() string {
	return "array_comp_expr"
}

// TODO: implement this correctly
func (a *ArrayCompExpr) GetType() types.Type {
	return nil
}

func (a *ArrayCompExpr) Print(indent string) {
	fmt.Printf("%sArrayCompExpr {\n", indent)
	fmt.Printf("%s  Generators: {\n", indent)
	for _, generator := range a.Generators {
		generator.Print(indent + "    ")
	}
	fmt.Printf("%s  }\n", indent)
	fmt.Printf("%s  Guards: {\n", indent)
	for _, guard := range a.Guards {
		fmt.Printf("%s    Guard: {\n", indent)
		guard.Print(indent + "      ")
		fmt.Printf("%s    }\n", indent)
	}
	fmt.Printf("%s  }\n", indent)
	fmt.Printf("%s  Result: {\n", indent)
	a.Result.Print(indent + "    ")
	fmt.Printf("%s  }\n", indent)
	fmt.Printf("%s}\n", indent)
}

type Generator struct {
	ExprBase
	Value      Expression
	Identifier string
}

func (g *Generator) exprNode() {}

func (g *Generator) GetName() string {
	return "generator"
}

func (g *Generator) GetType() types.Type {
	return nil
}

func (g *Generator) Print(indent string) {
	fmt.Printf("%sGenerator {\n", indent)
	fmt.Printf("%s  Value: {\n", indent)
	g.Value.Print(indent + "    ")
	fmt.Printf("%s  }\n", indent)
	fmt.Printf("%s  Identifier: %s\n", indent, g.Identifier)
	fmt.Printf("%s}\n", indent)
}
