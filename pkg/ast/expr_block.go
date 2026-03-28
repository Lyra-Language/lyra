package ast

import "fmt"

type BlockExpr struct {
	ExprBase
	Statements []Statement
}

func (b *BlockExpr) GetName() string {
	return "block_expr"
}

func (b *BlockExpr) Print(indent string) {
	fmt.Printf("%sBlockExpr {\n", indent)
	for _, statement := range b.Statements {
		statement.Print(indent + "\t")
	}
	fmt.Printf("%s}\n", indent)
}
