package checker

import (
	"fmt"
	"strings"

	"github.com/Lyra-Language/lyra/pkg/ast"
	diag "github.com/Lyra-Language/lyra/pkg/diagnostic"
)

// CheckUnusedImports walks all top-level ImportStmt nodes and warns for any
// imported name (member alias/name, or module alias) that never appears as an
// identifier reference anywhere in the program.
func CheckUnusedImports(program *ast.Program) []diag.Diagnostic {
	// Collect all identifier references used anywhere in the program.
	refs := collectAllRefs(program)

	var warnings []diag.Diagnostic
	for _, node := range program.Statements {
		stmt, ok := node.(*ast.ImportStmt)
		if !ok {
			continue
		}
		loc := stmt.GetLocation()

		switch {
		case len(stmt.Members) > 0:
			// Named member imports: `import foo.{ a, b as c }`
			for _, m := range stmt.Members {
				effective := m.Name
				if m.Alias != "" {
					effective = m.Alias
				}
				if strings.HasPrefix(effective, "_") {
					continue
				}
				if !refs[effective] {
					warnings = append(warnings, diag.Diagnostic{
						Location: m.Location,
						Severity: diag.SeverityWarning,
						Code:     diag.CodeUnusedImport,
						Message:  fmt.Sprintf("imported name %q is never used", effective),
						Tags:     []diag.Tag{diag.TagUnnecessary},
					})
				}
			}

		case stmt.Alias != "":
			// Module alias import: `import foo.bar as baz`
			if strings.HasPrefix(stmt.Alias, "_") {
				continue
			}
			if !refs[stmt.Alias] {
				warnings = append(warnings, diag.Diagnostic{
					Location: loc,
					Severity: diag.SeverityWarning,
					Code:     diag.CodeUnusedImport,
					Message:  fmt.Sprintf("imported alias %q is never used", stmt.Alias),
					Tags:     []diag.Tag{diag.TagUnnecessary},
				})
			}

		default:
			// Plain import: `import foo.bar` — bound name is last path component.
			if len(stmt.Path) == 0 {
				continue
			}
			name := stmt.Path[len(stmt.Path)-1].Name
			if strings.HasPrefix(name, "_") {
				continue
			}
			if !refs[name] {
				warnings = append(warnings, diag.Diagnostic{
					Location: loc,
					Severity: diag.SeverityWarning,
					Code:     diag.CodeUnusedImport,
					Message:  fmt.Sprintf("imported module %q is never used", name),
					Tags:     []diag.Tag{diag.TagUnnecessary},
				})
			}
		}
	}
	return warnings
}

// collectAllRefs returns the set of all identifier names referenced anywhere
// in the program (across all statements, expressions, and nested scopes).
func collectAllRefs(program *ast.Program) map[string]bool {
	refs := make(map[string]bool)
	for _, node := range program.Statements {
		stmt, ok := node.(ast.Statement)
		if !ok {
			continue
		}
		ast.WalkStmt(stmt, nil, func(e ast.Expression) bool {
			switch ex := e.(type) {
			case *ast.IdentifierExpr:
				refs[ex.Name] = true
			case *ast.SpreadExpr:
				refs[ex.Name] = true
			}
			return true
		})
	}
	return refs
}
