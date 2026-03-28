package ast

import (
	"fmt"

	"github.com/Lyra-Language/lyra/pkg/types"
)

type DestructuringDeclStmt struct {
	AstBase
	Keyword string
	Pattern Pattern
	Type    types.Type
	Value   Expression
}

func (d *DestructuringDeclStmt) statementNode() {}

func (d *DestructuringDeclStmt) GetName() string { return d.Pattern.GetName() }
func (d *DestructuringDeclStmt) Print(indent string) {
	fmt.Printf("%sDestructuringStatement {\n", indent)
	fmt.Printf("%s\tKeyword: %s\n", indent, d.Keyword)
	d.Pattern.Print(indent + "\t")
	fmt.Printf("\n%s\tValue: {\n", indent)
	d.Value.Print(indent + "\t\t")
	fmt.Printf("%s\t}\n", indent)
	if d.Type != nil {
		fmt.Printf("%s\tType: {\n", indent)
		d.Type.Print(indent + "\t\t")
		fmt.Printf("%s\t}\n", indent)
	}
	fmt.Printf("%s}\n", indent)
}

type IfDestructuringStmt struct {
	AstBase
	DestructuringStatement DestructuringDeclStmt
	Then                   BlockExpr
	Else                   *BlockExpr // nil when the source omits `else { ... }`
}

func (i *IfDestructuringStmt) statementNode() {}

func (i *IfDestructuringStmt) GetName() string {
	if i.Else != nil {
		return fmt.Sprintf("if %s { %s } else { %s }", i.DestructuringStatement.GetName(), i.Then.GetName(), i.Else.GetName())
	}
	return fmt.Sprintf("if %s { %s }", i.DestructuringStatement.GetName(), i.Then.GetName())
}

func (i *IfDestructuringStmt) Print(indent string) {
	fmt.Printf("%sIfDestructuringStatement {\n", indent)
	i.DestructuringStatement.Print(indent + "\t")
	fmt.Printf("%s\tThen: {\n", indent)
	i.Then.Print(indent + "\t\t")
	fmt.Printf("%s\t}\n", indent)
	if i.Else != nil {
		fmt.Printf("%s\tElse: {\n", indent)
		i.Else.Print(indent + "\t\t")
		fmt.Printf("%s\t}\n", indent)
	}
	fmt.Printf("%s}\n", indent)
}
