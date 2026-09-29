package expressions

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/Lyra-Language/lyra/pkg/analyzer/collector/collector_ctx"
	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/cst"
	diag "github.com/Lyra-Language/lyra/pkg/diagnostic"
	"github.com/Lyra-Language/lyra/pkg/types"
	sitter "github.com/tree-sitter/go-tree-sitter"
)

func collectFunctionCallExpr(node *sitter.Node, ctx *collector_ctx.Ctx, loc ast.Location) ast.Expression {
	// `m?.f(a)` is a call whose callee is an optional member: the whole call is what
	// reads through the Maybe, so it is desugared here rather than at the callee — the
	// callee alone would become `(match …)(a)`.
	if callee := cst.Field(node, "function"); callee != nil && callee.Kind() == "optional_member_expr" {
		if property := cst.Field(callee, "property"); property != nil {
			object := CollectExpression(cst.Field(callee, "object"), ctx)
			method := CollectIdentifierExpr(property, property.Kind() == "const_identifier", ctx.NodeLocation(property), ctx)
			if method != nil {
				calleeLoc := ctx.NodeLocation(callee)
				return optionalChain(object, ctx.NodeLocation(property), loc, func(payload ast.Expression) ast.Expression {
					return &ast.FunctionCallExpr{
						ExprBase: ast.ExprBase{AstBase: ast.AstBase{Location: loc}},
						Function: &ast.MemberExpr{
							ExprBase: ast.ExprBase{AstBase: ast.AstBase{Location: calleeLoc}},
							Object:   payload,
							Property: *method,
						},
						GenericArguments: collectCallGenericArguments(node, ctx),
						Arguments:        collectArgumentList(cst.Field(node, "arguments"), ctx),
					}
				})
			}
		}
	}
	return &ast.FunctionCallExpr{
		ExprBase:         ast.ExprBase{AstBase: ast.AstBase{Location: loc}},
		Function:         CollectExpression(cst.Field(node, "function"), ctx),
		GenericArguments: collectCallGenericArguments(node, ctx),
		Arguments:        collectArgumentList(cst.Field(node, "arguments"), ctx),
	}
}

func collectCallGenericArguments(node *sitter.Node, ctx *collector_ctx.Ctx) []types.Type {
	genericArgumentsNode := cst.Field(node, "generic_arguments")
	if genericArgumentsNode == nil {
		return nil
	}
	genericArguments := []types.Type{}
	for i := uint(0); i < genericArgumentsNode.ChildCount(); i++ {
		child := genericArgumentsNode.Child(i)
		if child.IsNamed() && !cst.IsComment(child) {
			genericArguments = append(genericArguments, ctx.ParseType(child))
		}
	}
	return genericArguments
}

func collectArgumentList(node *sitter.Node, ctx *collector_ctx.Ctx) []ast.Expression {
	arguments := []ast.Expression{}
	for i := uint(0); i < node.ChildCount(); i++ {
		child := node.Child(i)
		if child.IsNamed() && !cst.IsComment(child) {
			arguments = appendCollected(arguments, CollectExpression(child, ctx))
		}
	}
	return arguments
}

func collectMemberExpr(node *sitter.Node, ctx *collector_ctx.Ctx, loc ast.Location, optional bool) ast.Expression {
	object := CollectExpression(cst.Field(node, "object"), ctx)
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
		}
	}
	propertyNode := cst.Field(node, "property")
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
	if optional {
		return optionalChain(object, ctx.NodeLocation(propertyNode), loc, func(payload ast.Expression) ast.Expression {
			return &ast.MemberExpr{
				ExprBase: ast.ExprBase{AstBase: ast.AstBase{Location: loc}},
				Object:   payload,
				Property: *property,
			}
		})
	}
	return &ast.MemberExpr{
		ExprBase: ast.ExprBase{AstBase: ast.AstBase{Location: loc}},
		Object:   object,
		Property: *property,
	}
}

func collectTupleIndexExpr(node *sitter.Node, ctx *collector_ctx.Ctx, loc ast.Location) ast.Expression {
	object := CollectExpression(cst.Field(node, "object"), ctx)
	// An inert placeholder (index 0), never a nil node — same typed-nil hazard as
	// collectMemberExpr; the emitted error keeps the program from compiling.
	placeholder := func() ast.Expression {
		return &ast.TupleIndexExpr{
			ExprBase: ast.ExprBase{AstBase: ast.AstBase{Location: loc}},
			Object:   object,
			Index:    0,
		}
	}
	indexNode := cst.Field(node, "index")
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
	traitNameNode := cst.Field(node, "trait_name")
	methodNode := cst.Field(node, "method")
	if traitNameNode == nil || methodNode == nil {
		ctx.AddError(node, diag.SeverityError, "trait method path missing trait name or method")
		// An inert placeholder, never a nil node (typed-nil hazard); the error keeps
		// the program from compiling.
		return &ast.TraitMethodPathExpr{
			ExprBase: ast.ExprBase{AstBase: ast.AstBase{Location: loc}},
			Method:   ast.IdentifierExpr{ExprBase: ast.ExprBase{AstBase: ast.AstBase{Location: loc}}},
		}
	}
	// Recorded as the impl head and a bound are: a written trait name, which the import
	// gate (checkWrittenTypeNames) and the editor's position lookups both read.
	ctx.RecordTypeRef(ctx.NodeText(traitNameNode), ctx.NodeLocation(traitNameNode))
	return &ast.TraitMethodPathExpr{
		ExprBase:  ast.ExprBase{AstBase: ast.AstBase{Location: loc}},
		TraitName: ctx.NodeText(traitNameNode),
		Method:    *CollectIdentifierExpr(methodNode, false, ctx.NodeLocation(methodNode), ctx),
	}
}

func collectIndexExpr(node *sitter.Node, ctx *collector_ctx.Ctx, loc ast.Location, optional bool) ast.Expression {
	object := CollectExpression(cst.Field(node, "object"), ctx)
	indexNode := cst.Field(node, "index")
	index := CollectExpression(indexNode, ctx)
	if optional && indexNode != nil {
		return optionalChain(object, ctx.NodeLocation(indexNode), loc, func(payload ast.Expression) ast.Expression {
			return &ast.IndexExpr{
				ExprBase: ast.ExprBase{AstBase: ast.AstBase{Location: loc}},
				Object:   payload,
				Index:    index,
			}
		})
	}
	return &ast.IndexExpr{
		ExprBase: ast.ExprBase{AstBase: ast.AstBase{Location: loc}},
		Object:   object,
		Index:    index,
	}
}

// OptionalChainLift is the prelude function a safe-navigation arm wraps its result in:
// `Some` on a plain value, the identity on a Maybe (std/prelude/maybe.lyra).
const OptionalChainLift = "__optional_chain"

// optionalChain is what `m?.x`, `m?.f(a)` and `m?[i]` mean — safe navigation, reading
// through a Maybe: `None` stays `None`, and a `Some` has the access applied to its
// payload. Desugared here rather than given a node of its own, so every later pass sees
// an ordinary match (CLAUDE.md rule 8: a new expression kind is a case in every walk):
//
//	match m { Some(__opt_L_C) => __optional_chain(__opt_L_C.x), None => None }
//
// `__optional_chain` is what keeps `a?.b` a `Maybe<B>` rather than a `Maybe<Maybe<B>>`
// when the field is itself a Maybe, so `a?.b?.c` chains — each `?.` reads through one
// Maybe (Kotlin's rule; there is no Swift-style short-circuit of a whole chain, so
// `a?.b.c` is a `.c` on a Maybe and refused). A method call's arguments sit inside the
// Some arm, evaluated only when there is a value.
//
// The payload's name is stamped with the position of what follows the `?.` or `?[`,
// which no other chain shares — two chains nested in one another (`m?.f(n?.g)`) then
// bind different names, and neither shadows the other.
func optionalChain(object ast.Expression, at, loc ast.Location, access func(payload ast.Expression) ast.Expression) ast.Expression {
	name := fmt.Sprintf("__opt_%d_%d", at.StartLine, at.StartCol)
	base := func() ast.ExprBase { return ast.ExprBase{AstBase: ast.AstBase{Location: loc}} }
	patBase := ast.PatternBase{AstBase: ast.AstBase{Location: loc}}
	payload := &ast.IdentifierExpr{ExprBase: base(), Name: name}
	return &ast.MatchExpr{
		ExprBase:      base(),
		Scrutinee:     object,
		OptionalChain: true,
		MatchArms: []ast.MatchArm{
			{
				Pattern: &ast.DataPattern{
					PatternBase: patBase,
					Name:        "Some",
					Pattern: &ast.TuplePattern{
						PatternBase: patBase,
						Elements:    []ast.Pattern{&ast.IdentifierPattern{PatternBase: patBase, Name: name}},
					},
				},
				Body: &ast.FunctionCallExpr{
					ExprBase:  base(),
					Function:  &ast.IdentifierExpr{ExprBase: base(), Name: OptionalChainLift},
					Arguments: []ast.Expression{access(payload)},
				},
			},
			{
				Pattern: &ast.DataPattern{PatternBase: patBase, Name: "None"},
				Body:    &ast.DataConstructorExpr{ExprBase: base(), Constructor: "None"},
			},
		},
	}
}

func collectTryExpr(node *sitter.Node, ctx *collector_ctx.Ctx, loc ast.Location) *ast.TryExpr {
	return &ast.TryExpr{
		ExprBase: ast.ExprBase{AstBase: ast.AstBase{Location: loc}},
		Operand:  CollectExpression(cst.Field(node, "operand"), ctx),
	}
}
