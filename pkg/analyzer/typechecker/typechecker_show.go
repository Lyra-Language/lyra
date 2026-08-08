package typechecker

import (
	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/types"
)

// Formatting a value whose type is a **type parameter**.
//
// `print` and `"${…}"` pick a formatter per *concrete* type, so a `t` could not be
// rendered at all — there is nothing to pick. The prelude's own `expect` hit this the day
// it was written: the natural draft reports what it got, `panic("expected ${value}")`, and
// none of it was expressible.
//
// The fix is a **desugar**, not a second formatting path: where the operand's type is a
// type variable bound by a trait that declares `show`, the operand is rewritten to
// `operand.show()` before anything downstream sees it. That call is ordinary bound
// dispatch, which has resolved and lowered since 08/07, so the backend learns nothing new
// and `print`'s printable-type rule stays exactly as strict as it was — the rewritten
// operand is a `string`.
//
// It is the shape UFCS uses for a receiver and rule 10 recommends generally: one rewrite
// beats teaching every later pass what a `t` in print position means.
//
// **The trait is recognized by its method, not by its name.** Any in-scope bound whose
// trait declares `show` will do, so a program may define its own — the same rule
// arithmetic operator overloading follows, and the reason the compiler needs no
// `@builtin(Show)` marker for this. `Show` is only what the *diagnostic* suggests, because
// that is the one the prelude ships.

// showMethodName is the method a bound must declare for a type parameter to be formattable.
const showMethodName = "show"

// showTraitSuggestion is the trait named in the diagnostic when no bound provides it. The
// prelude's, and only a suggestion — nothing keys on this name.
const showTraitSuggestion = "Show"

// desugarShowOperand rewrites `v` to `v.show()` when v's type is a type parameter bound by
// a trait declaring `show`, returning the new expression and its type.
//
// ok=false means the operand is not a bound type parameter and the caller should apply its
// ordinary rule — which for a *concrete* type is the printable-type check, unchanged.
func (tc *TypeChecker) desugarShowOperand(operand ast.Expression, operandType types.Type) (ast.Expression, types.Type, bool) {
	if !tc.showApplies(operand, operandType) {
		return nil, nil, false
	}
	call := &ast.FunctionCallExpr{
		ExprBase: ast.ExprBase{AstBase: ast.AstBase{Location: operand.GetLocation()}},
		Function: &ast.MemberExpr{
			ExprBase: ast.ExprBase{AstBase: ast.AstBase{Location: operand.GetLocation()}},
			Object:   operand,
			Property: ast.IdentifierExpr{
				ExprBase: ast.ExprBase{AstBase: ast.AstBase{Location: operand.GetLocation()}},
				Name:     showMethodName,
			},
		},
	}
	// Checked as the call it now is, which is what records the bound resolution and the
	// per-implementing-type candidates the backend picks from.
	return call, tc.inferExprType(call), true
}

// showApplies reports whether the operand should be rendered through `show` rather than
// by a built-in formatter. Two cases reach it, and the order of the tests is the design:
//
//  1. **A printable type never does.** `print` picks a formatter per concrete type for
//     string, the integers, the floats, `bool` and `rune`, and that stays true whatever
//     impls a program declares — the same rule arithmetic and the comparisons follow, and
//     for the same reason: a language where a library can change how an `i64` prints is
//     one whose output no reader can predict.
//  2. **A type parameter** needs a bound declaring `show`; a **concrete** type needs an
//     impl providing it.
//
// The concrete case was left out when `Show` landed (08/08) and added the same day, on
// the strength of the inconsistency rather than a new argument: the *same* impl already
// rendered a `Pt` through `describe(pt)` under a `where t: Show` bound, and `println(pt)`
// was refused. One value, one impl, two answers — and the one that worked was the
// indirect one.
//
// It does mean `print` can call user code, which is the coherence question the operators
// answered in opposite directions. It is answered "yes" here because the alternative is
// not "print calls no user code" — the bounded-generic path already did — but "print
// calls user code only when laundered through a generic", which is a rule with nothing
// to recommend it. The call the rewrite produces is an ordinary one, so the purity
// ladders charge it and a `pure` function printing through an impure `show` is refused.
func (tc *TypeChecker) showApplies(operand ast.Expression, operandType types.Type) bool {
	if isPrintableType(operandType) {
		return false
	}
	if g, isVar := operandType.(types.GenericType); isVar {
		return tc.boundProvidesShow(g, operand)
	}
	// **Never rewrite into the method being defined.** `impl Show for Pt { show = (self)
	// => "${self}" }` is the first thing an author writes, because it is exactly what the
	// prelude's scalar impls say — and with the concrete case dispatching, that
	// interpolation would call `show` again. It compiled and stack-overflowed (SIGSEGV),
	// which is the "looks like it works" failure this project refuses; it is caught here
	// instead, with a message that names the fix.
	//
	// Direct self-recursion only. A `show` that renders a *different* type whose own
	// `show` comes back is ordinary mutual recursion, and no more the compiler's business
	// than any other cycle; the direct case is the one that is *implicit* — the author
	// wrote `${self}`, not `self.show()`.
	if tc.inShowImplFor(operandType) {
		return false
	}
	// Any trait providing `show` for this type. Ambiguity between two of them is not
	// reported here: the rewrite produces an ordinary `.show()` call, and the identifier
	// dispatch path reports it there — one message rather than two for one mistake.
	return len(tc.resolveTraitMethod(operandType, showMethodName, "")) > 0
}

// inShowImplFor reports whether the body being checked is the `show` of an impl for
// operandType — i.e. whether rewriting this operand would call the method it is inside.
func (tc *TypeChecker) inShowImplFor(operandType types.Type) bool {
	if tc.currentImplType == nil || tc.currentImplMethod.Kind != ast.MethodNameKindIdentifier {
		return false
	}
	if tc.currentImplMethod.Value != showMethodName {
		return false
	}
	return types.TypesEqual(tc.currentImplType, operandType)
}

// boundProvidesShow reports whether any trait bounding g in scope declares `show`.
func (tc *TypeChecker) boundProvidesShow(g types.GenericType, at ast.Expression) bool {
	for _, traitName := range tc.genericBounds[g.Name] {
		trait, ok := tc.symTable.LookupTraitFrom(traitName, at.GetLocation())
		if !ok {
			continue
		}
		if m := findTraitMethod(trait, showMethodName); m != nil && m.Signature != nil {
			return true
		}
	}
	return false
}

// reportUnshowableTypeParameter is the diagnostic for a type parameter in print position
// with no `show` bound.
//
// It replaces "expected a string, an integer, a float, bool, or rune", which is a true
// statement about a `t` and no help at all: the author cannot make a type parameter be one
// of those, and the thing they can do — add a bound — went unmentioned. Only reached for a
// type *variable*; a concrete type still gets the printable-type message, which is the
// right one for it.
func (tc *TypeChecker) reportUnshowableTypeParameter(operand ast.Expression, g types.GenericType, verb string) {
	tc.addError(operand.GetLocation(), SeverityError,
		"cannot %s a value of type %s: a type parameter has no representation to format — "+
			"add `where %s: %s` so the value can be rendered",
		verb, g.Name, g.Name, showTraitSuggestion)
}

// reportShowSelfRecursion is the diagnostic for `${self}` (or `println(self)`) inside the
// `show` that would render it.
//
// It exists because the generic printable-type message is actively misleading here: the
// author is looking at a type that *is* showable, being told it is not. What they need to
// know is that this particular occurrence would call the method they are writing.
func (tc *TypeChecker) reportShowSelfRecursion(operand ast.Expression, t types.Type, verb string) {
	tc.addError(operand.GetLocation(), SeverityError,
		"cannot %s a value of type %s here: this is %s's own `%s`, so the value would be "+
			"rendered by calling it again — render the fields instead",
		verb, t, t, showMethodName)
}
