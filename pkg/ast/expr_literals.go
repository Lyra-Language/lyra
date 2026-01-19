package ast

import "fmt"

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
