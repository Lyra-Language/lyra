package checker

import (
	"fmt"

	"github.com/Lyra-Language/lyra/pkg/ast"
	diag "github.com/Lyra-Language/lyra/pkg/diagnostic"
	"github.com/Lyra-Language/lyra/pkg/types"
)

// CheckTypeNames warns about a **struct** declared with an all-uppercase
// (SCREAMING_CASE) name. Such a name matches the `const_identifier` lexer rule
// (`[A-Z][A-Z0-9_]*`) rather than `user_defined_type_name` (which needs a
// lowercase letter to win the lexer's longest-match tie), so a struct literal
// `NAME { … }` won't parse — the name reads as a constant. The type still
// registers and works in reference positions (an annotation, a `data` payload),
// which is why this is a warning, not an error; but a *value* of it can never be
// constructed, and the failure otherwise surfaces far away as a confusing
// "undefined symbol" at the use site. Flagging it at the declaration points at the
// fix: give the struct a PascalCase name.
//
// Only structs are affected: a named tuple (`Name(…)`) and a `data` type
// (constructed via its constructors) don't use `Name { … }` construction, so a
// SCREAMING_CASE name constructs fine there.
func CheckTypeNames(program *ast.Program) []diag.Diagnostic {
	var warnings []diag.Diagnostic
	for _, node := range program.Statements {
		decl, ok := node.(*ast.TypeDeclStmt)
		if !ok || !isScreamingCaseName(decl.Name) {
			continue
		}
		if _, isStruct := decl.Type.(types.NamedStructType); !isStruct {
			continue
		}
		loc := decl.NameLocation
		if loc.StartLine == 0 {
			loc = decl.GetLocation()
		}
		warnings = append(warnings, diag.Diagnostic{
			Location: loc,
			Severity: diag.SeverityWarning,
			Code:     diag.CodeScreamingCaseTypeName,
			Message: fmt.Sprintf(
				"type name %q is all-uppercase, so the lexer reads it as a constant identifier (SCREAMING_CASE) — a struct literal `%s { … }` won't parse; give it a PascalCase name with a lowercase letter (e.g. `Node`, `Pair`) to construct values of it",
				decl.Name, decl.Name),
		})
	}
	return warnings
}

// isScreamingCaseName reports whether name matches the `const_identifier` lexer
// rule (`[A-Z][A-Z0-9_]*`) — no lowercase letter — so as a type name it collides
// with the constant-identifier token.
func isScreamingCaseName(name string) bool {
	if name == "" {
		return false
	}
	for i, r := range name {
		switch {
		case i == 0 && (r < 'A' || r > 'Z'):
			return false
		case i > 0 && !((r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_'):
			return false
		}
	}
	return true
}
