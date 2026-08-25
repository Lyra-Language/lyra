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
	// author meant built-in arithmetic or an impl, and a type parameter is neither. Since
	// 08/08 the second half points at the bound, which is now the way to get there.
	assertErrorsAre(t, res,
		"operator +: t is a type parameter — built-in arithmetic needs a numeric type, "+
			"and an overloaded `+` needs a `where t: Trait` bound whose trait declares `(_+_)`")
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

// **An untyped literal takes the width the call already settled**, rather than its own
// default. Until 08/22 a literal argument was promoted to `i64` *before* unification, so
// it bound the variable first and every other width was then a conflict: `pick(w, 100)`
// on a `u8` reported "cannot infer type variable t from these arguments" about a call
// that determines it perfectly well, and the workaround was to write the conversion the
// compiler could have inferred.
func TestGeneric_UntypedLiteralAdoptsASolvedWidth(t *testing.T) {
	assertNoErrors(t, parseCollectAndCheck(t, `
let pick = (a: t, b: t) -> t => a
let main = () -> void => {
  let w: u8 = 200
  println(pick(w, 100))
}
`, false))
}

// Order does not matter: the literal is deferred whichever side it is written on, so the
// concrete argument still speaks first.
func TestGeneric_UntypedLiteralAdoptsRegardlessOfPosition(t *testing.T) {
	assertNoErrors(t, parseCollectAndCheck(t, `
let pick = (a: t, b: t) -> t => a
let main = () -> void => {
  let w: u8 = 200
  println(pick(100, w))
}
`, false))
}

// Floats too, and the narrow one is the point — `f32` is exactly the case the old
// promote-first rule could not express.
func TestGeneric_UntypedFloatLiteralAdoptsANarrowWidth(t *testing.T) {
	assertNoErrors(t, parseCollectAndCheck(t, `
let pick = (a: t, b: t) -> t => a
let main = () -> void => {
  let f: f32 = 1.5
  println(pick(f, 2.5))
}
`, false))
}

// **Nothing settles it, so the default still applies.** This is the behaviour the old
// rule existed to guarantee, and it has to survive: a type variable is a real type in the
// specialized function, so leaving one untyped would push an unresolved literal type into
// codegen.
func TestGeneric_AllUntypedArgumentsStillDefault(t *testing.T) {
	res := parseCollectAndCheck(t, `
let pick = (a: t, b: t) -> t => a
let main = () -> i64 => pick(7, 8)
`, false)
	assertNoErrors(t, res)
}

// **A literal that cannot have the settled type is still an inference failure, not an
// adoption.** Without that condition `same(7, true)` typed as `bool` and produced two
// errors — an argument mismatch plus whatever the wrongly-typed result then broke —
// where the honest answer is the one error TestGeneric_InconsistentBindingRejected
// asserts. Kept as a separate test because the two failures look alike and only this one
// says which rule is doing the work.
func TestGeneric_ALiteralDoesNotAdoptATypeItCannotHave(t *testing.T) {
	res := parseCollectAndCheck(t, `
let same = (a: t, b: t) -> t => a
let main = () -> bool => same(7, true)
`, false)
	assertErrorsAre(t, res, "same: cannot infer type variable t from these arguments")
}

// Adopting a width does not exempt the literal from fitting in it: the argument check runs
// against the *instantiated* signature, so the range rule that catches `takes(300)` on a
// plain `u8` parameter catches this too.
func TestGeneric_AnAdoptedWidthStillBoundsTheLiteral(t *testing.T) {
	res := parseCollectAndCheck(t, `
let pick = (a: t, b: t) -> t => a
let main = () -> void => {
  let w: u8 = 200
  println(pick(w, 300))
}
`, false)
	assertErrorContainsGeneric(t, res, "literal value 300 overflows u8")
}
