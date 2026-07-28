package expressions

import (
	"strconv"
	"strings"

	"github.com/Lyra-Language/lyra/pkg/analyzer/collector/collector_ctx"
	"github.com/Lyra-Language/lyra/pkg/ast"
	diag "github.com/Lyra-Language/lyra/pkg/diagnostic"
	"github.com/Lyra-Language/lyra/pkg/types"
	sitter "github.com/tree-sitter/go-tree-sitter"
)

func collectFunctionCallExpr(node *sitter.Node, ctx *collector_ctx.Ctx, loc ast.Location) *ast.FunctionCallExpr {
	return &ast.FunctionCallExpr{
		ExprBase:         ast.ExprBase{AstBase: ast.AstBase{Location: loc}},
		Function:         CollectExpression(node.ChildByFieldName("function"), ctx),
		GenericArguments: collectCallGenericArguments(node, ctx),
		Arguments:        collectArgumentList(node.ChildByFieldName("arguments"), ctx),
	}
}

func collectCallGenericArguments(node *sitter.Node, ctx *collector_ctx.Ctx) []types.Type {
	genericArgumentsNode := node.ChildByFieldName("generic_arguments")
	if genericArgumentsNode == nil {
		return nil
	}
	genericArguments := []types.Type{}
	for i := uint(0); i < genericArgumentsNode.ChildCount(); i++ {
		child := genericArgumentsNode.Child(i)
		if child.IsNamed() {
			genericArguments = append(genericArguments, ctx.ParseType(child))
		}
	}
	return genericArguments
}

func collectArgumentList(node *sitter.Node, ctx *collector_ctx.Ctx) []ast.Expression {
	arguments := []ast.Expression{}
	for i := uint(0); i < node.ChildCount(); i++ {
		child := node.Child(i)
		if child.IsNamed() {
			arguments = append(arguments, CollectExpression(child, ctx))
		}
	}
	return arguments
}

func collectMemberExpr(node *sitter.Node, ctx *collector_ctx.Ctx, loc ast.Location, optional bool) ast.Expression {
	object := CollectExpression(node.ChildByFieldName("object"), ctx)
	// A member expression with no property (`f.`, a natural mid-edit state, or the
	// callee of `f.()`) must still yield an inert placeholder node, never a nil: a
	// nil `ast.Expression` slips past `== nil` checks and crashes a later pass — e.g.
	// inferFunctionCallExpr calling `.GetName()` on the nil callee of `f.()`. The
	// emitted error keeps the program from compiling. (See collector.go's "Never
	// return a nil expression node into the AST".)
	placeholder := func() ast.Expression {
		return &ast.MemberExpr{
			ExprBase: ast.ExprBase{AstBase: ast.AstBase{Location: loc}},
			Object:   object,
			Property: ast.IdentifierExpr{ExprBase: ast.ExprBase{AstBase: ast.AstBase{Location: loc}}},
			Optional: optional,
		}
	}
	propertyNode := node.ChildByFieldName("property")
	if propertyNode == nil {
		ctx.AddError(node, diag.SeverityError, "member expression missing property")
		return placeholder()
	}
	isConst := propertyNode.Kind() == "const_identifier"
	property := CollectIdentifierExpr(propertyNode, isConst, ctx.NodeLocation(propertyNode), ctx)
	if property == nil {
		ctx.AddError(node, diag.SeverityError, "could not parse member expression property")
		return placeholder()
	}
	return &ast.MemberExpr{
		ExprBase: ast.ExprBase{AstBase: ast.AstBase{Location: loc}},
		Object:   object,
		Property: *property,
		Optional: optional,
	}
}

func collectTupleIndexExpr(node *sitter.Node, ctx *collector_ctx.Ctx, loc ast.Location) ast.Expression {
	object := CollectExpression(node.ChildByFieldName("object"), ctx)
	// An inert placeholder (index 0), never a nil node — same typed-nil hazard as
	// collectMemberExpr; the emitted error keeps the program from compiling.
	placeholder := func() ast.Expression {
		return &ast.TupleIndexExpr{
			ExprBase: ast.ExprBase{AstBase: ast.AstBase{Location: loc}},
			Object:   object,
			Index:    0,
		}
	}
	indexNode := node.ChildByFieldName("index")
	if indexNode == nil {
		ctx.AddError(node, diag.SeverityError, "tuple index expression missing index")
		return placeholder()
	}
	// The index is a decimal_int token (`[0-9][0-9_]*`), so strip any digit
	// separators before parsing. A value too large to be a Go int is not a
	// plausible tuple arity, so it's a parse-level error rather than a panic.
	indexText := strings.ReplaceAll(ctx.NodeText(indexNode), "_", "")
	index, err := strconv.Atoi(indexText)
	if err != nil {
		ctx.AddError(indexNode, diag.SeverityError, "invalid tuple index %q", ctx.NodeText(indexNode))
		return placeholder()
	}
	return &ast.TupleIndexExpr{
		ExprBase: ast.ExprBase{AstBase: ast.AstBase{Location: loc}},
		Object:   object,
		Index:    index,
	}
}

func collectTraitMethodPathExpr(node *sitter.Node, ctx *collector_ctx.Ctx, loc ast.Location) ast.Expression {
	traitNameNode := node.ChildByFieldName("trait_name")
	methodNode := node.ChildByFieldName("method")
	if traitNameNode == nil || methodNode == nil {
		ctx.AddError(node, diag.SeverityError, "trait method path missing trait name or method")
		// An inert placeholder, never a nil node (typed-nil hazard); the error keeps
		// the program from compiling.
		return &ast.TraitMethodPathExpr{
			ExprBase: ast.ExprBase{AstBase: ast.AstBase{Location: loc}},
			Method:   ast.IdentifierExpr{ExprBase: ast.ExprBase{AstBase: ast.AstBase{Location: loc}}},
		}
	}
	return &ast.TraitMethodPathExpr{
		ExprBase:  ast.ExprBase{AstBase: ast.AstBase{Location: loc}},
		TraitName: ctx.NodeText(traitNameNode),
		Method:    *CollectIdentifierExpr(methodNode, false, ctx.NodeLocation(methodNode), ctx),
	}
}

func collectIndexExpr(node *sitter.Node, ctx *collector_ctx.Ctx, loc ast.Location, optional bool) *ast.IndexExpr {
	return &ast.IndexExpr{
		ExprBase: ast.ExprBase{AstBase: ast.AstBase{Location: loc}},
		Object:   CollectExpression(node.ChildByFieldName("object"), ctx),
		Index:    CollectExpression(node.ChildByFieldName("index"), ctx),
		Optional: optional,
	}
}

func collectTryExpr(node *sitter.Node, ctx *collector_ctx.Ctx, loc ast.Location) *ast.TryExpr {
	return &ast.TryExpr{
		ExprBase: ast.ExprBase{AstBase: ast.AstBase{Location: loc}},
		Operand:  CollectExpression(node.ChildByFieldName("operand"), ctx),
	}
}
