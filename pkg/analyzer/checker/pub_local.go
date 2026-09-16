package checker

import (
	"fmt"

	"github.com/Lyra-Language/lyra/pkg/ast"
	diag "github.com/Lyra-Language/lyra/pkg/diagnostic"
)

// CheckPubOnLocalBindings reports `pub` on a binding that is not at a module's top level
// (`lyra-E081`).
//
// **`pub` exports a name from its module, and a local binding has no name outside its
// block**, so the modifier asks for something that cannot happen. It parsed, collected and
// was then ignored — the author said "export this", the compiler did nothing, and no
// diagnostic connected the two. That is the case for an error rather than a warning: there
// is no program for which the modifier is right and none whose meaning changes by deleting
// it, so nothing is being taken away.
//
// **Top level is decided by identity, not by depth.** The program's own statements are the
// top level of their module, so they are collected into a set and everything the walk finds
// outside it is local. A depth counter would have to agree with every construct that holds
// statements — a loop body, a match arm, an `unsafe` block, a lambda — which is the
// per-construct bookkeeping rule 8 keeps cataloguing, for a question that has an exact
// answer.
func CheckPubOnLocalBindings(program *ast.Program) []diag.Diagnostic {
	topLevel := map[ast.Statement]bool{}
	for _, node := range program.Statements {
		if stmt, ok := node.(ast.Statement); ok {
			topLevel[stmt] = true
		}
	}

	var out []diag.Diagnostic
	for _, node := range program.Statements {
		stmt, ok := node.(ast.Statement)
		if !ok {
			continue
		}
		ast.WalkStmt(stmt, func(s ast.Statement) bool {
			decl, isVar := s.(*ast.VarDeclStmt)
			if !isVar || !decl.IsPublic || topLevel[s] {
				return true
			}
			out = append(out, diag.Diagnostic{
				Location: decl.GetLocation(),
				Severity: diag.SeverityError,
				Code:     diag.CodePubOnLocalBinding,
				Message: fmt.Sprintf(
					"`pub %s %s` is inside a function, and `pub` exports a name from its module — a local binding has none to export; drop the `pub`",
					decl.BindingKind, decl.Name),
			})
			return true
		}, nil)
	}
	return out
}
