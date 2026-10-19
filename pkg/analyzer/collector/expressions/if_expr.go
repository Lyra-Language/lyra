package expressions

import (
	"github.com/Lyra-Language/lyra/pkg/analyzer/collector/collector_ctx"
	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/cst"
	sitter "github.com/tree-sitter/go-tree-sitter"
)

func collectIfExpr(node *sitter.Node, ctx *collector_ctx.Ctx, loc ast.Location) ast.Expression {
	ifConditionNode, ok := ctx.MustField(node, "condition")
	if !ok {
		return nil
	}

	thenBlockNode, ok := ctx.MustField(node, "then_block")
	if !ok {
		return nil
	}

	var elseExpr ast.Expression
	if elseBranchNode := cst.Field(node, "else_branch"); elseBranchNode != nil {
		if elseBranchNode.Kind() == "destructuring_if_declaration" {
			elseExpr = CollectElseIf(elseBranchNode, ctx)
		} else {
			elseExpr = CollectExpression(elseBranchNode, ctx)
		}
	}

	return &ast.IfExpr{
		ExprBase:  ast.ExprBase{AstBase: ast.AstBase{Location: loc}},
		Condition: CollectExpression(ifConditionNode, ctx),
		Then:      CollectExpression(thenBlockNode, ctx),
		Else:      elseExpr,
	}
}

// CollectElseIf collects an `if` or `if let` chained after an `else` — of an `if`
// or of an `if let` — as the one-statement block the braced spelling produces:
// `else if c { … }` and `else { if c { … } }` collect to the same AST, so nothing
// past the collector learns the chained form exists. It is needed where the two
// kinds differ: an `if let` is a statement, so it cannot be an `if`'s else
// expression bare, and an `if let`'s else is a block.
func CollectElseIf(node *sitter.Node, ctx *collector_ctx.Ctx) *ast.BlockExpr {
	loc := ctx.NodeLocation(node)
	scope := ctx.PushBlockScope()
	defer ctx.PopScope()
	var stmt ast.Statement
	if node.Kind() == "if_block_expr" {
		if expr := CollectExpression(node, ctx); expr != nil {
			stmt = &ast.ExpressionStmt{AstBase: ast.AstBase{Location: loc}, Expression: expr}
		}
	} else {
		stmt = ctx.CollectStatement(node)
	}
	statements := []ast.Statement{}
	if stmt != nil {
		statements = append(statements, stmt)
	}
	block := &ast.BlockExpr{
		ExprBase:   ast.ExprBase{AstBase: ast.AstBase{Location: loc}},
		Statements: statements,
	}
	ctx.ScopeTable.Set(block, scope)
	return block
}
