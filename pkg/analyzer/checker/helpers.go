package checker

import "github.com/Lyra-Language/lyra/pkg/ast"

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

// patternBoundNames collects all variable names introduced by a pattern.
func patternBoundNames(pat ast.Pattern) []string {
	if pat == nil {
		return nil
	}
	switch p := pat.(type) {
	case *ast.IdentifierPattern:
		if p.Name == "_" {
			return nil
		}
		return []string{p.Name}

	case *ast.TuplePattern:
		var names []string
		for _, el := range p.Elements {
			names = append(names, patternBoundNames(el)...)
		}
		return names

	case *ast.ArrayPattern:
		var names []string
		for _, el := range p.Elements {
			names = append(names, patternBoundNames(el)...)
		}
		return names

	case *ast.StructPattern:
		var names []string
		for _, f := range p.Fields {
			names = append(names, patternBoundNames(f.Pattern)...)
		}
		return names

	case *ast.DataPattern:
		if p.Pattern != nil {
			return patternBoundNames(p.Pattern)
		}

	case *ast.RestPattern:
		if p.Identifier != "" {
			return []string{p.Identifier}
		}

		// WildcardPattern, LiteralPattern, RangePattern bind no names.
	}
	return nil
}
