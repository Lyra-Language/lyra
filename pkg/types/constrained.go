package types

import (
	"fmt"
)

type ConstrainedType struct {
	Name        string
	Type        Type
	Constraints []Constraint
}

func (c *ConstrainedType) typeNode()           {}
func (c *ConstrainedType) IsNumericType() bool { return false }
func (c *ConstrainedType) GetName() string     { return c.Name }
func (c *ConstrainedType) Print(indent string) {
	fmt.Printf("%sConstrainedType {\n", indent)
	fmt.Printf("%s\tName: %s\n", indent, c.Name)
	fmt.Printf("%s\tType: %s\n", indent, c.Type.GetName())
	if len(c.Constraints) > 0 {
		fmt.Printf("%s\tConstraints: {\n", indent)
		for _, constraint := range c.Constraints {
			constraint.Print(indent + "\t")
		}
		fmt.Printf("%s\t}\n", indent)
	}
	fmt.Printf("%s}\n", indent)
}

type Constraint interface {
	constraintNode()
	Print(indent string)
}

// LiteralUnionValue is implemented only by primitive literal AST nodes (int/float/string/bool).
// The marker method ensures other expressions (identifiers, calls, etc.) do not satisfy this interface.
type LiteralUnionValue interface {
	GetName() string
	GetType() Type
	SealLiteralUnion() // marker; only ast *IntegerLiteralExpr, *StringLiteralExpr, etc. implement this
}

// LiteralUnionConstraint holds a set of allowed literal values for a constrained type.
type LiteralUnionConstraint struct {
	Values []LiteralUnionValue
}

func (l *LiteralUnionConstraint) constraintNode() {}
func (l *LiteralUnionConstraint) Print(indent string) {
	fmt.Printf("%sLiteralUnionConstraint {\n", indent)
	for _, value := range l.Values {
		fmt.Printf("%s\tValue: %s\n", indent, value.GetName())
	}
	fmt.Printf("%s}\n", indent)
}

func (l *LiteralUnionConstraint) GetName() string {
	return fmt.Sprintf("literal_union(%v)", l.Values)
}

type RangeConstraint struct {
	Start      MathConstraintExpr
	Comparator string
	End        MathConstraintExpr
}

func (r *RangeConstraint) constraintNode() {}
func (r *RangeConstraint) Print(indent string) {
	fmt.Printf("%sRangeConstraint {\n", indent)
	if r.Start != nil {
		fmt.Printf("%s\tStart: {\n", indent)
		r.Start.Print(indent + "\t")
		fmt.Printf("%s\t}\n", indent)
	}
	fmt.Printf("%s\tComparator: %s\n", indent, r.Comparator)
	if r.End != nil {
		fmt.Printf("%s\tEnd: {\n", indent)
		r.End.Print(indent + "\t")
		fmt.Printf("%s\t}\n", indent)
	}
	fmt.Printf("%s}\n", indent)
}

type MathConstraintExpr interface {
	mathConstraintExprNode()
	GetName() string
	Print(indent string)
}

// LiteralNumberValue is implemented only by *ast.IntegerLiteralExpr and *ast.FloatLiteralExpr.
// Using AST nodes keeps constraint literals consistent with the rest of the AST (e.g. LiteralUnionValue).
type LiteralNumberValue interface {
	GetName() string
	GetType() Type
	SealMathConstraintLiteral() // marker; only ast int/float literal nodes implement this
	Int64() (int64, bool)       // for extraction
	Float64() (float64, bool)   // for extraction
	ConstraintString() string   // numeric string for constraint display, e.g. "15" or "3.14"
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

func (m *MathConstraintLiteralExpr) Print(indent string) {
	fmt.Printf("%sMathConstraintLiteralExpr { \n", indent)
	fmt.Printf("%s\tValue: %s\n", indent, m.Value.ConstraintString())
	if m.Type != nil {
		fmt.Printf("%s\tType: {\n", indent)
		m.Type.Print(indent + "\t")
		fmt.Printf("%s\t}\n", indent)
	}
	fmt.Printf("%s}\n", indent)
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

func (m *MathConstraintIdentifierExpr) Print(indent string) {
	fmt.Printf("%sMathConstraintIdentifierExpr(%s)\n", indent, m.Name)
	if m.Type != nil {
		fmt.Printf("%s\tType: {\n", indent)
		m.Type.Print(indent + "\t")
		fmt.Printf("%s\t}\n", indent)
	}
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

func (m *MathConstraintBinaryOpExpr) Print(indent string) {
	fmt.Printf("%sMathConstraintBinaryOpExpr(%s) {\n", indent, m.GetName())
	fmt.Printf("%s\tLeft: {\n", indent)
	m.Left.Print(indent + "\t")
	fmt.Printf("%s\t}\n", indent)
	fmt.Printf("%s\tOperator: %s\n", indent, m.Operator)
	fmt.Printf("%s\tRight: {\n", indent)
	m.Right.Print(indent + "\t")
	fmt.Printf("%s\t}\n", indent)
	fmt.Printf("%s}\n", indent)
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

func (m *MathConstraintNegationExpr) Print(indent string) {
	fmt.Printf("%sMathConstraintNegationExpr(%s)\n", indent, m.GetName())
	fmt.Printf("%s\tOperand: {\n", indent)
	m.Operand.Print(indent + "\t")
	fmt.Printf("%s\t}\n", indent)
}

type PrecisionConstraint struct {
	Value        MathConstraintExpr
	RoundingMode RoundingMode
}

func (p *PrecisionConstraint) constraintNode() {}

func (p *PrecisionConstraint) GetName() string {
	return fmt.Sprintf("precision(%s)", p.Value.GetName())
}

func (p *PrecisionConstraint) Print(indent string) {
	fmt.Printf("%sPrecisionConstraint(%s)\n", indent, p.GetName())
	fmt.Printf("%s\tValue: {\n", indent)
	p.Value.Print(indent + "\t")
	fmt.Printf("%s\t}\n", indent)
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

func (s *StepConstraint) Print(indent string) {
	fmt.Printf("%sStepConstraint(%s)\n", indent, s.GetName())
	fmt.Printf("%s\tValue: {\n", indent)
	s.Value.Print(indent + "\t")
	fmt.Printf("%s\t}\n", indent)
}

type PatternConstraint struct {
	Pattern string
}

func (p *PatternConstraint) constraintNode() {}

func (p *PatternConstraint) GetName() string {
	return fmt.Sprintf("pattern(%s)", p.Pattern)
}

func (p *PatternConstraint) Print(indent string) {
	fmt.Printf("%sPatternConstraint(%s)\n", indent, p.GetName())
	fmt.Printf("%s\tPattern: %s\n", indent, p.Pattern)
}
