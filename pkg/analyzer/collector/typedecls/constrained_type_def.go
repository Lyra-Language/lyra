package typedecls

import (
	"github.com/Lyra-Language/lyra/pkg/analyzer/collector/collector_ctx"
	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/cst"
	diag "github.com/Lyra-Language/lyra/pkg/diagnostic"
	"github.com/Lyra-Language/lyra/pkg/types"
	sitter "github.com/tree-sitter/go-tree-sitter"
)

func collectConstrainedTypeDeclaration(node *sitter.Node, ctx *collector_ctx.Ctx) *ast.TypeDeclStmt {
	nameNode := cst.Field(node, "name")
	typeNode := cst.Field(node, "type")
	constraintsNode := cst.Field(node, "constraints")
	literalUnionNode := cst.Field(node, "literal_union")

	name := ctx.NodeText(nameNode)
	typeType := ctx.ParseType(typeNode)
	constraints := []types.Constraint{}
	if constraintsNode != nil {
		constraints = CollectConstraints(constraintsNode, ctx)
	}
	if literalUnionNode != nil {
		literalUnion := collectLiteralUnionConstraint(literalUnionNode, ctx)
		constraints = append(constraints, literalUnion)
		typeType = inferTypeFromValues(literalUnion.Values, literalUnionNode, ctx)
	}
	// The step's well-formedness is checked here rather than in collectStepConstraint
	// because it needs the newtype's *base* type, which only this function has —
	// a fractional step is fine on `f32` and meaningless on `u8`. The rule itself
	// is types.InvalidStepReason, shared with an expression range's `:step` so the
	// two spellings cannot disagree about which steps are legal.
	checkStepConstraints(constraints, typeType, constraintsNode, ctx)

	// A generic `newtype` — `newtype Meters<t> = t`. The grammar had no slot for the
	// parameters until 08/07, so they landed in an ERROR node and the declaration
	// collected with them silently dropped; the golden file for that case recorded the
	// drop as if it were the intended output.
	var genericParams []ast.GenericParam
	if gp := cst.Field(node, "generic_parameters"); gp != nil {
		genericParams = ctx.CollectGenericParams(gp)
	}

	astNode := &ast.TypeDeclStmt{
		AstBase:       ast.AstBase{Location: ctx.NodeLocation(node)},
		Name:          name,
		NameLocation:  ctx.NodeLocation(nameNode),
		GenericParams: genericParams,
		Type: &types.ConstrainedType{
			Name:        name,
			Type:        typeType,
			Constraints: constraints,
		},
	}

	if err := ctx.RegisterType(astNode); err != nil {
		ctx.AddError(node, diag.SeverityError, "failed to register constrained type %q: %v", name, err)
	}

	return astNode
}

// CollectConstraints is exported because collector.go calls it from parseConstrainedType
// (which handles constrained_type as a type annotation, not just a declaration).
func CollectConstraints(node *sitter.Node, ctx *collector_ctx.Ctx) []types.Constraint {
	constraints := []types.Constraint{}
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

func collectLiteralUnionConstraint(node *sitter.Node, ctx *collector_ctx.Ctx) *types.LiteralUnionConstraint {
	values := []types.LiteralUnionValue{}
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

func inferTypeFromValues(values []types.LiteralUnionValue, node *sitter.Node, ctx *collector_ctx.Ctx) types.Type {
	if len(values) == 0 {
		ctx.AddError(node, diag.SeverityError, "literal union constraint must have at least one value")
		return nil
	}
	return values[0].GetType()
}

func collectRangeConstraint(node *sitter.Node, ctx *collector_ctx.Ctx) *types.RangeConstraint {
	var start types.MathConstraintExpr
	var comparator string
	var end types.MathConstraintExpr

	// `end_operator`, not `comparator`, and one `range_end_operator` node kind
	// rather than this rule's own `less_than_comparator`/`equal_to_comparator` —
	// the three range grammars share one notation now, and the operator is
	// enforced by the same check they all use (lyra-E032).
	comparator = ctx.RangeEndOperator(node, "range constraint")

	if startNode := cst.Field(node, "start"); collector_ctx.RangeBound(startNode) {
		start = collectMathConstraintExpr(startNode, ctx)
	}
	if endNode := cst.Field(node, "end"); collector_ctx.RangeBound(endNode) {
		end = collectMathConstraintExpr(endNode, ctx)
	}

	// The grammar makes `range(..)` unspellable — rangeBounds' open mode requires
	// one bound — so this is now a guard against a recovered parse rather than the
	// primary rule. Kept: tree-sitter can *insert* a bound to keep going, and
	// RangeBound above is what turns that back into "absent".
	if start == nil && end == nil {
		ctx.AddError(node, diag.SeverityError, "range constraint must have a start or an end bound")
	}
	return &types.RangeConstraint{
		Start:      start,
		Comparator: comparator,
		End:        end,
	}
}

func collectMathConstraintExpr(node *sitter.Node, ctx *collector_ctx.Ctx) types.MathConstraintExpr {
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
			Type:    ctx.ParseType(cst.Field(node, "type")),
			IsConst: node.Kind() == "const_identifier",
		}
	case "constraint_binary_expr":
		return collectMathConstraintBinaryOpExpr(node, ctx)
	case "constraint_negation":
		return collectMathConstraintNegationExpr(node, ctx)
	}
	return nil
}

func collectMathConstraintNegationExpr(node *sitter.Node, ctx *collector_ctx.Ctx) types.MathConstraintExpr {
	operandNode := cst.Field(node, "operand")
	operand := collectMathConstraintExpr(operandNode, ctx)
	if operand == nil {
		ctx.AddError(node, diag.SeverityError, "constraint negation must have an operand")
		return nil
	}
	return &types.MathConstraintNegationExpr{Operand: operand}
}

func collectMathConstraintBinaryOpExpr(node *sitter.Node, ctx *collector_ctx.Ctx) types.MathConstraintExpr {
	leftNode := cst.Field(node, "left")
	operatorNode := cst.Field(node, "operator")
	rightNode := cst.Field(node, "right")
	operator := types.MathConstraintBinaryOp(ctx.NodeText(operatorNode))
	left := collectMathConstraintExpr(leftNode, ctx)
	right := collectMathConstraintExpr(rightNode, ctx)
	if left == nil || right == nil {
		ctx.AddError(node, diag.SeverityError, "math constraint binary operator must have a left and right operand")
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
		ctx.AddError(node, diag.SeverityError, "invalid binary operator: %s", operator)
		return nil
	}
	return &types.MathConstraintBinaryOpExpr{
		Left:     left,
		Operator: operator,
		Right:    right,
	}
}

func collectPrecisionConstraint(node *sitter.Node, ctx *collector_ctx.Ctx) *types.PrecisionConstraint {
	valueNode := cst.Field(node, "value")
	if valueNode == nil {
		ctx.AddError(node, diag.SeverityError, "precision constraint must have a value")
		return nil
	}
	roundingMode := types.RoundingModeNearestEven
	if roundingModeNode := cst.Field(node, "rounding_mode"); roundingModeNode != nil {
		roundingMode = types.RoundingMode(ctx.NodeText(roundingModeNode))
		if roundingMode == "" {
			ctx.AddError(roundingModeNode, diag.SeverityError, "invalid rounding mode: %s", roundingMode)
			return nil
		}
	}
	return &types.PrecisionConstraint{
		Value:        collectMathConstraintExpr(valueNode, ctx),
		RoundingMode: roundingMode,
	}
}

// checkStepConstraints validates each `step()` against the newtype's base type,
// using the same rule an expression range's `:step` is held to
// (types.InvalidStepReason). Until this existed a step constraint was collected
// and checked by nothing at all, while the expression spelling was checked for
// type compatibility — the two spellings of one idea disagreeing about what a
// legal step even is.
//
// A step built from an identifier or an arithmetic expression folds to no
// constant and is left alone: it is legal and simply not decidable here.
func checkStepConstraints(constraints []types.Constraint, base types.Type, node *sitter.Node, ctx *collector_ctx.Ctx) {
	if node == nil {
		return
	}
	integerDomain := types.StepDomainIsInteger(base)
	for _, c := range constraints {
		step, ok := c.(*types.StepConstraint)
		if !ok {
			continue
		}
		v, isConst := constraintExprFloat(step.Value)
		if !isConst {
			continue
		}
		if reason := types.InvalidStepReason(v, integerDomain); reason != "" {
			ctx.AddErrorCoded(node, diag.SeverityError, diag.CodeInvalidRangeStep,
				"invalid step constraint `%s` on %s: %s", step.GetName(), base, reason)
		}
	}
}

// constraintExprFloat folds a constraint expression to a float64 when it is a
// literal (possibly negated). The counterpart of the typechecker's
// constNumericFromExpr, over the constraint expression tree rather than the AST.
func constraintExprFloat(e types.MathConstraintExpr) (float64, bool) {
	switch v := e.(type) {
	case *types.MathConstraintLiteralExpr:
		if v.Value == nil {
			return 0, false
		}
		if f, ok := v.Value.Float64(); ok {
			return f, true
		}
		if i, ok := v.Value.Int64(); ok {
			return float64(i), true
		}
	case *types.MathConstraintNegationExpr:
		if inner, ok := constraintExprFloat(v.Operand); ok {
			return -inner, true
		}
	}
	return 0, false
}

func collectStepConstraint(node *sitter.Node, ctx *collector_ctx.Ctx) *types.StepConstraint {
	valueNode := cst.Field(node, "value")
	if valueNode == nil {
		ctx.AddError(node, diag.SeverityError, "step constraint must have a value")
		return nil
	}
	return &types.StepConstraint{Value: collectMathConstraintExpr(valueNode, ctx)}
}

func collectPatternConstraint(node *sitter.Node, ctx *collector_ctx.Ctx) *types.PatternConstraint {
	patternNode := cst.Field(node, "pattern")
	if patternNode == nil {
		ctx.AddError(node, diag.SeverityError, "pattern constraint must have a pattern")
		return nil
	}
	return &types.PatternConstraint{Pattern: ctx.NodeText(patternNode)}
}
