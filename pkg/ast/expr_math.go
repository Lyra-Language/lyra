package ast

type MathBinaryOpExpr struct {
	ExprBase
	Left     Expression
	Operator MathBinaryOp
	Right    Expression
}

func (m *MathBinaryOpExpr) GetName() string {
	return "math_binary_op_expr"
}

type MathBinaryOp string

const (
	MathBinaryOpAdd       MathBinaryOp = "+"
	MathBinaryOpSub       MathBinaryOp = "-"
	MathBinaryOpMul       MathBinaryOp = "*"
	MathBinaryOpDiv       MathBinaryOp = "/"
	MathBinaryOpMod       MathBinaryOp = "%"
	MathBinaryOpRemainder MathBinaryOp = "%%"

	// Bitwise and shift. Xor is `~` rather than `^`, which is taken by raw-pointer
	// types (`^T`) and postfix deref (`ptr^`) — see tree-sitter-lyra's CLAUDE.md.
	// The value of each constant is the operator's source text, which is what the
	// collector reads, so these need no mapping table of their own.
	MathBinaryOpBitAnd MathBinaryOp = "&"
	MathBinaryOpBitOr  MathBinaryOp = "|"
	MathBinaryOpBitXor MathBinaryOp = "~"
	MathBinaryOpShl    MathBinaryOp = "<<"
	MathBinaryOpShr    MathBinaryOp = ">>"
)

// IsBitwise reports whether op is one of the bitwise/shift operators — the set
// that requires integer operands and has no float or untyped-float form.
func (op MathBinaryOp) IsBitwise() bool {
	switch op {
	case MathBinaryOpBitAnd, MathBinaryOpBitOr, MathBinaryOpBitXor,
		MathBinaryOpShl, MathBinaryOpShr:
		return true
	}
	return false
}

// IsShift reports whether op is `<<` or `>>`. Shifts differ from the other
// bitwise operators in that their right operand is a *distance*, not a value of
// the same domain, which is what makes an over-wide amount a runtime trap.
func (op MathBinaryOp) IsShift() bool {
	return op == MathBinaryOpShl || op == MathBinaryOpShr
}

type MathAssignOpExpr struct {
	ExprBase
	Left     IdentifierExpr
	Operator MathAssignOp
	Right    Expression
}

func (m *MathAssignOpExpr) GetName() string {
	return "math_assign_op_expr"
}

type MathAssignOp string

const (
	MathAssignOpAdd       MathAssignOp = "+="
	MathAssignOpSub       MathAssignOp = "-="
	MathAssignOpMul       MathAssignOp = "*="
	MathAssignOpDiv       MathAssignOp = "/="
	MathAssignOpMod       MathAssignOp = "%="
	MathAssignOpRemainder MathAssignOp = "%%="

	MathAssignOpBitAnd MathAssignOp = "&="
	MathAssignOpBitOr  MathAssignOp = "|="
	MathAssignOpBitXor MathAssignOp = "~="
	MathAssignOpShl    MathAssignOp = "<<="
	MathAssignOpShr    MathAssignOp = ">>="
)

// BinaryOp returns the binary operator a compound assignment applies, so a
// consumer can reuse the binary type rules and lowering rather than re-deriving
// them per assignment form (`x &= y` is `x = x & y`).
func (op MathAssignOp) BinaryOp() (MathBinaryOp, bool) {
	switch op {
	case MathAssignOpAdd:
		return MathBinaryOpAdd, true
	case MathAssignOpSub:
		return MathBinaryOpSub, true
	case MathAssignOpMul:
		return MathBinaryOpMul, true
	case MathAssignOpDiv:
		return MathBinaryOpDiv, true
	case MathAssignOpMod:
		return MathBinaryOpMod, true
	case MathAssignOpRemainder:
		return MathBinaryOpRemainder, true
	case MathAssignOpBitAnd:
		return MathBinaryOpBitAnd, true
	case MathAssignOpBitOr:
		return MathBinaryOpBitOr, true
	case MathAssignOpBitXor:
		return MathBinaryOpBitXor, true
	case MathAssignOpShl:
		return MathBinaryOpShl, true
	case MathAssignOpShr:
		return MathBinaryOpShr, true
	}
	return "", false
}
