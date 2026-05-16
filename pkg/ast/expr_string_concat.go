package ast

// StringConcatExpr represents a string concatenation expression using the `++`
// operator, e.g. `"Hello, " ++ name`. Both operands are arbitrary expressions;
// the type-checker is responsible for verifying they are strings.
type StringConcatExpr struct {
	ExprBase
	Left     Expression
	Operator string // always "++"
	Right    Expression
}

func (s *StringConcatExpr) GetName() string { return "string_concat_expr" }
