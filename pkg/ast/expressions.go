package ast

import (
	"fmt"

	"github.com/Lyra-Language/lyra/pkg/types"
)

type Expression interface {
	exprNode()
	GetName() string
	Print(indent string)
}

// Base struct to embed in all expression types
type ExprBase struct {
	AstBase
	Type types.Type
}

func (e *ExprBase) exprNode()             {}
func (e *ExprBase) GetLocation() Location { return e.Location }
func (e *ExprBase) GetName() string       { return "" }
func (e *ExprBase) Print(indent string)   {}

// Concrete expression types
type IntegerLiteralExpr struct {
	ExprBase
	Value int64
}

func (i *IntegerLiteralExpr) GetName() string {
	return fmt.Sprintf("%d", i.Value)
}

func (i *IntegerLiteralExpr) Print(indent string) {
	fmt.Printf("%sIntegerLiteralExpr(%d)\n", indent, i.Value)
}

type FloatLiteralExpr struct {
	ExprBase
	Value float64
}

func (f *FloatLiteralExpr) GetName() string {
	return fmt.Sprintf("%f", f.Value)
}

func (f *FloatLiteralExpr) Print(indent string) {
	fmt.Printf("%sFloatLiteralExpr(%f)\n", indent, f.Value)
}

type StringLiteralExpr struct {
	ExprBase
	Value string
}

func (s *StringLiteralExpr) GetName() string {
	return s.Value
}

func (s *StringLiteralExpr) Print(indent string) {
	fmt.Printf("%sStringLiteralExpr(%s)\n", indent, s.Value)
}

type BooleanLiteralExpr struct {
	ExprBase
	Value bool
}

func (b *BooleanLiteralExpr) GetName() string {
	return fmt.Sprintf("%t", b.Value)
}

func (b *BooleanLiteralExpr) Print(indent string) {
	fmt.Printf("%sBooleanLiteralExpr(%t)\n", indent, b.Value)
}

type IdentifierExpr struct {
	ExprBase
	Name string
}

func (i *IdentifierExpr) GetName() string {
	return i.Name
}

func (i *IdentifierExpr) Print(indent string) {
	fmt.Printf("%sIdentifierExpr(%s)\n", indent, i.Name)
}

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

type BooleanBinaryOpExpr struct {
	ExprBase
	Left     Expression
	Operator BooleanBinaryOp
	Right    Expression
}

func (b *BooleanBinaryOpExpr) GetName() string {
	return fmt.Sprintf("%s %s %s", b.Left.GetName(), b.Operator, b.Right.GetName())
}

func (b *BooleanBinaryOpExpr) Print(indent string) {
	fmt.Printf("%sBooleanBinaryOpExpr(%s)\n", indent, b.GetName())
	fmt.Printf("%s  Left: {\n", indent)
	b.Left.Print(indent + "    ")
	fmt.Printf("%s  }\n", indent)
	fmt.Printf("%s  Operator: %s\n", indent, b.Operator)
	fmt.Printf("%s  Right: {\n", indent)
	b.Right.Print(indent + "    ")
	fmt.Printf("%s  }\n", indent)
}

type BooleanBinaryOp string

const (
	BooleanBinaryOpLT  BooleanBinaryOp = "<"
	BooleanBinaryOpLTE BooleanBinaryOp = "<="
	BooleanBinaryOpGT  BooleanBinaryOp = ">"
	BooleanBinaryOpGTE BooleanBinaryOp = ">="
	BooleanBinaryOpEq  BooleanBinaryOp = "=="
	BooleanBinaryOpNEq BooleanBinaryOp = "!="
	BooleanBinaryOpAnd BooleanBinaryOp = "&&"
	BooleanBinaryOpOr  BooleanBinaryOp = "||"
)

type GuardExpr struct {
	ExprBase
	Condition Expression
}

func (g *GuardExpr) GetName() string {
	return fmt.Sprintf("guard %s", g.Condition.GetName())
}

func (g *GuardExpr) Print(indent string) {
	fmt.Printf("%sGuardExpr(%s)\n", indent, g.Condition.GetName())
	fmt.Printf("%s  Condition: {\n", indent)
	g.Condition.Print(indent + "    ")
	fmt.Printf("%s  }\n", indent)
}
