package typechecker_test

import "testing"

// `Show` — formatting a value whose type is a type parameter (08/08).
//
// The mechanism is a **desugar**: where the operand's type is a type variable bound by a
// trait declaring `show`, `"${v}"` and `println(v)` are rewritten to `v.show()` before
// anything downstream sees them. So this is bound dispatch, and the printable-type rule is
// untouched — the rewritten operand is a string.
//
// These run without the prelude, so each declares the trait itself (`showTrait`, shared
// with where_bounds_test.go) — which is also the point: nothing keys on the spelling
// `Show`, only on a bound declaring `show`. The prelude's own trait and impls are
// exercised end to end in pkg/backend/llvm.

func TestShow_InterpolatesABoundTypeParameter(t *testing.T) {
	assertNoErrors(t, parseCollectAndCheck(t, showTrait+`
let describe<t> where t: Show = (v: t) -> string => "value ${v}"
`, false))
}

func TestShow_PrintsABoundTypeParameter(t *testing.T) {
	assertNoErrors(t, parseCollectAndCheck(t, showTrait+`
let emit<t> where t: Show = (v: t) -> void => println(v)
`, false))
}

// Without a bound the message names the thing the author can actually do. It used to say
// "expected a string, an integer, a float, bool, or rune", which is true of a `t` and no
// help at all — a type parameter cannot be made into one of those.
func TestShow_UnboundTypeParameterNamesTheBound(t *testing.T) {
	res := parseCollectAndCheck(t, `
let describe<t> = (v: t) -> string => "value ${v}"
`, false)
	assertHasErrorContaining(t, res,
		"a type parameter has no representation to format — add `where t: Show` so the value can be rendered")
}

func TestShow_UnboundTypeParameterInPrint(t *testing.T) {
	res := parseCollectAndCheck(t, `
let emit<t> = (v: t) -> void => println(v)
`, false)
	assertHasErrorContaining(t, res, "cannot print a value of type t")
}

// A concrete type that is not printable still gets the printable-type message, which is
// the right one for it: the fix is a conversion or an impl, not a bound.
func TestShow_ConcreteUnprintableTypeKeepsItsOwnMessage(t *testing.T) {
	res := parseCollectAndCheck(t, `
struct Pt { x: i64 }
let show_it = (p: Pt) -> string => "${p}"
`, false)
	assertHasErrorContaining(t, res, "expected a string, an integer, a float, bool, or rune")
}

// The bound is enforced at the instantiation, which is the only point where the question
// has an answer.
func TestShow_InstantiationWithoutAnImplIsRejected(t *testing.T) {
	res := parseCollectAndCheck(t, showTrait+`
struct NoShow { x: i64 }
let describe<t> where t: Show = (v: t) -> string => "${v}"
let bad = describe(NoShow { x: 1 })
`, false)
	assertHasErrorContaining(t, res, "does not implement Show (required by `where t: Show`)")
}

// **The trait is recognized by its method, not by its name.** A program may declare its
// own; nothing keys on the spelling `Show`, which is only what the diagnostic suggests.
func TestShow_AUserTraitDeclaringShowAlsoWorks(t *testing.T) {
	assertNoErrors(t, parseCollectAndCheck(t, `
trait Render { show: (Self) -> string }
impl Render for i64 { show = (self) => "n" }
let describe<t> where t: Render = (v: t) -> string => "value ${v}"
let ok = describe(1)
`, false))
}

// A bound that does not declare `show` is not enough, and the message says what to add.
func TestShow_ABoundWithoutShowIsNotEnough(t *testing.T) {
	res := parseCollectAndCheck(t, `
trait Sized { size: (Self) -> i64 }
let describe<t> where t: Sized = (v: t) -> string => "value ${v}"
`, false)
	assertHasErrorContaining(t, res, "add `where t: Show`")
}
