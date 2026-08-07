package typechecker_test

import "testing"

// `where` bounds on a generic binding, which until 08/07 were collected and read by
// nobody. Two halves, and the first is what made the second worth having:
//
//   - a bound is in scope for the *body*, so a call on a value of the bounded type
//     parameter dispatches through it. Before, `v.show()` under `where t: Show`
//     reported "type parameter t has no method `show`; add a `where t: Trait` bound"
//     — naming the fix the author had already applied;
//   - a bound is enforced at the *instantiation*, so a generic cannot be used at a
//     type that does not satisfy it. Before, that type-checked clean and failed in
//     the backend with `llvm: unsupported method call`.
const showTrait = `
trait Show { show: (Self) -> string }
impl Show for i64 {
  show = (self) => "int"
}
`

func TestWhereBound_InScopeForTheBody(t *testing.T) {
	res := parseCollectAndCheck(t, showTrait+`
		let describe<t> where t: Show = (v: t) -> string => v.show()
		let go = () -> string => describe(7)
	`, false)
	assertNoErrors(t, res)
}

func TestWhereBound_UnsatisfiedAtInstantiation(t *testing.T) {
	res := parseCollectAndCheck(t, showTrait+`
		struct Pt { x: i64 }
		let describe<t> where t: Show = (v: t) -> string => v.show()
		let go = () -> string => describe(Pt { x: 1 })
	`, false)
	assertErrorsAre(t, res,
		"describe: t is instantiated at Pt, which does not implement Show (required by `where t: Show`)")
}

// The bound travels: forwarding into a bounded generic is legal exactly when the
// enclosing declaration carries the same bound. Checking a type *variable* against an
// impl would be wrong — there is no impl for `u` — so the question is whether the
// enclosing scope bounds it, which is what makes a correctly-forwarded bound compile.
func TestWhereBound_ForwardedThroughAnotherGeneric(t *testing.T) {
	res := parseCollectAndCheck(t, showTrait+`
		let describe<t> where t: Show = (v: t) -> string => v.show()
		let via<u> where u: Show = (x: u) -> string => describe(x)
		let go = () -> string => via(7)
	`, false)
	assertNoErrors(t, res)
}

// …and refused when it is not, with the message naming the bound to add rather than
// the impl that is missing — there is no impl to write for a type variable.
func TestWhereBound_ForwardedWithoutTheBound(t *testing.T) {
	res := parseCollectAndCheck(t, showTrait+`
		let describe<t> where t: Show = (v: t) -> string => v.show()
		let unbounded<u> = (x: u) -> string => describe(x)
	`, false)
	assertErrorsAre(t, res,
		"describe: t is instantiated at the type parameter u, which is not bound by Show; add `where u: Show` to the enclosing declaration")
}

// An unbounded generic is unaffected — the check must not make every generic call
// pay for a feature it does not use.
func TestWhereBound_UnboundedGenericIsUnconstrained(t *testing.T) {
	res := parseCollectAndCheck(t, showTrait+`
		struct Pt { x: i64 }
		let id<t> = (v: t) -> t => v
		let go = () -> Pt => id(Pt { x: 1 })
	`, false)
	assertNoErrors(t, res)
}
