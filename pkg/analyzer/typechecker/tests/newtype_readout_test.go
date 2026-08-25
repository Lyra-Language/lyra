package typechecker_test

import "testing"

// The newtype read-out conversion, in all four positions a binding can come from.
//
// `i64(c)` is the spelling lyra-E047 names as *the* way to read a newtype's value back out.
// It was accepted for a plain binding and for a field access, and refused — "cannot convert
// Meters to i64" — when the binding came from a **match pattern** or a **destructuring**,
// which is the error telling you to write the thing you just wrote.
//
// The cause: the conversion strips newtype wrappers in a loop, resolving each base as it
// goes because "a chained newtype's base is stored as a name" — but it took the *operand's*
// own type as it came, and a pattern-bound binding arrives as the bare name rather than as a
// resolved `*ConstrainedType`. So the loop saw a name it had no arm for and fell through to
// "not numeric". One resolve on the way in, matching what the loop already did for every
// base after the first.
func TestTypeCheck_NewtypeReadOutFromEveryBindingPosition(t *testing.T) {
	const decl = `
newtype Meters = i64
struct Leg { d: Meters, n: i64 }
`
	for _, c := range []struct{ name, body string }{
		{"plain binding", `
let d: Meters = 5
let out = i64(d)`},
		{"field access", `
let l = Leg { d: 5, n: 2 }
let out = i64(l.d)`},
		{"match pattern", `
let l = Leg { d: 5, n: 2 }
let out = match l { { d, n } => i64(d) + n }`},
		{"destructuring", `
let l = Leg { d: 5, n: 2 }
let { d, n } = l
let out = i64(d) + n`},
	} {
		t.Run(c.name, func(t *testing.T) {
			assertNoErrors(t, parseCollectAndCheck(t, decl+c.body, false))
		})
	}
}

// Resolving the operand must not make the conversion *permissive*. Each of these is refused
// for its own reason, and a strip that resolved too eagerly — or that dropped the wrapper
// before the target was compared — would quietly admit them.
func TestTypeCheck_ConversionStillRefusesWhatItShould(t *testing.T) {
	for _, c := range []struct{ name, src, want string }{
		{"string to i64", `
let s = "abc"
let out = i64(s)`, "cannot convert string to i64"},
		// A newtype over string: stripping is right, and the *base* is then what fails.
		{"newtype over string to i64", `
newtype Name = string
let n: Name = "x"
let out = i64(n)`, "cannot convert string to i64"},
		{"lossy float", `
let f = 1.5
let out = i64(f)`, "cannot convert f64 to i64: use floor(), ceil(), or round() to convert explicitly"},
		{"bool is identity-only", `
let n = 1
let out = bool(n)`, "cannot convert i64 to bool: `bool(...)` only reads a value of that type — or a newtype over it — back out"},
	} {
		t.Run(c.name, func(t *testing.T) {
			assertErrorsAre(t, parseCollectAndCheck(t, c.src, false), c.want)
		})
	}
}
