package typechecker_test

import (
	"testing"
)

// A PascalCase name in value position that owns no constructor used to infer as a
// silent nil, so `Rng.seeded(42)` type-checked clean and then died in the backend as
// `llvm: unsupported method call "seeded"` — the backend refusing a form the front end
// had never looked at, which is hazard 5 inverted.
//
// A type with no function of that name says so, and names where one would go. The tests of
// what such a call does when there is one are in associated_function_test.go.
func TestTypeNameAsValue_MemberCallOnAStructName(t *testing.T) {
	res := parseCollectAndCheck(t, `
		struct Rng {
			state: u64,
		}
		let go = () -> void => {
			let r = Rng.seeded(42)
		}
	`, false)
	assertErrorsAre(t, res,
		`Rng has no associated function "seeded"; declare one with no `+"`self`"+` in an `+"`impl`"+` block`)
}

// The receiver is what is wrong, so the diagnostic does not depend on the member
// being a call: a plain access and a bare mention report identically. Reporting at
// the receiver rather than at each of the three consumers is what keeps them
// consistent (hazard 8).
func TestTypeNameAsValue_WithoutACall(t *testing.T) {
	for _, source := range []string{
		`struct Rng { state: u64 }
let go = () -> void => { let s = Rng.state }`,
		`struct Rng { state: u64 }
let go = () -> void => { let r = Rng }`,
	} {
		res := parseCollectAndCheck(t, source, false)
		assertErrorsAre(t, res,
			"Rng is a type, not a value; write Rng { … } or Rng(…) to build one, or Rng.function(…) to call one it declares in an `impl` block")
	}
}

// A data type's *name* is not one of its constructors — `Maybe` is a type and `Some`
// is the value — so the type name gets the same diagnostic as a struct's.
func TestTypeNameAsValue_DataTypeName(t *testing.T) {
	res := parseCollectAndCheck(t, `
		data Shape = Circle(i64) | Square(i64)
		let go = () -> void => {
			let s = Shape.make(1)
		}
	`, false)
	assertErrorsAre(t, res,
		`Shape has no associated function "make"; declare one with no `+"`self`"+` in an `+"`impl`"+` block`)
}

// A trait gets its own message, because `Trait::method(…)` *is* a spelling the
// language has and `.` is a plausible way to reach for it. Naming the working form
// costs one branch and saves the reader the guess.
func TestTypeNameAsValue_TraitName(t *testing.T) {
	res := parseCollectAndCheck(t, `
		trait Greet {
			hello: (Self) -> i64,
		}
		let go = () -> void => {
			let n = Greet.hello(1)
		}
	`, false)
	assertErrorsAre(t, res,
		"Greet is a trait, not a value; to call one of its methods write Greet::method(receiver, ...)")
}

// A name that is neither is an undefined name, and says so. It reached the backend
// the same way before, since the silence was in the constructor lookup rather than in
// anything specific to types.
func TestTypeNameAsValue_UndefinedName(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let go = () -> void => {
			let x = Nonexistent.make(1)
		}
	`, false)
	assertErrorsAre(t, res, `undefined constructor or type "Nonexistent"`)
}

// The guard on all of the above: a real nullary constructor is still a value, and a
// method call on one still resolves. This is the case the silent nil existed to serve,
// and breaking it would be invisible in the tests above.
func TestTypeNameAsValue_RealNullaryConstructorStillWorks(t *testing.T) {
	res := parseCollectAndCheck(t, `
		data Color = Red | Green
		let describe = (self: Color) -> i64 => match self {
			Red => 1,
			Green => 2,
		}
		let go = () -> i64 => Red.describe()
	`, false)
	assertNoErrors(t, res)
}
