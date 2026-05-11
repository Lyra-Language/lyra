package types

import (
	"fmt"
)

// ConstrainedType uses the pointer convention: it is always constructed as
// *types.ConstrainedType (Collector.parseConstrainedType and
// typedecls.collectConstrainedTypeDeclaration) and then stored inside a
// types.Type interface. Receivers are pointer receivers so that only
// *ConstrainedType satisfies types.Type, keeping the convention enforced at
// compile time; code that matches the interface concrete type (notably
// TypesEqual) must use the pointer form *ConstrainedType.
type ConstrainedType struct {
	Name        string
	Type        Type
	Constraints []Constraint
}

func (c *ConstrainedType) typeNode()       {}
func (c *ConstrainedType) GetName() string { return c.Name }
func (c *ConstrainedType) String() string  { return c.GetName() }

type Constraint interface {
	constraintNode()
}

// LiteralUnionValue is implemented only by primitive literal AST nodes (int/float/string/bool).
// LiteralText restricts the interface to concrete literal types — no other expression has it.
type LiteralUnionValue interface {
	GetName() string
	GetType() Type
	LiteralText() string // source-text representation of the literal value, e.g. `42`, `"hello"`, `true`
}

// LiteralUnionConstraint holds a set of allowed literal values for a constrained type.
type LiteralUnionConstraint struct {
	Values []LiteralUnionValue
}

func (l *LiteralUnionConstraint) constraintNode() {}

func (l *LiteralUnionConstraint) GetName() string {
	return fmt.Sprintf("literal_union(%v)", l.Values)
}

type RangeConstraint struct {
	Start      MathConstraintExpr
	Comparator string
	End        MathConstraintExpr
}

func (r *RangeConstraint) constraintNode() {}

type MathConstraintExpr interface {
	mathConstraintExprNode()
	GetName() string
}

// LiteralNumberValue is implemented only by *ast.IntegerLiteralExpr and *ast.FloatLiteralExpr.
// Int64, Float64, and ConstraintString restrict this to numeric literal nodes.
type LiteralNumberValue interface {
	GetName() string
	GetType() Type
	Int64() (int64, bool)     // for extraction
	Float64() (float64, bool) // for extraction
	ConstraintString() string // numeric string for constraint display, e.g. "15" or "3.14"
}

// MathConstraintLiteralExpr implements MathConstraintExpr
type MathConstraintLiteralExpr struct {
	Value LiteralNumberValue
	Type  Type
}

func (m *MathConstraintLiteralExpr) mathConstraintExprNode() {}

func (m *MathConstraintLiteralExpr) GetName() string {
	return m.Value.ConstraintString()
}

// MathConstraintIdentifierExpr implements MathConstraintExpr
type MathConstraintIdentifierExpr struct {
	Name    string
	Type    Type
	IsConst bool
}

func (m *MathConstraintIdentifierExpr) mathConstraintExprNode() {}

func (m *MathConstraintIdentifierExpr) GetName() string {
	return m.Name
}

// MathConstraintBinaryOpExpr implements MathConstraintExpr
type MathConstraintBinaryOpExpr struct {
	Left     MathConstraintExpr
	Operator MathConstraintBinaryOp
	Right    MathConstraintExpr
}

func (m *MathConstraintBinaryOpExpr) mathConstraintExprNode() {}

func (m *MathConstraintBinaryOpExpr) GetName() string {
	return fmt.Sprintf("%s %s %s", m.Left.GetName(), m.Operator, m.Right.GetName())
}

type MathConstraintBinaryOp string

const (
	MathConstraintBinaryOpAdd MathConstraintBinaryOp = "+"
	MathConstraintBinaryOpSub MathConstraintBinaryOp = "-"
	MathConstraintBinaryOpMul MathConstraintBinaryOp = "*"
	MathConstraintBinaryOpDiv MathConstraintBinaryOp = "/"
)

// MathConstraintNegationExpr implements MathConstraintExpr
type MathConstraintNegationExpr struct {
	Operand MathConstraintExpr
}

func (m *MathConstraintNegationExpr) mathConstraintExprNode() {}

func (m *MathConstraintNegationExpr) GetName() string {
	return fmt.Sprintf("-%s", m.Operand.GetName())
}

type PrecisionConstraint struct {
	Value        MathConstraintExpr
	RoundingMode RoundingMode
}

func (p *PrecisionConstraint) constraintNode() {}

func (p *PrecisionConstraint) GetName() string {
	return fmt.Sprintf("precision(%s)", p.Value.GetName())
}

type RoundingMode string

const (
	RoundingModeNearestEven RoundingMode = "round_even"
	RoundingModeZero        RoundingMode = "round_zero"
	RoundingModeUp          RoundingMode = "round_up"
	RoundingModeDown        RoundingMode = "round_down"
	RoundingModeTrunc       RoundingMode = "round_trunc"
)

type StepConstraint struct {
	Value MathConstraintExpr
}

func (s *StepConstraint) constraintNode() {}

func (s *StepConstraint) GetName() string {
	return fmt.Sprintf("step(%s)", s.Value.GetName())
}

type PatternConstraint struct {
	Pattern string
}

func (p *PatternConstraint) constraintNode() {}

func (p *PatternConstraint) GetName() string {
	return fmt.Sprintf("pattern(%s)", p.Pattern)
}
