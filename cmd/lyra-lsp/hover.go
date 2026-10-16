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
	walkProgramExprs(program, onExpr)
	return best
}

// findPatternAtPos is findExprAtPos for the pattern half of the AST, and exists for the
// same reason: `Keyboard(Up) => …` is a *pattern*, and every position feature walked only
// expressions, so go-to-definition on a constructor in a `match` arm did nothing.
//
// Worse than nothing, in fact, which is why this is a lookup of its own rather than a
// fallback on the expression one: patterns hold no expressions (bar a range's `const`
// bounds, which walkProgramExprs supplies) and expressions no patterns, but their *spans overlap*, so asking `findExprAtPos` about a position inside a pattern
// returns whatever expression encloses the arm — a nearby tuple literal, in the reported
// case. Answering from the wrong node is how a feature comes to be confidently wrong rather
// than silent.
//
// Patterns hang off ordinary nodes, so this walks statements and expressions as usual
// (`ast.WalkStmt`) and descends into whatever each holds (`ast.PatternsOf`,
// `ast.WalkPattern`). The narrowest span containing the position wins, exactly as above.
func findPatternAtPos(program *ast.Program, line, col int) ast.Pattern {
	if program == nil {
		return nil
	}
	var best ast.Pattern
	onPattern := func(p ast.Pattern) bool {
		loc := p.GetLocation()
		if containsPos(loc, line, col) && (best == nil || spanWithin(loc, best.GetLocation())) {
			best = p
		}
		return true
	}
	consider := func(node ast.AstNode) {
		for _, p := range ast.PatternsOf(node) {
			ast.WalkPattern(p, onPattern)
		}
	}
	for _, node := range program.Statements {
		stmt, ok := node.(ast.Statement)
		if !ok {
			continue
		}
		ast.WalkStmt(stmt, func(s ast.Statement) bool {
			consider(s)
			return true
		}, func(e ast.Expression) bool {
			consider(e)
			return true
		})
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
func hoverContent(expr ast.Expression, typ types.Type, analysis *docAnalysis, line, col int) string {
	shown := typeWithParamNames(typ, paramNamesAt(expr, line, col, analysis))
	if ident, ok := expr.(*ast.IdentifierExpr); ok {
		return fmt.Sprintf("```lyra\n%s: %s\n```", ident.Name, shown)
	}
	return fmt.Sprintf("```lyra\n%s\n```", shown)
}

// typeWithParamNames renders a function type with each parameter's name before its type —
// `(width: i64, height: i64) -> i64` rather than `(i64, i64) -> i64`, which says what a
// function takes but not which argument is which.
//
// The **types are the recorded ones**, not the declaration's: at a use of a generic
// function the recorded type is the instantiation, and that is what the hover showed
// before names were added. Only the names come from the declaration, and a name list
// whose length disagrees with the type is dropped whole rather than lined up by guesswork.
// An `extern`'s names need no declaration — they are part of its type.
func typeWithParamNames(typ types.Type, names []string) string {
	fn, ok := typ.(*types.LambdaType)
	if !ok {
		return typ.String()
	}
	if len(names) != len(fn.Parameters) {
		names = nil
	}
	params := make([]string, 0, len(fn.Parameters)+1)
	for i, p := range fn.Parameters {
		name := p.Name
		if names != nil {
			name = names[i]
		}
		if name == "" {
			params = append(params, p.String())
		} else {
			params = append(params, name+": "+p.String())
		}
	}
	if fn.IsVariadic {
		params = append(params, "...")
	}
	return fmt.Sprintf("%s(%s) -> %s", fn.EffectPrefix(), strings.Join(params, ", "), fn.ReturnType)
}

// paramNamesAt returns the parameter names of the function the hovered expression is or
// names, or nil when it is neither.
//
// For a name, the declaration it resolves to — except at a **receiver-overloaded call**,
// where the name alone does not pick a declaration (rule 9) and the one the typechecker
// chose is read from `TypeTable.Callee` first. A function value with no declaration of
// its own (a parameter `f: (i64) -> i64`, `let g = f`) has no names to give.
func paramNamesAt(expr ast.Expression, line, col int, analysis *docAnalysis) []string {
	switch e := expr.(type) {
	case *ast.LambdaExpr:
		return lambdaParamNames(e)
	case *ast.IdentifierExpr:
		if call := callOf(analysis.program, e); call != nil {
			if fn, ok := analysis.typeTable.Callee(call); ok {
				return lambdaParamNames(fn)
			}
		}
		named, ok := resolveNameAtPos(e, line, col, analysis)
		if !ok {
			return nil
		}
		return declParamNames(named)
	}
	return nil
}

// declParamNames is paramNamesAt for a declaration in hand: a `let` (or `var`, `const`)
// bound to a function literal. An overload set is not one function and answers nil.
func declParamNames(named ast.Named) []string {
	if v, ok := named.(*ast.VarDeclStmt); ok {
		if fn, ok := v.Value.(*ast.LambdaExpr); ok {
			return lambdaParamNames(fn)
		}
	}
	return nil
}

// lambdaParamNames lists the name each parameter binds, or "" for one that binds a
// pattern — a destructured tuple, or a multi-clause function's desugared parameters —
// which is shown by its type alone.
func lambdaParamNames(fn *ast.LambdaExpr) []string {
	names := make([]string, len(fn.Parameters))
	for i := range fn.Parameters {
		if id, ok := fn.Parameters[i].Pattern.(*ast.IdentifierPattern); ok {
			names[i] = id.Name
		}
	}
	return names
}

// callOf returns the call whose callee is exactly ident, or nil.
func callOf(program *ast.Program, ident *ast.IdentifierExpr) *ast.FunctionCallExpr {
	var found *ast.FunctionCallExpr
	walkProgramExprs(program, func(e ast.Expression) bool {
		if call, ok := e.(*ast.FunctionCallExpr); ok && call.Function == ast.Expression(ident) {
			found = call
		}
		return found == nil
	})
	return found
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
	named, ok := lookupTypeOrTrait(analysis, ref)
	if !ok {
		return nil
	}
	summary, doc := "", (*ast.Doc)(nil)
	switch decl := named.(type) {
	case *ast.TypeDeclStmt:
		summary, doc = typeDeclSummary(decl), decl.Doc
	case *ast.TraitDeclStmt:
		summary, doc = traitDeclSummary(decl), decl.Doc
	default:
		return nil
	}
	content := renderHover("```lyra\n"+summary+"\n```", doc)
	return &lsp.Hover{Contents: lsp.MarkupContent{Kind: lsp.Markdown, Value: content}}
}

// traitDeclSummary is a trait's one-line spelling, supertraits included: `trait B: A` is
// two claims at once (every implementer of B implements A, and a `where t: B` bound
// reaches A's methods), so a hover that hid the `: A` would hide half of what the name
// means.
func traitDeclSummary(decl *ast.TraitDeclStmt) string {
	out := "trait " + decl.Name
	if len(decl.GenericParams) > 0 {
		params := make([]string, 0, len(decl.GenericParams))
		for _, p := range decl.GenericParams {
			params = append(params, p.Name)
		}
		out += "<" + strings.Join(params, ", ") + ">"
	}
	if len(decl.Bounds) > 0 {
		out += ": " + strings.Join(decl.Bounds, " + ")
	}
	return out
}

// typeDeclSummary is the one-line spelling of what a type declaration is: the keyword a
// reader would have written, then the name. `alias X = …` says its target too, since an
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
		return "alias " + name + " = " + decl.Type.GetName()
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
		// newtype's own, so asking it produced `type Cents = Cents`.
		ct := decl.Type.(*types.ConstrainedType)
		return "type " + name + " = " + ct.Type.GetName()
	}
	return "type " + name
}
