package statements

import (
	"fmt"

	"github.com/Lyra-Language/lyra/pkg/analyzer/collector/collector_ctx"
	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/cst"
	diag "github.com/Lyra-Language/lyra/pkg/diagnostic"
	sitter "github.com/tree-sitter/go-tree-sitter"
)

// CollectTupleAssignmentStmt collects `(p0, p1, …) = rhs` — several places written from
// one tuple — by desugaring it here into statements every later pass already handles:
//
//	{
//	  let (__tuple_L_C_0, __tuple_L_C_1, …) = rhs
//	  p0 = __tuple_L_C_0
//	  p1 = __tuple_L_C_1
//	  …
//	}
//
// The shape *is* the semantics. The right side is evaluated to a tuple first, so
// `(a, b) = (b, a)` reads both before writing either — which is what makes it a swap —
// and the places are then written left to right, each one's address computed once, by
// the assignment statement `=` already lowers for that kind of place. Arity and element
// types are checked by the destructuring `let`, writability by each assignment, so a
// misplaced element is refused by the same rule and message as its stand-alone spelling.
// The block scopes the synthesized names; the position in the name keeps two tuple
// assignments in one scope from colliding.
//
// A desugaring rather than a statement kind of its own because the passes that know an
// assignment — purity, ownership, use-after-move, captures, range analysis, the backend —
// number a dozen, and a new node would have to be taught to each. Nothing downstream
// knows tuple assignment exists.
//
// The target is parsed as an ordinary `tuple_literal` (see the grammar for why), so the
// collector is where a non-place element — a literal, a call, a constructor — is refused.
func CollectTupleAssignmentStmt(node *sitter.Node, ctx *collector_ctx.Ctx) ast.Statement {
	targetNode := cst.Field(node, "target")
	valueNode := cst.Field(node, "value")
	if targetNode == nil || valueNode == nil {
		ctx.AddError(node, diag.SeverityError, "tuple_assignment: missing target or value")
		return nil
	}
	if cst.Field(targetNode, "tuple_name") != nil {
		ctx.AddError(targetNode, diag.SeverityError,
			"cannot assign to a constructor: a tuple assignment's target is a parenthesized list of places, `(a, b) = …`")
		return nil
	}

	// Places and value are collected in the enclosing scope: they name what already
	// exists there. Only the synthesized bindings live in the block.
	var placeNodes []*sitter.Node
	var places []ast.Expression
	for i := uint(0); i < targetNode.ChildCount(); i++ {
		child := targetNode.Child(i)
		if child.Kind() == "tuple_value" {
			placeNodes = append(placeNodes, child)
			places = append(places, ctx.CollectExpr(child.Child(0)))
		}
	}
	if len(places) == 0 {
		ctx.AddError(targetNode, diag.SeverityError, "a tuple assignment needs at least one place to write")
		return nil
	}
	value := ctx.CollectExpr(valueNode)

	loc := ctx.NodeLocation(node)
	scope := ctx.PushBlockScope()
	defer ctx.PopScope()

	names := make([]string, len(places))
	elements := make([]ast.Pattern, len(places))
	for i := range places {
		names[i] = fmt.Sprintf("__tuple_%d_%d_%d", loc.StartLine, loc.StartCol, i)
		elements[i] = &ast.IdentifierPattern{
			PatternBase: ast.PatternBase{AstBase: ast.AstBase{Location: ctx.NodeLocation(placeNodes[i])}},
			Name:        names[i],
		}
	}
	decl := &ast.DestructuringDeclStmt{
		AstBase: ast.AstBase{Location: loc},
		Keyword: "let",
		Pattern: &ast.TuplePattern{
			PatternBase: ast.PatternBase{AstBase: ast.AstBase{Location: ctx.NodeLocation(targetNode)}},
			Elements:    elements,
		},
		Value: value,
	}
	for _, name := range names {
		ctx.RegisterDestructuredName(name, decl)
	}

	stmts := []ast.Statement{decl}
	for i, place := range places {
		placeLoc := ctx.NodeLocation(placeNodes[i])
		read := &ast.IdentifierExpr{
			ExprBase: ast.ExprBase{AstBase: ast.AstBase{Location: placeLoc}},
			Name:     names[i],
		}
		var stmt ast.Statement
		switch p := place.(type) {
		case *ast.IdentifierExpr:
			stmt = &ast.VarReassignmentStmt{AstBase: ast.AstBase{Location: placeLoc}, Name: p.Name, Value: read}
		case *ast.MemberExpr, *ast.IndexExpr:
			stmt = &ast.LValueAssignmentStmt{AstBase: ast.AstBase{Location: placeLoc}, Target: p, Value: read}
		case *ast.DerefExpr:
			stmt = &ast.DerefAssignmentStmt{AstBase: ast.AstBase{Location: placeLoc}, Target: *p, Value: read}
		default:
			ctx.AddError(placeNodes[i], diag.SeverityError,
				"cannot assign to this: each element of a tuple assignment's target must be a place — a name, a member `p.x`, an index `xs[i]` or a deref `p^`")
			return nil
		}
		stmts = append(stmts, stmt)
	}

	block := &ast.BlockExpr{
		ExprBase:   ast.ExprBase{AstBase: ast.AstBase{Location: loc}},
		Statements: stmts,
	}
	ctx.RecordScope(block, scope)
	return &ast.ExpressionStmt{AstBase: ast.AstBase{Location: loc}, Expression: block}
}
