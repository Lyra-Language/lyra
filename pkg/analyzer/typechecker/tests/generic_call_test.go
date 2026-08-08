package typechecker_test

import (
	"strings"
	"testing"
)

// A generic function's type variables are solved from its call's argument types
// (instantiate.go). The declaration is checked once, generically; each call site is
// checked against the *substituted* signature.

func TestGeneric_SolvedFromArgument(t *testing.T) {
	assertNoErrors(t, parseCollectAndCheck(t, `
let identity = (x: t) -> t => x
let main = () -> u8 => u8(identity(7))
`, false))
}

// The solved return type flows on: the call's result is `t`'s binding, so it is
// assignable where that concrete type is expected.
func TestGeneric_ResultTypeIsSubstituted(t *testing.T) {
	assertNoErrors(t, parseCollectAndCheck(t, `
let identity = (x: t) -> t => x
let main = () -> u8 => {
  let n: i64 = identity(7)
  u8(n)
}
`, false))
}

// A variable under a composite type is solved from the argument's corresponding
// subterm.
func TestGeneric_SolvedThroughArrayType(t *testing.T) {
	assertNoErrors(t, parseCollectAndCheck(t, `
let first = (xs: [3]t) -> t => xs[0]
let main = () -> u8 => u8(first([7, 8, 9]))
`, false))
}

// Two variables are solved independently.
func TestGeneric_TwoVariables(t *testing.T) {
	assertNoErrors(t, parseCollectAndCheck(t, `
let takeFirst = (a: t, b: u) -> t => a
let main = () -> u8 => u8(takeFirst(7, true))
`, false))
}

// One variable in two positions must be bound consistently — the check the shared
// unifier already performed for trait-impl targets, now doing the same job here.
func TestGeneric_InconsistentBindingRejected(t *testing.T) {
	res := parseCollectAndCheck(t, `
let same = (a: t, b: t) -> t => a
let main = () -> u8 => u8(same(7, true))
`, false)
	assertErrorsAre(t, res, "same: cannot infer type variable t from these arguments")
}

// A variable that appears only in the return type cannot be solved from arguments,
// so it is reported at the call rather than discovered during lowering.
func TestGeneric_UnsolvableReturnOnlyVariable(t *testing.T) {
	res := parseCollectAndCheck(t, `
let make = (n: i64) -> t => n
let main = () -> u8 => u8(make(1))
`, false)
	assertErrorContainsGeneric(t, res, "cannot infer type variable t")
}

// Arity is checked before inference: a missing argument is exactly a variable with
// nothing to bind it, and "expected 2 arguments" is the actionable message.
func TestGeneric_ArityCheckedFirst(t *testing.T) {
	res := parseCollectAndCheck(t, `
let same = (a: t, b: t) -> t => a
let main = () -> u8 => u8(same(7))
`, false)
	assertErrorContainsGeneric(t, res, "expected 2 argument(s), got 1")
}

// An **unbounded** type variable supports only what every type supports — being
// passed, returned, and stored. Arithmetic on one is rejected, and correctly so:
// `t` could be `bool` or a struct. Making it work needs bounded polymorphism over
// an operator trait (`where t: Add`), which does not exist yet — so this pins the
// boundary rather than a bug.
func TestGeneric_ArithmeticOnUnboundedVariableRejected(t *testing.T) {
	res := parseCollectAndCheck(t, `
let double = (x: t) -> t => x + x
`, false)
	// The message names both readings since 08/07, when `+` became overloadable: the
	// author meant built-in arithmetic or an impl, and a type parameter is neither.
	assertErrorsAre(t, res,
		"operator +: t is a type parameter — built-in arithmetic needs a numeric type, "+
			"and an overloaded `+` needs a concrete operand type to find its impl")
}

// assertErrorContainsGeneric asserts some error mentions want, without pinning the
// full set (these cases produce a cascade once inference fails).
func assertErrorContainsGeneric(t *testing.T, res checkResult, want string) {
	t.Helper()
	for _, e := range res.errors {
		if strings.Contains(e.Message, want) {
			return
		}
	}
	t.Errorf("expected an error containing %q; got %v", want, res.errors)
}
