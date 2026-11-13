package ast

import (
	"github.com/Lyra-Language/lyra/pkg/types"
)

type TupleLiteralExpr struct {
	ExprBase
	Elements         []Expression
	GenericArguments []types.Type
	Name             string
	// Namespace is the namespace import the name was written through (`shapes` in
	// `shapes.Circle(2)`), empty for a bare name. Set when the driver erases the qualifier
	// (eraseQualifiedNames), so a re-analysis of the same node can record it again.
	Namespace string
	// SpacedMinus is the operand's text when a constructor was juxtaposed with a `-` spaced on
	// both sides — `1` in `Empty - 1`, which reads as a subtraction and is `Empty(-1)` — and
	// empty otherwise. Only the collector sees the spacing; the typechecker decides what to
	// say, knowing whether the constructor takes a payload (lyra-W033).
	SpacedMinus string
}

func (t *TupleLiteralExpr) exprNode() {}

func (t *TupleLiteralExpr) GetName() string {
	return "tuple_literal_expr"
}
