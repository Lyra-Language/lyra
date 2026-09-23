package llvm

import "testing"

// **`|` in a pattern matches any of its alternatives**, and a range over a `rune` is
// written in runes (09/23). Both came out of the collector written in Lyra: classifying a
// character wanted `'0'..<='9'`, and a set of node kinds sharing one arm wanted `|`.
//
// The lowering is one `or` of the alternatives' own tests, built in `scalarMatchTest`
// before it dispatches on the scrutinee's shape — so strings, runes, floats and integers
// get the same answer from one place rather than a copy per delegate, which is hazard 8's
// shape. These cases exist to hold that: each scrutinee kind appears at least once.
func TestExec_OrPatternAndRuneRanges(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		src  string
		want int
	}{
		{
			// A rune range, which was `range patterns are not allowed on a rune
			// scrutinee` until the bounds could be runes.
			"a rune range matches inside and not outside",
			`let digit = pure (d: rune) -> i64 => match d {
  '0'..<='9' => 1,
  _ => 0,
}
let main = () -> u8 => u8(digit('7') * 10 + digit('z'))`,
			10,
		},
		{
			// The case the collector wanted: two ranges sharing one arm.
			"alternation of rune ranges",
			`let hex = pure (d: rune) -> i64 => match d {
  'a'..<='f' | 'A'..<='F' => 1,
  _ => 0,
}
let main = () -> u8 => u8(hex('c') + hex('E') + hex('7'))`,
			2,
		},
		{
			"alternation of strings",
			`let method = pure (s: string) -> i64 => match s {
  "get" | "post" | "put" => 1,
  _ => 0,
}
let main = () -> u8 => u8(method("post") + method("put") + method("head"))`,
			2,
		},
		{
			"alternation of integers",
			`let small = pure (n: i64) -> i64 => match n {
  1 | 2 | 3 => 1,
  _ => 0,
}
let main = () -> u8 => u8(small(2) + small(3) + small(9))`,
			2,
		},
		{
			// The alternatives are tried in order and any one suffices, so a value in
			// the *last* alternative matches as surely as one in the first.
			"the last alternative still matches",
			`let f = pure (n: i64) -> i64 => match n {
  1 | 2 | 3 | 4 | 5 => 7,
  _ => 0,
}
let main = () -> u8 => u8(f(5))`,
			7,
		},
		{
			// `|` is also the bitwise operator, and the scrutinee is an expression: the
			// two readings sit two lines apart and must stay apart.
			"a bitor expression beside an alternation pattern",
			`let f = pure (a: i64, b: i64) -> i64 => match a | b {
  3 | 7 => 1,
  _ => 0,
}
let main = () -> u8 => u8(f(1, 2) + f(8, 8))`,
			1,
		},
		{
			// **An alternation makes a match exhaustive**, which is the expandAlternations
			// half: each analysis sees one row per alternative. Bool rather than a numeric
			// range on purpose — a non-exhaustive numeric match is only a *warning*, so it
			// compiles either way and a test built on one proves nothing. Missing bool arms
			// are an error, so this case fails to build without the expansion.
			"an alternation that makes a match exhaustive",
			`let g = pure (b: bool) -> i64 => match b {
  true | false => 4,
}
let main = () -> u8 => u8(g(true) + g(false))`,
			8,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if got := buildAndRun(t, c.src); got != c.want {
				t.Errorf("exited %d; want %d", got, c.want)
			}
		})
	}
}
