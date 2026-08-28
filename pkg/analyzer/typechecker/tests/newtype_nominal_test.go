package typechecker_test

import "testing"

// Keeping a newtype's wrapper across a call boundary must not make it less **nominal** —
// nominal identity is the whole reason newtype exists.
//
// The read-out rule is uniform (08/28): a base a conversion can name is refused with that
// name (`i64(...)`), and every other base with the universal `base(...)`. The asymmetry
// this file used to pin — an array base flowing implicitly because nothing could spell its
// read-out — is retired.
func TestTypeCheck_NewtypeStaysNominalAcrossCalls(t *testing.T) {
	for _, c := range []struct{ name, src, want string }{
		// Passed before the fix as well — the base was never implicitly admitted. Kept
		// because it is the half a change like this could plausibly loosen.
		{"base does not flow into a newtype", `
newtype Bag = []string
let take = (b: Bag) -> i64 => 1
let xs: []string = ["x"]
let out = take(xs)`,
			"cannot use DynamicArray<string> as Bag implicitly: Bag is a distinct type over DynamicArray<string>, so the conversion must be written — `Bag(...)`"},
		// Two newtypes over one base are distinct. The diagnostic names *both* newtypes,
		// which it could not do while the wrapper was being lost — it used to describe the
		// base instead.
		{"two newtypes over one base", `
newtype Bag = []string
newtype Sack = []string
let take = (s: Sack) -> i64 => 1
let b: Bag = ["x"]
let out = take(b)`,
			"take: argument 1 (s): cannot assign Bag to Sack"},
		// Also passed before: a scalar base never lost its wrapper.
		{"a scalar newtype needs its read-out written", `
newtype Cents = i64
let take = (n: i64) -> i64 => n
let c: Cents = 5
let out = take(c)`,
			"cannot use Cents as i64 implicitly: reading a newtype out discards the name it carries, so the conversion must be written — `i64(...)`"},
	} {
		t.Run(c.name, func(t *testing.T) {
			assertErrorsAre(t, parseCollectAndCheck(t, c.src, false), c.want)
		})
	}
}

// The asymmetry is retired (08/28): an array base is refused implicitly at a call
// boundary exactly as a scalar one is, and `base(...)` is the spelling offered.
func TestTypeCheck_NewtypeOverAnArrayIsRefusedAtCallsToo(t *testing.T) {
	res := parseCollectAndCheck(t, `
newtype Bag = []string
let take = (xs: []string) -> i64 => 1
let b: Bag = ["x"]
let out = take(b)
`, false)
	assertErrorsAre(t, res,
		"cannot use Bag as DynamicArray<string> implicitly: reading a newtype out discards the name it carries, so the conversion must be written — `base(...)`")
}

// …and writing the read-out is the way through at a call, as everywhere.
func TestTypeCheck_NewtypeOverAnArrayReadsOutWithBase(t *testing.T) {
	assertNoErrors(t, parseCollectAndCheck(t, `
newtype Bag = []string
let take = (xs: []string) -> i64 => 1
let b: Bag = ["x"]
let out = take(base(b))
`, false))
}
