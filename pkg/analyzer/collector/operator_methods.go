package collector

import (
	"github.com/Lyra-Language/lyra/pkg/ast"
	diag "github.com/Lyra-Language/lyra/pkg/diagnostic"
)

// Operator-named trait methods — `(_==_)`, `(_+_)`, `(-_)`, `(_++)`.
//
// The grammar reserves twenty binary spellings plus the prefix and suffix forms, and
// they fall into three groups. This file reports the two that do not work; the third
// needs nothing said about it.
//
//   - **Dispatched** (08/07): the ten arithmetic and bitwise binary operators
//     `+ - * / % << >> & | ~`, and the prefix `-` and `~`. `a + b` on a user type
//     resolves to a `(_+_)` method exactly as `a.show()` resolves to `show` — see
//     typechecker_operator_overload.go. Nothing is reported for these.
//   - **Owned by the compiler** — the seven comparisons. Refused (lyra-E039), naming
//     the trait that owns each: `==`/`!=` → `Eq`, `<`/`<=`/`>`/`>=`/`<=>` → `Ord`.
//     Declaring them one at a time reintroduces exactly the `<`-disagrees-with-`<=>`
//     failure `Ord`'s single `compare` exists to prevent, which is the C++/Java shape.
//   - **Inert** — everything left. Warned about (lyra-W015) with the reason, because
//     each is inert for a *different* reason and a reader deserves to know which.
//
// The reasons the inert group is inert:
//
//   - `&&` and `||` **cannot** be overloaded. A function call evaluates its arguments,
//     and not evaluating the right operand is the entire content of these two
//     operators; an impl would silently take that away at every use. (C++ allows it
//     and the advice everywhere is never to do it.)
//   - `!` is boolean negation, and the language has no notion of user truthiness. A
//     `!` returning anything other than a bool is a puzzle, not a feature.
//   - `**` is a *spelling with no operator*: the grammar reserves the method name and
//     the language has no exponent operator, so nothing could ever call it. Its mirror
//     image is `%%`, an operator with no spelling — the two gaps are the grammar's
//     and are recorded in todo.md.
//   - `_++` and `_--` are suffix forms of operators the language does not have either;
//     `++` is *string concatenation* in Lyra, a binary operator, and it is not in the
//     reserved list at all.
var comparisonOperatorMethods = map[string]bool{
	"==": true, "!=": true, "<": true, "<=": true, ">": true, ">=": true, "<=>": true,
}

// dispatchedBinaryOperatorMethods is the set the typechecker routes an operator to.
// It is deliberately a *copy* of the typechecker's own table rather than an import:
// the collector runs before type checking and must not depend on it, and the two are
// pinned together by a test that fails if they drift
// (TestOperatorMethods_DispatchedSetMatchesTypechecker).
var dispatchedBinaryOperatorMethods = map[string]bool{
	"+": true, "-": true, "*": true, "/": true, "%": true,
	"<<": true, ">>": true, "&": true, "|": true, "~": true,
}

// dispatchedPrefixOperatorMethods is the prefix half: `-` and `~`. `!` is absent for
// the reason given above.
var dispatchedPrefixOperatorMethods = map[string]bool{"-": true, "~": true}

// inertOperatorReason gives the reason an operator-named method is not dispatched to,
// or "" when it is. One sentence per operator, because "nothing calls it" was the old
// message and it left the author with no idea whether to wait or to rewrite.
func inertOperatorReason(name ast.MethodName) string {
	switch name.Kind {
	case ast.MethodNameKindBinary:
		if dispatchedBinaryOperatorMethods[name.Value] {
			return ""
		}
		switch name.Value {
		case "&&", "||":
			return "a call evaluates its arguments, so an impl could not short-circuit — " +
				"which is the whole of what `" + name.Value + "` does"
		case "**":
			return "the language has no `**` operator, so nothing could call it"
		}
	case ast.MethodNameKindPrefix:
		if dispatchedPrefixOperatorMethods[name.Value] {
			return ""
		}
		if name.Value == "!" {
			return "`!` is boolean negation and the language has no user-defined truthiness"
		}
	case ast.MethodNameKindSuffix:
		return "the language has no suffix `" + name.Value + "` operator, so nothing could call it"
	}
	return "nothing dispatches to it"
}

// checkOperatorMethodNames reports every operator-named method in a trait declaration
// or an impl block that is refused or inert.
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
		trait, method := c.canonicalTraitName("Ord"), "compare"
		if name.Value == "==" || name.Value == "!=" {
			trait, method = c.canonicalTraitName("Eq"), "eq"
		}
		c.addDeriveDiagnostic(loc, diag.SeverityError, diag.CodeComparisonOperatorMethod,
			"%s: `(_%s_)` cannot be a method name — the compiler owns %s; implement `%s` with its `%s` method instead",
			where, name.Value, name.Value, trait, method)
		return
	}
	reason := inertOperatorReason(name)
	if reason == "" {
		return
	}
	c.addDeriveDiagnostic(loc, diag.SeverityWarning, diag.CodeInertOperatorMethod,
		"%s: the operator method %q is not dispatched to — %s",
		where, name.Value, reason)
}
