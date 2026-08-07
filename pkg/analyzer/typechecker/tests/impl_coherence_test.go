package typechecker_test

import "testing"

// A trait may be implemented once per type. Two impls of one trait for one type make
// which body a call runs depend on declaration order, which is not a property a
// program should have.
//
// Accepted silently until 08/07, and it looked harmless while a trait only *added*
// methods: whichever impl won, the call had a body. It stops being harmless the moment
// a trait **overrides** something — an `Eq` impl replacing structural equality means
// two impls make `==` mean two things — which is why this is the prerequisite for the
// Eq/Ord work rather than a tidy-up.
func TestImplCoherence_DuplicateImplIsRejected(t *testing.T) {
	res := parseCollectAndCheck(t, `
		trait Show { show: (Self) -> string }
		impl Show for i64 { show = (self) => "a" }
		impl Show for i64 { show = (self) => "b" }
	`, false)
	assertErrorsAre(t, res,
		"duplicate impl: Show is already implemented for i64 at 3:3; a trait may be implemented once per type, or which impl a call uses would depend on declaration order")
}

// Reported once, at the second impl — not per call site. A call-site report would name
// a line that is correct, repeat for every call, and leave the reader hunting for the
// pair that caused it.
func TestImplCoherence_ReportedOncePerPairNotPerCall(t *testing.T) {
	res := parseCollectAndCheck(t, `
		trait Show { show: (Self) -> string }
		impl Show for i64 { show = (self) => "a" }
		impl Show for i64 { show = (self) => "b" }
		let d<t> where t: Show = (v: t) -> string => v.show()
		let one = () -> string => d(1)
		let two = () -> string => d(2)
	`, false)
	if len(res.errors) != 1 {
		t.Fatalf("want exactly one diagnostic for one duplicated pair, got %d: %v", len(res.errors), res.errors)
	}
}

// The distinctions that must not trip it: a different type, a different trait, and a
// generic target. Over-firing here would make the trait system unusable.
func TestImplCoherence_DistinctImplsAreFine(t *testing.T) {
	res := parseCollectAndCheck(t, `
		trait Show { show: (Self) -> string }
		trait Other { name: (Self) -> string }
		struct Box<t> { v: t }
		impl Show for i64 { show = (self) => "int" }
		impl Show for bool { show = (self) => "bool" }
		impl Show for Box<t> { show = (self) => "box" }
		impl Other for i64 { name = (self) => "other" }
	`, false)
	assertNoErrors(t, res)
}
