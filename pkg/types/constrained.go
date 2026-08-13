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

// StripNewtype returns t with any newtype wrapper removed. A newtype is
// *nominal only*: `newtype Percent = u8` is a distinct type to the typechecker
// (that isolation is the whole point — see isAssignable), but a Percent value
// **is** a u8 at run time, with no wrapper of its own. So every decision about
// representation — the LLVM type, whether the value is refcount-managed, an
// untyped literal's width, how print formats it — must key off the base, and
// this is how those consumers get there.
//
// Chained newtypes unwrap transitively. A base written as a *name* is stored as
// an UnresolvedType, so resolving that far needs the symbol table; consumers
// that have one resolve first and strip after (the backend's recordedType, the
// ownership pass's OwnsManaged).
func StripNewtype(t Type) Type {
	for {
		ct, ok := t.(*ConstrainedType)
		if !ok {
			return t
		}
		t = ct.Type
	}
}

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
	// Pattern is the constraint's **source text, delimiters included** — `r"[0-9]+"`
	// rather than `[0-9]+` — because a diagnostic quotes it back the way it was
	// written. Body() is the form a regex engine takes.
	Pattern string
}

func (p *PatternConstraint) constraintNode() {}

// Body strips the `r"…"` delimiters, giving the pattern a regex engine compiles.
//
// It lives on the type because two places need it and they must not drift: the
// typechecker compiles the pattern to check a string literal, and the backend
// compiles the *same* pattern into the DFA tables it emits for the runtime check
// (08/13). Those are the two rungs of one constraint, so a difference in what they
// strip would be a difference in what they match.
func (p *PatternConstraint) Body() string {
	s := p.Pattern
	if len(s) >= 3 && s[:2] == `r"` && s[len(s)-1] == '"' {
		return s[2 : len(s)-1]
	}
	return s // already stripped, or a bare pattern string
}

func (p *PatternConstraint) GetName() string {
	return fmt.Sprintf("pattern(%s)", p.Pattern)
}
