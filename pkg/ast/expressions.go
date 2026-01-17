package ast

import (
	"fmt"

	"github.com/Lyra-Language/lyra/pkg/types"
)

type Expression interface {
	exprNode()
	GetType() types.Type
	SetType(types.Type)
	GetName() string
}

// Base struct to embed in all expression types
type ExprBase struct {
	AstBase
	Type types.Type
}

func (e *ExprBase) exprNode()             {}
func (e *ExprBase) GetType() types.Type   { return e.Type }
func (e *ExprBase) SetType(t types.Type)  { e.Type = t }
func (e *ExprBase) GetLocation() Location { return e.Location }
func (e *ExprBase) GetName() string       { return "" }

// Concrete expression types
type IntegerLiteral struct {
	ExprBase
	Value int64
}

func (i *IntegerLiteral) GetName() string {
	return fmt.Sprintf("%d", i.Value)
}

type FloatLiteral struct {
	ExprBase
	Value float64
}

func (f *FloatLiteral) GetName() string {
	return fmt.Sprintf("%f", f.Value)
}

type StringLiteral struct {
	ExprBase
	Value string
}

func (s *StringLiteral) GetName() string {
	return s.Value
}

type BooleanLiteral struct {
	ExprBase
	Value bool
}

func (b *BooleanLiteral) GetName() string {
	return fmt.Sprintf("%t", b.Value)
}

type Identifier struct {
	ExprBase
	Name string
}

func (i *Identifier) GetName() string {
	return i.Name
}

// type BinaryExpr struct {
// 	exprBase
// 	Left     Expression
// 	Operator string
// 	Right    Expression
// }

// type UnaryExpr struct {
// 	exprBase
// 	Operator string
// 	Operand  Expression
// }

// type CallExpr struct {
// 	exprBase
// 	Callee    Expression
// 	Arguments []Expression
// }

// type IfThenElse struct {
// 	exprBase
// 	Condition Expression
// 	Then      Expression
// 	Else      Expression // nil if no else
// }

// type ArrayLiteral struct {
// 	exprBase
// 	Elements []Expression
// }

// type IndexExpr struct {
// 	exprBase
// 	Object Expression
// 	Index  Expression
// }

// type MemberExpr struct {
// 	exprBase
// 	Object Expression
// 	Member string
// }

// type LambdaExpr struct {
// 	exprBase
// 	Parameters []Pattern
// 	Body       Expression
// }
