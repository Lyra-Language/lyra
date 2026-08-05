package expressions

import (
	"github.com/Lyra-Language/lyra/pkg/analyzer/collector/collector_ctx"
	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/cst"
	sitter "github.com/tree-sitter/go-tree-sitter"
)

func collectNegationExpr(node *sitter.Node, ctx *collector_ctx.Ctx, loc ast.Location) *ast.NegationExpr {
	return &ast.NegationExpr{
		ExprBase: ast.ExprBase{AstBase: ast.AstBase{Location: loc}},
		Operand:  CollectExpression(cst.Field(node, "operand"), ctx),
	}
}

func collectBitwiseNotExpr(node *sitter.Node, ctx *collector_ctx.Ctx, loc ast.Location) *ast.BitwiseNotExpr {
	return &ast.BitwiseNotExpr{
		ExprBase: ast.ExprBase{AstBase: ast.AstBase{Location: loc}},
		Operand:  CollectExpression(cst.Field(node, "operand"), ctx),
	}
}

func collectAddressOfExpr(node *sitter.Node, ctx *collector_ctx.Ctx, loc ast.Location) *ast.AddressOfExpr {
	return &ast.AddressOfExpr{
		ExprBase: ast.ExprBase{AstBase: ast.AstBase{Location: loc}},
		Operand:  CollectExpression(cst.Field(node, "operand"), ctx),
		IsMut:    cst.Field(node, "is_mut") != nil,
	}
}

func collectDerefExpr(node *sitter.Node, ctx *collector_ctx.Ctx, loc ast.Location) *ast.DerefExpr {
	return &ast.DerefExpr{
		ExprBase: ast.ExprBase{AstBase: ast.AstBase{Location: loc}},
		Operand:  CollectExpression(cst.Field(node, "operand"), ctx),
	}
}
