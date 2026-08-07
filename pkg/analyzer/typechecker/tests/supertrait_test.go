package typechecker_test

import "testing"

// A supertrait is a promise that every implementer of a trait also implements the ones
// it names — which is what would let a `where t: B` bound reach `A`'s methods.
//
// It was never checked. `TraitDeclStmt.Bounds` was collected and read by nobody, found
// 08/07 by sweeping the AST for fields with no consumer: of 119 exported fields, this
// was one of two genuine phantoms. So `trait B: A` parsed, `impl B for S` compiled with
// no `A` in sight, and the declaration said something the compiler did not enforce.
func TestSupertrait_UnsatisfiedIsRefused(t *testing.T) {
	res := parseCollectAndCheck(t, `
		trait A { foo: (Self) -> i64 }
		trait B: A { bar: (Self) -> i64 }
		struct Pt { x: i64 }
		impl B for Pt { bar = (self) => 2 }
	`, false)
	assertErrorsAre(t, res,
		"impl of B for Pt: B requires A, which Pt does not implement")
}

func TestSupertrait_SatisfiedIsFine(t *testing.T) {
	res := parseCollectAndCheck(t, `
		trait A { foo: (Self) -> i64 }
		trait B: A { bar: (Self) -> i64 }
		struct Pt { x: i64 }
		impl A for Pt { foo = (self) => 1 }
		impl B for Pt { bar = (self) => 2 }
	`, false)
	assertNoErrors(t, res)
}

// Declaration order must not matter: the impls are gathered up front precisely so a
// call can dispatch against one declared later, and the same has to hold here.
func TestSupertrait_SatisfiedByALaterImpl(t *testing.T) {
	res := parseCollectAndCheck(t, `
		trait A { foo: (Self) -> i64 }
		trait B: A { bar: (Self) -> i64 }
		struct Pt { x: i64 }
		impl B for Pt { bar = (self) => 2 }
		impl A for Pt { foo = (self) => 1 }
	`, false)
	assertNoErrors(t, res)
}

// A trait with no supertraits is unaffected — the check must not fire on the ordinary
// case, which is nearly every trait.
func TestSupertrait_PlainTraitIsUnaffected(t *testing.T) {
	res := parseCollectAndCheck(t, `
		trait Show { show: (Self) -> string }
		struct Pt { x: i64 }
		impl Show for Pt { show = (self) => "pt" }
	`, false)
	assertNoErrors(t, res)
}
