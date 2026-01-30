package collector

import (
	"fmt"
	"strconv"

	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/types"
	sitter "github.com/tree-sitter/go-tree-sitter"
)

func (c *Collector) collectConstrainedTypeDeclaration(node *sitter.Node) *ast.TypeDeclStmt {
	nameNode := node.ChildByFieldName("name")
	typeNode := node.ChildByFieldName("type")
	constraintsNode := node.ChildByFieldName("constraints")

	name := c.nodeText(nameNode)
	typeType := c.parseType(typeNode)

	astNode := &ast.TypeDeclStmt{
		AstBase: ast.AstBase{Location: c.nodeLocation(node)},
		Name:    name,
		Type: &types.ConstrainedType{
			Name:        name,
			Type:        typeType,
			Constraints: c.collectConstraints(constraintsNode),
		},
	}

	if err := c.table.RegisterType(astNode); err != nil {
		c.errors = append(c.errors, err)
	}

	return astNode
}

func (c *Collector) collectConstraints(node *sitter.Node) []types.Constraint {
	constraints := make([]types.Constraint, 0)
	for i := uint(0); i < node.ChildCount(); i++ {
		child := node.Child(i)
		switch child.Kind() {
		case "range_constraint":
			constraints = append(constraints, c.collectRangeConstraint(child))
		case "precision_constraint":
			constraints = append(constraints, c.collectPrecisionConstraint(child))
		}
	}
	return constraints
}

func (c *Collector) collectRangeConstraint(node *sitter.Node) *types.RangeConstraint {
	var start types.MathConstraintExpr
	var comparator string
	var end types.MathConstraintExpr
	startNode := node.ChildByFieldName("start")
	comparatorNode := node.ChildByFieldName("comparator")
	endNode := node.ChildByFieldName("end")

	if startNode != nil {
		start = c.collectMathConstraintExpr(startNode)
	}
	if comparatorNode != nil {
		comparator = c.nodeText(comparatorNode)
	}
	if endNode != nil {
		end = c.collectMathConstraintExpr(endNode)
	}

	if start == nil && end == nil {
		c.errors = append(c.errors, fmt.Errorf("range constraint must have a start or end"))
	}
	return &types.RangeConstraint{
		Start:      start,
		Comparator: comparator,
		End:        end,
	}
}

func (c *Collector) collectMathConstraintExpr(node *sitter.Node) types.MathConstraintExpr {
	if node == nil {
		return nil
	}
	switch node.Kind() {
	case "constraint_math_expr":
		return c.collectMathConstraintExpr(node.Child(0))
	case "integer_literal":
		value, _ := strconv.ParseInt(c.nodeText(node), 10, 64)
		return &types.MathConstraintLiteralExpr{
			Value: value,
			Type:  types.PrimitiveType{Name: types.Int},
		}
	case "float_literal":
		value, _ := strconv.ParseFloat(c.nodeText(node), 64)
		return &types.MathConstraintLiteralExpr{
			Value: value,
			Type:  types.PrimitiveType{Name: types.Float},
		}
	case "identifier", "const_identifier":
		return &types.MathConstraintIdentifierExpr{
			Name:    c.nodeText(node),
			Type:    c.parseType(node.ChildByFieldName("type")),
			IsConst: node.Kind() == "const_identifier",
		}
	case "constraint_multiplication", "constraint_division", "constraint_addition", "constraint_subtraction":
		return c.collectMathConstraintBinaryOpExpr(node)
		// case "constraint_negation":
		// 	return c.collectMathConstraintNegation(node)
	}
	return nil
}

func (c *Collector) collectMathConstraintBinaryOpExpr(node *sitter.Node) types.MathConstraintExpr {
	leftNode := node.ChildByFieldName("left")
	operatorNode := node.ChildByFieldName("operator")
	rightNode := node.ChildByFieldName("right")
	operator := types.MathConstraintBinaryOp(c.nodeText(operatorNode))
	left := c.collectMathConstraintExpr(leftNode)
	right := c.collectMathConstraintExpr(rightNode)
	if left == nil || right == nil {
		c.errors = append(c.errors, fmt.Errorf("math constraint binary operator must have a left and right operand"))
		return nil
	}
	binaryOperator := types.MathConstraintBinaryOp("")
	switch operator {
	case types.MathConstraintBinaryOpMul:
		binaryOperator = types.MathConstraintBinaryOpMul
	case types.MathConstraintBinaryOpDiv:
		binaryOperator = types.MathConstraintBinaryOpDiv
	case types.MathConstraintBinaryOpAdd:
		binaryOperator = types.MathConstraintBinaryOpAdd
	case types.MathConstraintBinaryOpSub:
		binaryOperator = types.MathConstraintBinaryOpSub
	}
	if binaryOperator == "" {
		c.errors = append(c.errors, fmt.Errorf("invalid binary operator: %s", operator))
		return nil
	}
	return &types.MathConstraintBinaryOpExpr{
		Left:     left,
		Operator: operator,
		Right:    right,
	}
}

func (c *Collector) collectPrecisionConstraint(node *sitter.Node) *types.PrecisionConstraint {
	valueNode := node.ChildByFieldName("value")
	if valueNode == nil {
		c.errors = append(c.errors, fmt.Errorf("precision constraint must have a value"))
		return nil
	}
	roundingModeNode := node.ChildByFieldName("rounding_mode")
	roundingMode := types.RoundingModeNearestEven
	if roundingModeNode != nil {
		roundingMode = types.RoundingMode(c.nodeText(roundingModeNode))
		if roundingMode == "" {
			c.errors = append(c.errors, fmt.Errorf("invalid rounding mode: %s", roundingMode))
			return nil
		}
	}
	value := c.collectMathConstraintExpr(valueNode)
	return &types.PrecisionConstraint{
		Value:        value,
		RoundingMode: roundingMode,
	}
}
