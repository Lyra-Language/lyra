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
}

func (t *TupleLiteralExpr) exprNode() {}

func (t *TupleLiteralExpr) GetName() string {
	return "tuple_literal_expr"
}
