package typechecker_test

import "testing"

// The spelling of an array literal is its flavor (09/14): `[…]` and `[v; n]` are `[]T`,
// `#[…]` and `#[v; n]` are `[N]T`, and no context changes either.

func TestArrayLiteralFlavor_SpellingDecides(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"plain literal and repeat are dynamic": `
let f = (n: i64) -> i64 => {
  var xs = [1, 2, 3]
  xs.push(4)
  var ys = [0; n]
  ys.push(1)
  var zs = [0; 3]
  zs.push(1)
  xs.len() + ys.len() + zs.len()
}`,
		"fixed literal and repeat are fixed": `
let f = () -> i64 => {
  let xs: [3]i64 = #[1, 2, 3]
  let ys: [4]u8 = #[0; 4]
  let e: [0]i64 = #[]
  xs[2] + i64(ys[3]) + 0
}`,
		"elements still narrow within a flavor": `
let a: []u8 = [1, 2]
let b: [][]u8 = [[1], [2, 3]]
let c: [][2]u8 = [#[1, 2], #[3, 4]]
let d: [2][]u8 = #[[1], []]
let e: []u8 = []
let f = [["a"], []]`,
		"a newtype over each flavor": `
newtype Row = [3]i64
newtype Bag = []string
let r: Row = #[1, 2, 3]
let b: Bag = ["a"]`,
	}
	for name, src := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assertNoErrors(t, parseCollectAndCheck(t, src, false))
		})
	}
}

// lyra-E079 in every position a value meets an array type: the message names the other
// spelling, once, instead of two whole types.
func TestArrayLiteralFlavor_OtherSpellingRefusedEverywhere(t *testing.T) {
	t.Parallel()
	const wantFixed = "`[…]` builds a dynamic array, and StaticArray<i64, 2> is a fixed-size one — write `#[…]` for a fixed array"
	const wantDyn = "`#[…]` builds a fixed-size array, and DynamicArray<i64> is a dynamic one — write `[…]` for a dynamic array"
	cases := map[string]struct{ src, want string }{
		"annotation":          {`let a: [2]i64 = [1, 2]`, wantFixed},
		"annotation, dynamic": {`let a: []i64 = #[1, 2]`, wantDyn},
		"argument": {`
let take = (xs: [2]i64) -> i64 => xs[0]
let a = take([1, 2])`, wantFixed},
		"return":               {`let f = () -> [2]i64 => [1, 2]`, wantFixed},
		"repeat":               {`let a: [2]i64 = [0; 2]`, wantFixed},
		"nested element":       {`let a: [][2]i64 = [[1, 2]]`, wantFixed},
		"repeated value":       {`let a: [][2]i64 = [[1, 2]; 3]`, wantFixed},
		"struct field":         {"struct S { xs: [2]i64 }\nlet s = S { xs: [1, 2] }", wantFixed},
		"anonymous tuple":      {`let t: ([2]i64, i64) = ([1, 2], 3)`, wantFixed},
		"data payload":         {"data D = Full([2]i64) | Empty\nlet d = Full([1, 2])", wantFixed},
		"generic parameter":    {"let first<t> = (xs: [2]t) -> t => xs[0]\nlet a = first([1, 2])", "`[…]` builds a dynamic array, and StaticArray<t, 2> is a fixed-size one — write `#[…]` for a fixed array"},
		"generic struct field": {"struct H<t> { xs: [2]t }\nlet h = () -> H<i64> => H { xs: [1, 2] }", wantFixed},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assertErrorsAre(t, parseCollectAndCheck(t, tc.src, false), tc.want)
		})
	}
}

func TestArrayLiteralFlavor_FixedSpreadAndCount(t *testing.T) {
	t.Parallel()
	cases := map[string]struct{ src, want string }{
		"a spread cannot be fixed (lyra-E078)": {`
let xs: []i64 = [1, 2]
let a = #[...xs, 3]`, "a fixed array literal cannot spread: its length is part of its type — write `[...]` to splice into a dynamic array"},
		"a fixed repeat's count must fold (lyra-E056)": {`
let f = (n: i64) -> i64 => { let a = #[0; n]; 0 }`, "a fixed-size array's length is part of its type, so the count must be a compile-time constant; n is not a `const` — write `[v; n]` for an array sized at run time"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assertErrorsAre(t, parseCollectAndCheck(t, tc.src, false), tc.want)
		})
	}
}

// A non-generic constructor's payload was recorded as its declared type without being
// checked, so `N("y")` against `N(i64)` type-checked and failed only in the backend — and
// `Full([1, 2])` was silently built fixed, the one position the spelling did not decide.
func TestArrayLiteralFlavor_ConstructorPayloadIsChecked(t *testing.T) {
	t.Parallel()
	res := parseCollectAndCheck(t, `
data Num = N(i64) | Z
let a = N("y")`, false)
	assertErrorsAre(t, res, "N: cannot assign string to i64")
}
