package typechecker_test

import (
	"strings"
	"testing"
)

func TestTraitImpl_UnknownTrait(t *testing.T) {
	res := parseCollectAndCheck(t, `
	impl Ghost for i64 {
		foo = (n) => n
	}
	`, false)
	assertErrorsAre(t, res, `impl: unknown trait "Ghost"`)
}

func TestTraitImpl_AllRequiredMethodsProvided(t *testing.T) {
	res := parseCollectAndCheck(t, `
	trait Show {
		show: (Self) -> string
	}

	impl Show for i64 {
		show = (n) => "x"
	}
	`, false)
	assertNoErrors(t, res)
}

func TestTraitImpl_MissingRequiredMethod(t *testing.T) {
	res := parseCollectAndCheck(t, `
	trait Show {
		show: (Self) -> string
	}

	impl Show for i64 {
	}
	`, false)
	assertErrorsAre(t, res, `impl of Show for i64: missing required method "show"`)
}

func TestTraitImpl_DefaultMethodNotRequired(t *testing.T) {
	res := parseCollectAndCheck(t, `
	trait Describable {
		describe: (Self) -> string = (self) => "thing"
	}

	impl Describable for i64 {
	}
	`, false)
	assertNoErrors(t, res)
}

func TestTraitImpl_MissingOneOfMultipleRequired(t *testing.T) {
	// Identifier method names: this is about an impl *missing* one of several required
	// methods, and the operator spellings it used were incidental — they are now
	// refused (lyra-E039), which would drown the assertion this test exists for.
	res := parseCollectAndCheck(t, `
	trait Pair {
		first: (Self) -> bool,
		second: (Self) -> bool
	}

	impl Pair for i64 {
		first = (a) => true
	}
	`, false)
	assertErrorsAre(t, res, `impl of Pair for i64: missing required method "second"`)
}

func TestTraitImpl_ExtraneousMethod(t *testing.T) {
	res := parseCollectAndCheck(t, `
	trait Show {
		show: (Self) -> string
	}

	impl Show for i64 {
		show = (n) => "x",
		extra = (n) => "y"
	}
	`, false)
	assertWarningsAre(t, res, `impl of Show for i64: method "extra" is not declared in trait`)
}

func TestTraitImpl_WrongArity(t *testing.T) {
	res := parseCollectAndCheck(t, `
	trait Show {
		show: (Self) -> string
	}

	impl Show for i64 {
		show = () => "x"
	}
	`, false)
	assertErrorsAre(t, res, `impl of Show for i64: method "show" has wrong number of parameters: expected 1, got 0`)
}

func TestTraitImpl_WrongArityTooMany(t *testing.T) {
	res := parseCollectAndCheck(t, `
	trait Show {
		show: (Self) -> string
	}

	impl Show for i64 {
		show = (a, b) => "x"
	}
	`, false)
	assertErrorsAre(t, res, `impl of Show for i64: method "show" has wrong number of parameters: expected 1, got 2`)
}

func TestTraitImpl_MixedDefaultAndRequired(t *testing.T) {
	res := parseCollectAndCheck(t, `
	trait Show {
		show: (Self) -> string,
		show_twice: (Self) -> string = (x) => "twice"
	}

	impl Show for i64 {
		show = (n) => "x"
	}
	`, false)
	assertNoErrors(t, res)
}

func TestTraitImpl_MultipleMissingMethods(t *testing.T) {
	res := parseCollectAndCheck(t, `
	trait Collection {
		add: (Self, i64) -> Self,
		remove: (Self, i64) -> Self,
		size: (Self) -> i64
	}

	impl Collection for i64 {
	}
	`, false)
	if len(res.errors) != 3 {
		t.Errorf("expected 3 errors, got %d:", len(res.errors))
		for _, e := range res.errors {
			t.Errorf("  %s", e.Message)
		}
	}
}

// ── Impl coherence is keyed on the trait *declaration*, not its name (08/14) ──

// A module may declare its own `Add` beside the prelude's, and `impl Add for i64` in each
// is two impls of two *different* traits. Keying the duplicate check on the trait's name
// called them duplicates and refused a correct program — hazard 9 (a name does not
// identify a declaration), and the same mistake dispatch already avoids.
//
// Reachable before the prelude shipped arithmetic impls, via `trait Show` over
// std/prelude/show.lyra's `impl Show for i64`; latent only because nothing wrote it.
func TestImplCoherence_OwnTraitDoesNotCollideWithThePrelude(t *testing.T) {
	res := parseCollectAndCheck(t, `
		trait Show { show: (Self) -> string }
		impl Show for i64 { show = (self) => "mine" }
	`, false)
	assertNoErrors(t, res)
}

// The check must still bite: two impls of the *same* trait for one type is the ambiguity
// it exists for, and widening the key must not have widened it into uselessness.
func TestImplCoherence_RealDuplicateIsStillRefused(t *testing.T) {
	res := parseCollectAndCheck(t, `
		trait Greet { hi: (Self) -> string }
		struct Pt { x: i64 }
		impl Greet for Pt { hi = (self) => "a" }
		impl Greet for Pt { hi = (self) => "b" }
	`, false)
	if len(res.errors) == 0 {
		t.Fatal("a second impl of one trait for one type must be refused")
	}
	if !strings.Contains(res.errors[0].Error(), "already implemented") {
		t.Errorf("want the duplicate-impl diagnostic, got: %v", res.errors[0])
	}
}
