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
	g, isVar := operandType.(types.GenericType)
	if !isVar || !tc.boundProvidesShow(g, operand) {
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
