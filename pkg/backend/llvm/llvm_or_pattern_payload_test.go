package llvm

import "testing"

// **An alternative may test a payload, and an alternation may sit below the top of a pattern**
// (11/03): `Some(_) | None`, `Some(1 | 2)`, `(0, _) | (_, 0)`. Only a test: an alternative that
// binds is lyra-E090.
//
// One lowering per shape of match. A `data` match with no payload test keeps its tag `switch`,
// one case per alternative; a test of a payload takes the ladder, an `or` of the alternatives'
// tests; an array match, whose test spans blocks, fails over from one alternative to the next.
func TestExec_OrPatternAlternativesTestPayloads(t *testing.T) {
	t.Parallel()
	const types = `data Opt = Yes(i64) | No
data Tool = Pencil | Eraser(i64) | Brush(i64, i64)
data Deep = Wrap(Opt) | Bare
struct Pt { x: i64, y: i64 }
`
	cases := []struct {
		name string
		src  string
		want int
	}{
		{
			"a payload ignored beside a nullary constructor takes the tag switch",
			`let f = pure (o: Opt) -> i64 => match o {
  Yes(_) | No => 3,
}
let main = () -> u8 => u8(f(Yes(1)) + f(No))`,
			6,
		},
		{
			"the wildcard payload written without parentheses",
			`let f = pure (o: Opt) -> i64 => match o {
  Yes _ | No => 3,
}
let main = () -> u8 => u8(f(Yes(1)) + f(No))`,
			6,
		},
		{
			"a tested payload takes the ladder",
			`let f = pure (o: Opt) -> i64 => match o {
  Yes(0) | No => 1,
  Yes(_) => 2,
}
let main = () -> u8 => u8(f(Yes(0)) + f(No) * 10 + f(Yes(5)) * 100)`,
			1 + 10 + 200,
		},
		{
			"a juxtaposed payload ends at the bar",
			`let f = pure (o: Opt) -> i64 => match o {
  Yes 0 | Yes 1 | No => 1,
  Yes _ => 2,
}
let main = () -> u8 => u8(f(Yes(1)) + f(No) * 10 + f(Yes(5)) * 100)`,
			1 + 10 + 200,
		},
		{
			"an alternation inside a payload",
			`let f = pure (o: Opt) -> i64 => match o {
  Yes(1 | 2) => 1,
  Yes(_) => 2,
  No => 3,
}
let main = () -> u8 => u8(f(Yes(2)) + f(Yes(3)) * 10 + f(No) * 50)`,
			1 + 20 + 150,
		},
		{
			"several fields, each with its own test",
			`let f = pure (t: Tool) -> i64 => match t {
  Pencil | Eraser(_) => 1,
  Brush(1 | 2, 3 | 4) => 2,
  Brush(_, _) => 3,
}
let main = () -> u8 => u8(f(Pencil) + f(Eraser(9)) * 2 + f(Brush(2, 4)) * 10 + f(Brush(2, 5)) * 30)`,
			1 + 2 + 20 + 90,
		},
		{
			"a nested constructor alternation covers its type",
			`let f = pure (d: Deep) -> i64 => match d {
  Wrap(Yes(_) | No) => 1,
  Bare => 2,
}
let main = () -> u8 => u8(f(Wrap(No)) + f(Wrap(Yes(1))) * 2 + f(Bare) * 10)`,
			1 + 2 + 20,
		},
		{
			"tuple alternatives",
			`let f = pure (t: (i64, i64)) -> i64 => match t {
  (0, _) | (_, 0) => 1,
  (1 | 2, 3 | 4) => 2,
  _ => 3,
}
let main = () -> u8 => u8(f((0, 5)) + f((5, 0)) * 2 + f((2, 4)) * 10 + f((3, 3)) * 30)`,
			1 + 2 + 20 + 90,
		},
		{
			"struct alternatives",
			`let f = pure (p: Pt) -> i64 => match p {
  { x: 0 } | { y: 0 } => 1,
  { x: 1 | 2 } => 2,
  _ => 3,
}
let main = () -> u8 => u8(f(Pt { x: 0, y: 1 }) + f(Pt { x: 1, y: 0 }) * 2 + f(Pt { x: 2, y: 2 }) * 10 + f(Pt { x: 3, y: 3 }) * 30)`,
			1 + 2 + 20 + 90,
		},
		{
			"a name on the whole alternation binds what matched",
			`let f = pure (o: Opt) -> i64 => match o {
  w @ Yes(1 | 2) | No => match w { Yes(n) => n, No => 9 },
  _ => 0,
}
let main = () -> u8 => u8(f(Yes(2)) + f(No) * 10)`,
			92,
		},
		{
			"an `if let`, a `while let` and a `let … else`",
			`let f = pure (o: Opt) -> i64 => {
  if let Yes(1 | 2) | No = o { return 1 }
  let Yes(5 | 6) = o else { return 2 }
  3
}
let g = () -> i64 => {
  var o = Yes(0)
  var n = 0
  while let Yes(0 | 1) | No = o {
    n = n + 1
    o = if n == 1 { No } else { Yes(9) }
  }
  n
}
let main = () -> u8 => u8(f(Yes(1)) + f(No) * 2 + f(Yes(9)) * 10 + f(Yes(5)) * 30 + g() * 10)`,
			1 + 2 + 20 + 90 + 20,
		},
		{
			"a guard runs after any alternative",
			`let f = pure (o: Opt, ok: bool) -> i64 => match o {
  Yes(_) | No if ok => 1,
  Yes(_) => 2,
  No => 3,
}
let main = () -> u8 => u8(f(No, true) + f(Yes(1), false) * 10 + f(No, false) * 50)`,
			1 + 20 + 150,
		},
		{
			"a later arm covered in part stays reachable",
			`let f = pure (o: Opt) -> i64 => match o {
  No => 1,
  Yes(_) | No => 2,
}
let main = () -> u8 => u8(f(No) + f(Yes(0)) * 10)`,
			1 + 20,
		},
		{
			"a constructor alternation in a lambda's parameter",
			`let f = pure (o: Opt) -> i64 => {
  let g = (x: Opt) -> i64 {
    (Yes(_) | No) => 7,
  }
  g(o)
}
let main = () -> u8 => u8(f(No))`,
			7,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if got := buildAndRun(t, types+c.src); got != c.want {
				t.Errorf("exited %d; want %d", got, c.want)
			}
		})
	}
}

// An array match's test spans blocks, so an alternation there fails over from one alternative
// to the next rather than being an `or`, and an element's alternation is the scalar one.
func TestExec_OrPatternOverArrays(t *testing.T) {
	t.Parallel()
	src := `let f = pure (s: []i64) -> i64 => match s {
  [] | [_] => 1,
  [1 | 2, ...tail] => 2 + tail.len(),
  [_, 5] | [5, _] => 3,
  _ => 4,
}
let main = () -> u8 => {
  let codes = [f([]), f([9]), f([2, 7, 7]), f([9, 5]), f([5, 9]), f([9, 9])]
  if codes == [1, 1, 4, 3, 3, 4] { 0 } else { 1 }
}`
	if got := buildAndRunASanWithPrelude(t, src); got != 0 {
		t.Errorf("exited %d; want 0", got)
	}
}

// **An arm's alternatives test managed payloads without owning them**: no alternative binds, so
// no path through one retains or releases anything, whichever matched. Under ASan because a
// mistake here reads freed memory or leaves a double release, and a right answer does not show
// either.
func TestExec_OrPatternOverManagedPayloads(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		src  string
	}{
		{
			"strings in a payload",
			`let f = pure (m: Maybe<string>) -> i64 => match m {
  Some("a" | "b") | None => 1,
  Some(_) => 2,
}
let main = () -> u8 => {
  let s = "" ++ "a"
  if f(Some(s)) == 1 && f(None) == 1 && f(Some("c" ++ "d")) == 2 { 0 } else { 1 }
}`,
		},
		{
			// Not an alternation at all, and the same read: the shape below comes down to this.
			"a string test against another variant's payload, alone",
			`data D = Text(string) | Pair(i64, i64) | Nil
let f = pure (d: D) -> i64 => match d {
  Text("x") => 1,
  _ => 2,
}
let main = () -> u8 => if f(Pair(0, 1)) == 2 && f(Text("x")) == 1 && f(Nil) == 2 { 0 } else { 1 }`,
		},
		{
			// The alternative that does not hold still reads its own payload's shape out of
			// another variant's bits, so a string test must survive not being a string there.
			"a string test against another variant's payload",
			`data D = Text(string) | Pair(i64, i64) | Nil
let f = pure (d: D) -> i64 => match d {
  Text("x" | "y") | Pair(5, _) => 1,
  Text(_) | Pair(_, _) => 2,
  Nil => 3,
}
let main = () -> u8 => {
  if f(Pair(0, 1)) == 2 && f(Pair(5, 1)) == 1 && f(Text("y")) == 1 && f(Text("z")) == 2 && f(Nil) == 3 { 0 } else { 1 }
}`,
		},
		{
			"a shared recursive type through a tag switch",
			`data List = Cons(i64, shared List) | Nil
let is_list = (l: List) -> i64 => match l {
  Cons(_, _) | Nil => 1,
}
let main = () -> u8 => {
  let l = Cons(1, Cons(2, Nil))
  if is_list(l) + is_list(Nil) == 2 { 0 } else { 1 }
}`,
		},
		{
			"a shared recursive type, testing its payload",
			`data List = Cons(i64, shared List) | Nil
let starts = (l: List) -> i64 => match l {
  Cons(1 | 2, _) | Nil => 1,
  Cons(_, _) => 2,
}
let main = () -> u8 => {
  let l = Cons(2, Cons(3, Nil))
  let m = Cons(9, Nil)
  if starts(l) == 1 && starts(m) == 2 && starts(Nil) == 1 { 0 } else { 1 }
}`,
		},
		{
			// The match rebuilds the list in its other arm, which is the shape the reuse
			// analysis looks for: it must decline an arm it cannot read as a tag switch.
			"a rebuilt list beside an alternation is not reused from",
			`data List = Cons(i64, shared List) | Nil
let bump = (l: own List) -> List => match l {
  Cons(0 | 1, _) | Nil => Nil,
  Cons(h, t) => Cons(h + 1, t),
}
let main = () -> u8 => {
  let a = bump(Cons(5, Cons(6, Nil)))
  let b = bump(Cons(1, Nil))
  let got_a = match a { Cons(n, _) => n, Nil => 0 }
  let got_b = match b { Cons(n, _) => n, Nil => 0 }
  if got_a == 6 && got_b == 0 { 0 } else { 1 }
}`,
		},
		{
			"arrays in a tuple",
			`let f = pure (t: ([]i64, string)) -> i64 => match t {
  (_, "a" | "b") => 1,
  _ => 2,
}
let main = () -> u8 => {
  let xs = [1, 2, 3]
  if f((xs, "b")) == 1 && f((xs, "z")) == 2 && xs.len() == 3 { 0 } else { 1 }
}`,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if got := buildAndRunASanWithPrelude(t, c.src); got != 0 {
				t.Errorf("exited %d; want 0", got)
			}
		})
	}
}
