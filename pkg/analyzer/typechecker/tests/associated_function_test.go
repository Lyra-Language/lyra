package typechecker_test

import "testing"

// What `Type.name` refuses, and where each refusal points. lyra-E035 keeps its three cases (a
// type, a trait, a name that is neither) for a type name that names no function.
func TestAssociatedFunction_Refusals(t *testing.T) {
	const decls = `struct Pt { x: i64 }
impl Pt {
  let origin = pure () -> Pt => Pt { x: 0 }
  let shifted = pure (self, by: i64) -> Pt => Pt { x: self.x + by }
  let generic_one<u> = pure (v: u) -> u => v
}
let free_one = pure () -> i64 => 1
trait Shape { area: (Self) -> i64 }
`
	cases := []struct{ name, src, want string }{
		{"no such function", `let a = Pt.nope()`, `Pt has no associated function "nope"; declare one with no ` + "`self`"},
		{"a method named through the type", `let a = Pt.shifted(Pt.origin(), 1)`, "shifted takes `self`, so call it on a value"},
		{"a free function named through the type", `let a = Pt.free_one()`, "a free function has that name"},
		{"a builtin's", `let a = string.nope()`, `string has no associated function "nope"`},
		{"an all-caps type's", "data IO = Fd(i64)\nlet a = IO.nope()", `IO has no associated function "nope"`},
		{"the bare name", `let a = origin()`, `undefined function "origin"`},
		{"too many arguments", `let a = Pt.origin(1)`, "Pt.origin"},
		{"a generic function as a value", `let a = Pt.generic_one`, "is generic, so it has no one signature"},
		{"a type as a value", `let a = Pt`, "Pt is a type, not a value"},
		{"a field of a type", `let a = Pt.x`, "Pt is a type, not a value"},
		{"an undefined name", `let a = Nope.make()`, `undefined constructor or type "Nope"`},
		{"a trait", `let a = Shape.area(1)`, "Shape is a trait, not a value"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assertHasErrorContaining(t, parseCollectAndCheck(t, decls+c.src, false), c.want)
		})
	}
}

// A generic target's trait function takes its arguments from the context, which has to be that
// type: under another type's annotation the call would otherwise pick that type's impl.
func TestAssociatedFunction_AGenericTargetsTraitFunctionNeedsItsTypeAsContext(t *testing.T) {
	const decls = `trait Zero { zero: () -> Self }
struct Wrap<t> { items: []t }
impl Zero for Wrap<t> { zero = () => Wrap { items: [] } }
impl Zero for i64 { zero = () => 0 }
`
	assertNoErrors(t, parseCollectAndCheck(t, decls+`let a: Wrap<i64> = Wrap.zero()`, false))
	for _, src := range []string{`let a = Wrap.zero()`, `let a: i64 = Wrap.zero()`} {
		assertHasErrorContaining(t, parseCollectAndCheck(t, decls+src, false), "Wrap is generic, so Wrap.zero takes its arguments")
	}
}

// One type declared twice under two names has no one function to call.
func TestAssociatedFunction_TwoDeclarationsOfOneTypeAreAmbiguous(t *testing.T) {
	res := parseCollectAndCheck(t, `struct Pt { x: i64 }
alias P2 = Pt
impl Pt { let one = pure () -> i64 => 1 }
impl P2 { let one = pure () -> i64 => 2 }
let a = Pt.one()`, false)
	assertHasErrorContaining(t, res, "Pt.one is ambiguous")
}
