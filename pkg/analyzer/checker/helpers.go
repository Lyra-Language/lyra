package checker

import "github.com/Lyra-Language/lyra/pkg/ast"

// walkLambdaBodies walks a lambda's body and every clause body with the given
// statement/expression visitors — the descent every context-tracking pass
// performs over a lambda's executable interior (the main body and any
// pattern-clause bodies share one context). Parameter default values are NOT
// walked here: they run in the enclosing context, so a caller that cares about
// them walks them separately before calling this.
func walkLambdaBodies(lam *ast.LambdaExpr, sv func(ast.Statement) bool, ev func(ast.Expression) bool) {
	ast.WalkExpr(lam.Body, sv, ev)
	for _, clause := range lam.LambdaClauses {
		ast.WalkExpr(clause.Body, sv, ev)
	}
}

// directDeclaredNames returns the variable name(s) declared directly by stmt,
// without recursing into nested blocks.
func directDeclaredNames(stmt ast.Statement) []string {
	if stmt == nil {
		return nil
	}
	switch s := stmt.(type) {
	case *ast.VarDeclStmt:
		return []string{s.Name}
	case *ast.DestructuringDeclStmt:
		return patternBoundNames(s.Pattern)
	}
	return nil
}

type nameLocation struct {
	Name     string
	Location ast.Location
}

// directDeclaredNamesWithLocations is like directDeclaredNames but also
// returns the source location of each declared name.
func directDeclaredNamesWithLocations(stmt ast.Statement) []nameLocation {
	if stmt == nil {
		return nil
	}
	switch s := stmt.(type) {
	case *ast.VarDeclStmt:
		return []nameLocation{{Name: s.Name, Location: s.GetLocation()}}
	case *ast.DestructuringDeclStmt:
		names := patternBoundNames(s.Pattern)
		result := make([]nameLocation, len(names))
		for i, name := range names {
			result[i] = nameLocation{Name: name, Location: s.GetLocation()}
		}
		return result
	}
	return nil
}

// patternBoundNames collects all variable names introduced by a pattern.
func patternBoundNames(pat ast.Pattern) []string {
	// **This one had drifted**, which is why the three are now one: it handled neither
	// `name @ pattern` nor a struct pattern's shorthand `{ x }`, so a name bound either way
	// was invisible to whatever this feeds. The `_` filter stays here rather than moving
	// into the shared walk — a wildcard binds a name the language makes unforgeable, and
	// the passes that register bindings *want* it.
	var out []string
	ast.EachPatternBinding(pat, func(b ast.PatternBinding) {
		if b.Name != "_" {
			out = append(out, b.Name)
		}
	})
	return out
}
