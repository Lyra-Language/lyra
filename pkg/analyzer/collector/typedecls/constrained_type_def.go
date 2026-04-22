package typedecls

import (
	"fmt"

	"github.com/Lyra-Language/lyra/pkg/analyzer/collector/collctx"
	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/types"
	sitter "github.com/tree-sitter/go-tree-sitter"
)

func collectConstrainedTypeDeclaration(node *sitter.Node, ctx *collctx.Ctx) *ast.TypeDeclStmt {
	nameNode := node.ChildByFieldName("name")
	typeNode := node.ChildByFieldName("type")
	constraintsNode := node.ChildByFieldName("constraints")
	literalUnionNode := node.ChildByFieldName("literal_union")

	name := ctx.NodeText(nameNode)
	typeType := ctx.ParseType(typeNode)
	constraints := make([]types.Constraint, 0)
	if constraintsNode != nil {
		constraints = CollectConstraints(constraintsNode, ctx)
	}
	if literalUnionNode != nil {
		literalUnion := collectLiteralUnionConstraint(literalUnionNode, ctx)
		constraints = append(constraints, literalUnion)
		typeType = inferTypeFromValues(literalUnion.Values, ctx)
	}

	astNode := &ast.TypeDeclStmt{
		AstBase: ast.AstBase{Location: ctx.NodeLocation(node)},
		Name:    name,
		Type: &types.ConstrainedType{
			Name:        name,
			Type:        typeType,
			Constraints: constraints,
		},
	}

	if err := ctx.RegisterType(astNode); err != nil {
		ctx.AppendError(err)
	}

	return astNode
}

// CollectConstraints is exported because collector.go calls it from parseConstrainedType
// (which handles constrained_type as a type annotation, not just a declaration).
func CollectConstraints(node *sitter.Node, ctx *collctx.Ctx) []types.Constraint {
	constraints := make([]types.Constraint, 0)
	for i := uint(0); i < node.ChildCount(); i++ {
		child := node.Child(i)
		switch child.Kind() {
		case "range_constraint":
			constraints = append(constraints, collectRangeConstraint(child, ctx))
		case "precision_constraint":
			constraints = append(constraints, collectPrecisionConstraint(child, ctx))
		case "step_constraint":
			constraints = append(constraints, collectStepConstraint(child, ctx))
		case "pattern_constraint":
			constraints = append(constraints, collectPatternConstraint(child, ctx))
		case "literal_union_constraint":
			constraints = append(constraints, collectLiteralUnionConstraint(child, ctx))
		}
	}
	return constraints
}

func collectLiteralUnionConstraint(node *sitter.Node, ctx *collctx.Ctx) *types.LiteralUnionConstraint {
	values := make([]types.LiteralUnionValue, 0)
	for i := uint(0); i < node.ChildCount(); i++ {
		child := node.Child(i)
		if child.Kind() == "literal_val" {
			expr := ctx.CollectExpr(child)
			if v, ok := expr.(types.LiteralUnionValue); ok {
				values = append(values, v)
			}
		}
	}
	return &types.LiteralUnionConstraint{Values: values}
}

func inferTypeFromValues(values []types.LiteralUnionValue, ctx *collctx.Ctx) types.Type {
	if len(values) == 0 {
		ctx.AppendError(fmt.Errorf("literal union constraint must have at least one value"))
		return nil
	}
	return values[0].GetType()
}

func collectRangeConstraint(node *sitter.Node, ctx *collctx.Ctx) *types.RangeConstraint {
	var start types.MathConstraintExpr
	var comparator string
	var end types.MathConstraintExpr

	if startNode := node.ChildByFieldName("start"); startNode != nil {
		start = collectMathConstraintExpr(startNode, ctx)
	}
	if comparatorNode := node.ChildByFieldName("comparator"); comparatorNode != nil {
		comparator = ctx.NodeText(comparatorNode)
	}
	if endNode := node.ChildByFieldName("end"); endNode != nil {
		end = collectMathConstraintExpr(endNode, ctx)
	}

	if start == nil && end == nil {
		ctx.AppendError(fmt.Errorf("range constraint must have a start or end"))
	}
	return &types.RangeConstraint{
		Start:      start,
		Comparator: comparator,
		End:        end,
	}
}

func collectMathConstraintExpr(node *sitter.Node, ctx *collctx.Ctx) types.MathConstraintExpr {
	if node == nil {
		return nil
	}
	switch node.Kind() {
	case "constraint_math_expr":
		return collectMathConstraintExpr(node.Child(0), ctx)
	case "integer_literal":
		expr := ctx.CollectExpr(node)
		if expr == nil {
			return nil
		}
		litVal, ok := expr.(types.LiteralNumberValue)
		if !ok {
			return nil
		}
		return &types.MathConstraintLiteralExpr{Value: litVal, Type: litVal.GetType()}
	case "float_literal":
		expr := ctx.CollectExpr(node)
		if expr == nil {
			return nil
		}
		litVal, ok := expr.(types.LiteralNumberValue)
		if !ok {
			return nil
		}
		return &types.MathConstraintLiteralExpr{Value: litVal, Type: litVal.GetType()}
	case "identifier", "const_identifier":
		return &types.MathConstraintIdentifierExpr{
			Name:    ctx.NodeText(node),
			Type:    ctx.ParseType(node.ChildByFieldName("type")),
			IsConst: node.Kind() == "const_identifier",
		}
	case "constraint_binary_expression":
		return collectMathConstraintBinaryOpExpr(node, ctx)
	case "constraint_negation":
		return collectMathConstraintNegationExpr(node, ctx)
	}
	return nil
}

func collectMathConstraintNegationExpr(node *sitter.Node, ctx *collctx.Ctx) types.MathConstraintExpr {
	operandNode := node.ChildByFieldName("operand")
	operand := collectMathConstraintExpr(operandNode, ctx)
	if operand == nil {
		ctx.AppendError(fmt.Errorf("constraint negation must have an operand"))
		return nil
	}
	return &types.MathConstraintNegationExpr{Operand: operand}
}

func collectMathConstraintBinaryOpExpr(node *sitter.Node, ctx *collctx.Ctx) types.MathConstraintExpr {
	leftNode := node.ChildByFieldName("left")
	operatorNode := node.ChildByFieldName("operator")
	rightNode := node.ChildByFieldName("right")
	operator := types.MathConstraintBinaryOp(ctx.NodeText(operatorNode))
	left := collectMathConstraintExpr(leftNode, ctx)
	right := collectMathConstraintExpr(rightNode, ctx)
	if left == nil || right == nil {
		ctx.AppendError(fmt.Errorf("math constraint binary operator must have a left and right operand"))
		return nil
	}
	var binaryOperator types.MathConstraintBinaryOp
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
		ctx.AppendError(fmt.Errorf("invalid binary operator: %s", operator))
		return nil
	}
	return &types.MathConstraintBinaryOpExpr{
		Left:     left,
		Operator: operator,
		Right:    right,
	}
}

func collectPrecisionConstraint(node *sitter.Node, ctx *collctx.Ctx) *types.PrecisionConstraint {
	valueNode := node.ChildByFieldName("value")
	if valueNode == nil {
		ctx.AppendError(fmt.Errorf("precision constraint must have a value"))
		return nil
	}
	roundingMode := types.RoundingModeNearestEven
	if roundingModeNode := node.ChildByFieldName("rounding_mode"); roundingModeNode != nil {
		roundingMode = types.RoundingMode(ctx.NodeText(roundingModeNode))
		if roundingMode == "" {
			ctx.AppendError(fmt.Errorf("invalid rounding mode: %s", roundingMode))
			return nil
		}
	}
	return &types.PrecisionConstraint{
		Value:        collectMathConstraintExpr(valueNode, ctx),
		RoundingMode: roundingMode,
	}
}

func collectStepConstraint(node *sitter.Node, ctx *collctx.Ctx) *types.StepConstraint {
	valueNode := node.ChildByFieldName("value")
	if valueNode == nil {
		ctx.AppendError(fmt.Errorf("step constraint must have a value"))
		return nil
	}
	return &types.StepConstraint{Value: collectMathConstraintExpr(valueNode, ctx)}
}

func collectPatternConstraint(node *sitter.Node, ctx *collctx.Ctx) *types.PatternConstraint {
	patternNode := node.ChildByFieldName("pattern")
	if patternNode == nil {
		ctx.AppendError(fmt.Errorf("pattern constraint must have a pattern"))
		return nil
	}
	return &types.PatternConstraint{Pattern: ctx.NodeText(patternNode)}
}
