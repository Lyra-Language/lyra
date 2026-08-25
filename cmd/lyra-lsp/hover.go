package main

import (
	"fmt"
	"strings"

	"github.com/owenrumney/go-lsp/lsp"

	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/types"
)

// findExprAtPos returns the innermost expression whose source span contains the given
// 1-based line/col, or nil if none does. Hover, go-to-definition, references, rename and
// document-highlight all start here, so a construct this cannot reach is one the editor
// is silent inside.
//
// **This used to be three hand-written switches** — findExprInStmt over statement kinds,
// findInChildren over expression kinds, and findExprInExpr joining them — mirroring
// ast.walkStmtChildren and ast.walkExprChildren by hand. The expression half was
// registered in pkg/ast's exhaustiveness test and kept in step; the statement half never
// was, and had fallen eight kinds behind: WithStmt, both destructuring-if forms,
// LValueAssignmentStmt, BreakStmt, DestructuringDeclStmt and — the two that matter most —
// TraitDeclStmt and TraitImplStmt, so navigation was dead inside every trait-impl method
// body and every trait default method in every program.
//
// Walking with ast.WalkStmt/ast.WalkExpr retires the mirror rather than re-checking it:
// there is now no second list of node kinds to fall behind.
//
// The walk does not prune at a node that fails to contain the position, and takes the
// **narrowest** containing span rather than the first one found. That is deliberate:
// pruning makes any node with an unset Location (hazard 14) invisible along with its
// whole subtree, and picking by span keeps the answer independent of traversal order.
// Equal spans resolve to the later-visited node, which is the child — the same
// children-first preference the recursive version had.
func findExprAtPos(program *ast.Program, line, col int) ast.Expression {
	if program == nil {
		return nil
	}
	var best ast.Expression
	onExpr := func(e ast.Expression) bool {
		loc := e.GetLocation()
		if containsPos(loc, line, col) && (best == nil || spanWithin(loc, best.GetLocation())) {
			best = e
		}
		return true
	}
	for _, node := range program.Statements {
		if stmt, ok := node.(ast.Statement); ok {
			ast.WalkStmt(stmt, nil, onExpr)
		}
	}
	return best
}

// spanWithin reports whether inner's span lies within outer's, inclusive — so two nodes
// with the same span are each within the other, and the later-visited one wins.
func spanWithin(inner, outer ast.Location) bool {
	if inner.StartLine < outer.StartLine ||
		(inner.StartLine == outer.StartLine && inner.StartCol < outer.StartCol) {
		return false
	}
	if inner.EndLine > outer.EndLine ||
		(inner.EndLine == outer.EndLine && inner.EndCol > outer.EndCol) {
		return false
	}
	return true
}

// containsPos reports whether loc (1-based) contains the given 1-based line/col.
func containsPos(loc ast.Location, line, col int) bool {
	if line < loc.StartLine || line > loc.EndLine {
		return false
	}
	if line == loc.StartLine && col < loc.StartCol {
		return false
	}
	if line == loc.EndLine && col > loc.EndCol {
		return false
	}
	return true
}

// hoverContent formats the hover markdown string for an expression and its type.
func hoverContent(expr ast.Expression, typ types.Type) string {
	if ident, ok := expr.(*ast.IdentifierExpr); ok {
		return fmt.Sprintf("```lyra\n%s: %s\n```", ident.Name, typ)
	}
	return fmt.Sprintf("```lyra\n%s\n```", typ)
}

// hoverTypeReference renders the declaration a written type name refers to, for a cursor
// that no expression covers — `(p: Point)`, `-> Maybe<Point>`, a field's `n: Node`.
//
// It shows the *kind* and name rather than a rendered definition: a struct's fields are
// already on screen two lines up as often as not, and what a reader hovering a name in a
// signature wants first is "what is this, and what is it for". The doc comment carries the
// second half.
func hoverTypeReference(analysis *docAnalysis, line, col int) *lsp.Hover {
	if analysis.symTable == nil {
		return nil
	}
	ref, ok := analysis.symTable.TypeRefs.At(analysis.file, line, col)
	if !ok {
		return nil
	}
	decl, ok := analysis.symTable.LookupTypeFrom(ref.Name, ref.Loc)
	if !ok {
		return nil
	}
	content := renderHover("```lyra\n"+typeDeclSummary(decl)+"\n```", decl.Doc)
	return &lsp.Hover{Contents: lsp.MarkupContent{Kind: lsp.Markdown, Value: content}}
}

// typeDeclSummary is the one-line spelling of what a type declaration is: the keyword a
// reader would have written, then the name. `type X = …` says its target too, since an
// alias's whole content is what it aliases and hiding it would leave the hover saying
// nothing the name did not.
func typeDeclSummary(decl *ast.TypeDeclStmt) string {
	name := decl.Name
	if len(decl.GenericParams) > 0 {
		params := make([]string, 0, len(decl.GenericParams))
		for _, p := range decl.GenericParams {
			params = append(params, p.Name)
		}
		name += "<" + strings.Join(params, ", ") + ">"
	}
	if decl.IsAlias {
		return "type " + name + " = " + decl.Type.GetName()
	}
	switch decl.Type.(type) {
	case types.NamedStructType:
		return "struct " + name
	case types.DataType:
		return "data " + name
	case types.TupleType:
		return "tuple " + name
	case *types.ConstrainedType:
		// The **base**, not `decl.Type.GetName()`: a ConstrainedType's name is the
		// newtype's own, so asking it produced `newtype Cents = Cents`.
		ct := decl.Type.(*types.ConstrainedType)
		return "newtype " + name + " = " + ct.Type.GetName()
	}
	return "type " + name
}
