package expressions

import (
	"github.com/Lyra-Language/lyra/pkg/analyzer/collector/collctx"
	"github.com/Lyra-Language/lyra/pkg/ast"
	sitter "github.com/tree-sitter/go-tree-sitter"
)

func CollectMatchExpression(node *sitter.Node, ctx *collctx.Ctx, loc ast.Location) *ast.MatchExpr {
	value := CollectExpression(node.ChildByFieldName("value"), ctx)
	if value == nil {
		ctx.AddError(node, collctx.SeverityError, "CollectMatchExpression: value is nil")
		return nil
	}
	matchArms := CollectMatchArms(node, ctx)
	if matchArms == nil {
		ctx.AddError(node, collctx.SeverityError, "CollectMatchExpression: match arms are nil")
		return nil
	}
	return &ast.MatchExpr{
		ExprBase:  ast.ExprBase{AstBase: ast.AstBase{Location: loc}},
		Value:     value,     // guaranteed to be non-nil
		MatchArms: matchArms, // guaranteed to be non-nil
	}
}

func CollectMatchArms(node *sitter.Node, ctx *collctx.Ctx) []ast.MatchArm {
	matchArms := []ast.MatchArm{}
	for i := uint(0); i < node.ChildCount(); i++ {
		if node.Child(i).Kind() == "match_arm" {
			matchArm := CollectMatchArm(node.Child(i), ctx)
			if matchArm == nil {
				ctx.AddError(node, collctx.SeverityError, "CollectMatchArms: match arm is nil")
				return nil
			}
			matchArms = append(matchArms, *matchArm)
		}
	}
	return matchArms
}

func CollectMatchArm(node *sitter.Node, ctx *collctx.Ctx) *ast.MatchArm {
	patternNode := node.ChildByFieldName("pattern")
	if patternNode == nil {
		ctx.AddError(node, collctx.SeverityError, "CollectMatchArm: pattern is nil")
		return nil
	}
	pattern := ctx.CollectPattern(patternNode)
	guardNode := node.ChildByFieldName("guard")
	var guard *ast.GuardExpr = nil
	if guardNode != nil {
		guard = collectGuard(guardNode, ctx)
	}
	body := CollectExpression(node.ChildByFieldName("body"), ctx)
	if body == nil {
		ctx.AddError(node, collctx.SeverityError, "CollectMatchArm: body is nil")
		return nil
	}
	return &ast.MatchArm{
		Pattern: pattern,
		Guard:   guard,
		Body:    body,
	}
}
