package ast

import (
	"fmt"

	"github.com/Lyra-Language/lyra/pkg/types"
)

type PrimitiveLiteralValue interface {
	primitiveLiteralValueNode()
	GetName() string
}

type IntegerLiteralExpr struct {
	ExprBase
	Value int64
	Base  IntegerBase
}

func (i *IntegerLiteralExpr) primitiveLiteralValueNode() {}
func (i *IntegerLiteralExpr) SealLiteralUnion()          {}
func (i *IntegerLiteralExpr) SealMathConstraintLiteral() {}

func (i *IntegerLiteralExpr) GetName() string {
	return fmt.Sprintf("IntegerLiteralExpr(%d, Base: %d)", i.Value, i.Base)
}

func (i *IntegerLiteralExpr) GetType() types.Type {
	return types.PrimitiveType{Name: types.Int}
}

func (i *IntegerLiteralExpr) Int64() (int64, bool)       { return i.Value, true }
func (i *IntegerLiteralExpr) Float64() (float64, bool)   { return 0, false }
func (i *IntegerLiteralExpr) ConstraintString() string   { return fmt.Sprintf("%d", i.Value) }

func (i *IntegerLiteralExpr) Print(indent string) {
	fmt.Printf("%s%s\n", indent, i.GetName())
}

type IntegerBase int // 10, 8, 16, 2

const (
	IntegerBase2  IntegerBase = 2
	IntegerBase8  IntegerBase = 8
	IntegerBase10 IntegerBase = 10
	IntegerBase16 IntegerBase = 16
)

type FloatLiteralExpr struct {
	ExprBase
	Value float64
}

func (f *FloatLiteralExpr) primitiveLiteralValueNode() {}
func (f *FloatLiteralExpr) SealLiteralUnion()          {}
func (f *FloatLiteralExpr) SealMathConstraintLiteral() {}

func (f *FloatLiteralExpr) GetName() string {
	return fmt.Sprintf("FloatLiteralExpr(%g)", f.Value) // NOTE: no trailing zeros
}

func (f *FloatLiteralExpr) Print(indent string) {
	fmt.Printf("%s%s\n", indent, f.GetName())
}

func (f *FloatLiteralExpr) GetType() types.Type {
	return types.PrimitiveType{Name: types.Float}
}

func (f *FloatLiteralExpr) Int64() (int64, bool)       { return 0, false }
func (f *FloatLiteralExpr) Float64() (float64, bool)   { return f.Value, true }
func (f *FloatLiteralExpr) ConstraintString() string   { return fmt.Sprintf("%g", f.Value) }

type StringLiteralExpr struct {
	ExprBase
	Value string
}

func (s *StringLiteralExpr) primitiveLiteralValueNode() {}
func (s *StringLiteralExpr) SealLiteralUnion()          {}

func (s *StringLiteralExpr) GetName() string {
	return fmt.Sprintf("StringLiteralExpr(%s)", s.Value)
}

func (s *StringLiteralExpr) GetType() types.Type {
	return types.PrimitiveType{Name: types.String}
}

func (s *StringLiteralExpr) Print(indent string) {
	fmt.Printf("%s%s\n", indent, s.GetName())
}

type BooleanLiteralExpr struct {
	ExprBase
	Value bool
}

func (b *BooleanLiteralExpr) primitiveLiteralValueNode() {}
func (b *BooleanLiteralExpr) SealLiteralUnion()          {}

func (b *BooleanLiteralExpr) GetType() types.Type {
	return types.PrimitiveType{Name: types.Bool}
}

func (b *BooleanLiteralExpr) GetName() string {
	return fmt.Sprintf("BooleanLiteralExpr(%t)", b.Value)
}

func (b *BooleanLiteralExpr) Print(indent string) {
	fmt.Printf("%s%s\n", indent, b.GetName())
}
