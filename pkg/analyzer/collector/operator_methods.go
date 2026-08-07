package collector

import (
	"github.com/Lyra-Language/lyra/pkg/ast"
	diag "github.com/Lyra-Language/lyra/pkg/diagnostic"
)

// Operator-named trait methods — `(_==_)`, `(_+_)`, `(-_)`, `(_++)`.
//
// The grammar reserves twenty binary spellings plus prefix and suffix forms, and
// **nothing dispatches to any of them**: every consumer (resolveTraitMethod,
// findTraitMethod, the purity pass) filters on `MethodNameKindIdentifier` and skips the
// rest, so the declaration parses, the impl collects, and the operator keeps its
// built-in meaning. Verified 08/07 — a `(_==_)` impl on a struct is simply never
// called.
//
// That is the fourth collected-and-unread surface found in two days, after `wallClock`,
// the `where` bounds and `@derive`, and it is reported for the same reason: syntax that
// looks like a feature and does nothing costs more than syntax that does not exist.
//
// The split between the two diagnostics is the design decision (todo.md):
//
//   - The **comparison** operators are now owned by the compiler. `==`/`!=` are
//     structural and overridden by the prelude's `Eq`; `<`/`<=`/`>`/`>=`/`<=>` all
//     derive from `Ord::compare`. A second mechanism would be a coherence question
//     with no answer, and declaring them one by one reintroduces exactly the
//     disagreement between `<` and `<=>` that `Ord`'s single method prevents. Refused.
//   - Everything else — arithmetic, bitwise, the prefix and suffix forms — has no
//     canonical trait and no other design on the table, so the syntax is kept and
//     warned about rather than removed. `(_-_)` in particular is load-bearing for a
//     hazard already recorded: `Empty - 1` parses as `Empty(-1)`, which only bites a
//     `data` type that overloads `-`.
var comparisonOperatorMethods = map[string]bool{
	"==": true, "!=": true, "<": true, "<=": true, ">": true, ">=": true, "<=>": true,
}

// checkOperatorMethodNames reports every operator-named method in a trait declaration
// or an impl block.
//
// Both are walked, not just the declaration: an impl may name a method the trait never
// declared (checkTraitImpl warns about that separately), so an operator impl can exist
// with no operator declaration to report it.
func (c *Collector) checkOperatorMethodNames() {
	for _, stmt := range c.ast.Statements {
		switch decl := stmt.(type) {
		case *ast.TraitDeclStmt:
			for i := range decl.Methods {
				c.reportOperatorMethod(decl.Methods[i].Name, decl.NameLocation, "trait "+decl.Name)
			}
		case *ast.TraitImplStmt:
			for i := range decl.Methods {
				c.reportOperatorMethod(decl.Methods[i].Name, decl.GetLocation(),
					"impl "+decl.TraitName)
			}
		}
	}
}

func (c *Collector) reportOperatorMethod(name ast.MethodName, loc ast.Location, where string) {
	if name.Kind == ast.MethodNameKindIdentifier {
		return
	}
	if name.Kind == ast.MethodNameKindBinary && comparisonOperatorMethods[name.Value] {
		trait, method := "Ord", "compare"
		if name.Value == "==" || name.Value == "!=" {
			trait, method = "Eq", "eq"
		}
		c.addDeriveDiagnostic(loc, diag.SeverityError, diag.CodeComparisonOperatorMethod,
			"%s: `(_%s_)` cannot be a method name — the compiler owns %s; implement `%s` with its `%s` method instead",
			where, name.Value, name.Value, trait, method)
		return
	}
	c.addDeriveDiagnostic(loc, diag.SeverityWarning, diag.CodeInertOperatorMethod,
		"%s: the operator method %q is not dispatched to — nothing calls it, and `%s` keeps its built-in meaning",
		where, name.Value, name.Value)
}
