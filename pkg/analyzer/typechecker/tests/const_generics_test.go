package typechecker_test

import "testing"

// **Const generics, layer 0** (09/30): a `const N: i64` parameter sizes a fixed array
// (`[N]t`) and is solved from the argument's size, so one function takes a table of any
// length — std.genesis's `load_tiles(first, HERO)` rather than a pointer and a count.

func TestConstGenerics_SolvedFromTheArgument(t *testing.T) {
	assertNoErrors(t, parseCollectAndCheck(t, `
let total<const N: i64> = pure (xs: ref [N]i64) -> i64 => {
  var s = 0
  for x in xs { s += x }
  s + xs[0] * 0
}
let doubled<const N: i64> = pure (xs: [N]i64) -> [N]i64 => {
  var out: [N]i64 = xs
  for i in 0..<xs.len() { out[i] = xs[i] * 2 }
  out
}
let average<const M: i64> = pure (xs: ref [M]i64) -> i64 => total(xs) / xs.len()
let main = () -> void => {
  let small = #[1, 2, 3]
  let big: [5]i64 = #[1, 1, 1, 1, 1]
  let a = total(small) + total(big) + average(big)
  let d: [3]i64 = doubled(small)
}`, false))
}

func TestConstGenerics_Refusals(t *testing.T) {
	for _, c := range []struct{ name, src, want string }{
		{"two sizes for one parameter", `let dot<const N: i64> = pure (a: ref [N]i64, b: ref [N]i64) -> i64 => 0
let main = () -> void => {
  let x = dot(#[1, 2, 3], #[1, 2])
}`, "dot: the arrays sized by N must be the same size, and these are 3 and 2"},
		{"a return sized by the wrong size", `let f<const N: i64> = pure (xs: [N]i64) -> [N]i64 => xs
let main = () -> void => {
  let d: [4]i64 = f(#[1, 2, 3])
}`, "cannot assign"},
	} {
		t.Run(c.name, func(t *testing.T) {
			res := parseCollectAndCheck(t, c.src, false)
			if !hasError(res, c.want) {
				t.Errorf("expected an error containing %q; got %v", c.want, res.errors)
			}
		})
	}
}
