package llvm

import (
	"strings"
	"testing"
)

// An operand already evaluated beside one that leaves. `("a" ++ b, one(n)?)`: the first
// operand is owned, its aggregate or call hasn't taken it yet, and nothing else knows of it,
// so until 11/03 every way out leaked it. Each case runs the path that leaves and the one that
// doesn't, so it says both that the leaving path releases and that the other doesn't release
// as well. The answer was always right: only the leak check on Linux sees the first, and ASan
// the second.
func TestExec_OperandsBesideAnExit(t *testing.T) {
	t.Parallel()
	const lib = `
struct Two { x: string, y: string }
struct Num { x: string, n: i64 }
struct First { n: i64, x: string }
struct Defaulted { xs: []string = [], n: i64 }
data Tag = Tagged(string, string) | Plain
data Shape = Box { label: string, n: i64 } | Dot
let one = (n: i64) -> Maybe<string> => if n > 1 { None } else { Some("${n}" ++ "!") }
let num = (n: i64) -> Maybe<i64> => if n > 1 { None } else { Some(n) }
let bad = (n: i64) -> Result<string, string> => if n > 1 { Err("no" ++ "${n}") } else { Ok("${n}" ++ "!") }
let glue = (a: own string, b: own string) -> string => a ++ b
let glue_arr = (a: own []string, b: own string) -> string => a[0] ++ b
let lend = (a: string, b: string) -> string => a ++ b
trait Into { into_with: (own Self, own string) -> string }
impl Into for string { into_with = (self, b) => self ++ b }
let total = (a: own string, n: i64) -> i64 => a.byte_len() + n
let b = "bc"
`
	cases := []struct{ name, src string }{
		// The three the leak was found on.
		{"a dynamic array", `
let run = (n: i64) -> Maybe<i64> => Some(["a" ++ b, one(n)?].len())`},
		{"a tuple", `
let run = (n: i64) -> Maybe<i64> => {
  let t = ("a" ++ b, one(n)?)
  Some(t.0.byte_len() + t.1.byte_len())
}`},
		{"a call taking `own`", `
let run = (n: i64) -> Maybe<i64> => Some(glue("a" ++ b, one(n)?).byte_len())`},

		// The other aggregates.
		{"a fixed-size array", `
let run = (n: i64) -> Maybe<i64> => {
  let t = #["a" ++ b, one(n)?]
  Some(t[0].byte_len())
}`},
		{"a struct", `
let run = (n: i64) -> Maybe<i64> => {
  let t = Two { x: "a" ++ b, y: one(n)? }
  Some(t.x.byte_len() + t.y.byte_len())
}`},
		{"a struct, its `?` declared first", `
let run = (n: i64) -> Maybe<i64> => {
  let t = First { x: "a" ++ b, n: num(n)? }
  Some(t.x.byte_len() + t.n)
}`},
		{"a struct, written out of order", `
let run = (n: i64) -> Maybe<i64> => {
  let t = Two { y: one(n)?, x: "a" ++ b }
  Some(t.x.byte_len() + t.y.byte_len())
}`},
		{"a struct, the `?` numeric", `
let run = (n: i64) -> Maybe<i64> => {
  let t = Num { x: "a" ++ b, n: num(n)? }
  Some(t.x.byte_len() + t.n)
}`},
		{"a struct's filled-in default", `
let run = (n: i64) -> Maybe<i64> => {
  let t = Defaulted { n: num(n)? }
  Some(t.xs.len() + t.n)
}`},
		{"a record update", `
let run = (n: i64) -> Maybe<i64> => {
  let base = Two { x: "a" ++ b, y: "z" ++ b }
  let t = Two { base | y: one(n)? }
  Some(t.x.byte_len() + t.y.byte_len() + base.x.byte_len())
}`},
		{"an anonymous struct", `
let run = (n: i64) -> Maybe<i64> => {
  let t = { x: "a" ++ b, y: one(n)? }
  Some(t.x.byte_len() + t.y.byte_len())
}`},
		{"a data constructor", `
let run = (n: i64) -> Maybe<i64> => {
  let t = Tagged("a" ++ b, one(n)?)
  Some(match t { Tagged(x, y) => x.byte_len() + y.byte_len(), Plain => 0 })
}`},
		{"a data constructor's record", `
let run = (n: i64) -> Maybe<i64> => {
  let t = Box { label: "a" ++ b, n: num(n)? }
  Some(match t { Box { label, n } => label.byte_len() + n, Dot => 0 })
}`},
		{"a `shared` tuple's parts", `
let run = (n: i64) -> Maybe<i64> => {
  let t: shared (string, string) = ("a" ++ b, one(n)?)
  Some(t.0.byte_len())
}`},
		{"an array repeated, the count leaving", `
let run = (n: i64) -> Maybe<i64> => Some(["a" ++ b; num(n)? + 1].len())`},
		{"an array repeated, a temporary in the count", `
let run = (n: i64) -> Maybe<i64> => Some([n; ("a" ++ b).byte_len()].len() + (num(n)?))`},

		// The calls.
		{"a nested function taking `own`, called by its name", `
let run = (n: i64) -> Maybe<i64> => {
  let f = (a: own string, c: own string) -> string => glue(a, c)
  Some(f("a" ++ b, one(n)?).byte_len())
}`},
		{"a method taking `own`, on its receiver", `
let run = (n: i64) -> Maybe<i64> => Some(("a" ++ b).into_with(one(n)?).byte_len())`},
		{"a borrowed argument beside an owned one", `
let run = (n: i64) -> Maybe<i64> => Some(total("a" ++ b, num(n)?))`},
		{"a borrowed argument, as it always was", `
let run = (n: i64) -> Maybe<i64> => Some(lend("a" ++ b, one(n)?).byte_len())`},

		// Where the operand came from.
		{"a binding at its last use", `
let run = (n: i64) -> Maybe<i64> => {
  let s = "a" ++ b
  Some([s, one(n)?].len())
}`},
		{"a binding used again after", `
let run = (n: i64) -> Maybe<i64> => {
  let s = "a" ++ b
  let t = (s, one(n)?)
  Some(t.0.byte_len() + s.byte_len())
}`},
		{"a field, retained", `
let run = (n: i64) -> Maybe<i64> => {
  let w = Two { x: "a" ++ b, y: "z" }
  Some(glue(w.x, one(n)?).byte_len() + w.x.byte_len())
}`},
		{"an `if` as the operand", `
let run = (n: i64) -> Maybe<i64> => {
  let t = (if n > 0 { "a" ++ b } else { "z" ++ b }, one(n)?)
  Some(t.0.byte_len())
}`},
		{"a literal, pinned", `
let run = (n: i64) -> Maybe<i64> => {
  let t = ("lit", one(n)?)
  Some(t.0.byte_len())
}`},

		// Nothing owned in the operands: the box alone.
		{"a dynamic array of numbers", `
let run = (n: i64) -> Maybe<i64> => Some([1, num(n)?].len())`},
		{"a dynamic array, the `?` first", `
let run = (n: i64) -> Maybe<i64> => Some([one(n)?, "a" ++ b].len())`},
		{"a spread beside the `?`", `
let run = (n: i64) -> Maybe<i64> => {
  let xs = ["p" ++ b, "q" ++ b]
  Some([...xs, "a" ++ b, one(n)?].len())
}`},

		// Nesting.
		{"an aggregate in an aggregate", `
let run = (n: i64) -> Maybe<i64> => {
  let t = (("a" ++ b, "x" ++ b), [one(n)?])
  Some(t.0.0.byte_len() + t.1.len())
}`},
		{"the `?` a level down", `
let run = (n: i64) -> Maybe<i64> => {
  let t = ("a" ++ b, ("x" ++ b, one(n)?))
  Some(t.0.byte_len() + t.1.1.byte_len())
}`},
		{"an array in a call in a tuple", `
let run = (n: i64) -> Maybe<i64> => {
  let t = ("a" ++ b, glue_arr(["x" ++ b], one(n)?))
  Some(t.1.byte_len())
}`},
		{"in a comprehension's result", `
let run = (n: i64) -> Maybe<i64> => Some([x in [0, n] | ("a" ++ b, one(x)?)].len())`},
		{"as the value returned", `
let make = (n: i64) -> Maybe<(string, string)> => Some(("a" ++ b, one(n)?))
let run = (n: i64) -> Maybe<i64> => {
  let t = make(n)?
  Some(t.0.byte_len())
}`},
		{"a `?` carrying a managed error", `
let two = (n: i64) -> Result<(string, string), string> => Ok(("a" ++ b, bad(n)?))
let run = (n: i64) -> Maybe<i64> => match two(n) {
  Ok(t) => Some(t.0.byte_len()),
  Err(e) => if e.byte_len() > 0 { None } else { Some(0) },
}`},
		{"the `?`'s own operand a temporary", `
let run = (n: i64) -> Maybe<i64> => {
  let t = ("a" ++ b, one(n + ("x" ++ b).byte_len() - 3)?)
  Some(t.0.byte_len())
}`},

		// Not a `?`.
		{"a `return` in an operand", `
let run = (n: i64) -> Maybe<i64> => {
  let t = ("a" ++ b, if n > 1 { return None } else { "z" ++ b })
  Some(t.0.byte_len())
}`},
		{"a `return` in an array's element", `
let run = (n: i64) -> Maybe<i64> => {
  let t = ["a" ++ b, if n > 1 { return None } else { "z" ++ b }]
  Some(t.len())
}`},
		{"a `return` in an argument", `
let run = (n: i64) -> Maybe<i64> => Some(glue("a" ++ b, if n > 1 { return None } else { "z" ++ b }).byte_len())`},
		{"a `break` in an operand", `
let run = (n: i64) -> Maybe<i64> => {
  var seen = 0
  for i in 0..<3 {
    let t = ("a" ++ b, if n > 1 { break } else { "z" ++ b })
    seen = seen + t.0.byte_len()
  }
  if n > 1 { None } else { Some(seen) }
}`},
		{"a `continue` in an array's element", `
let run = (n: i64) -> Maybe<i64> => {
  var seen = 0
  for i in 0..<3 {
    let t = ["a" ++ b, if n > 1 { continue } else { "z" ++ b }]
    seen = seen + t.len()
  }
  if n > 1 { None } else { Some(seen) }
}`},
		{"a `break` in an argument", `
let run = (n: i64) -> Maybe<i64> => {
  var seen = 0
  for i in 0..<3 {
    seen = seen + glue("a" ++ b, if n > 1 { break } else { "z" ++ b }).byte_len()
  }
  if n > 1 { None } else { Some(seen) }
}`},
		{"a loop inside the operand keeps its `break`", `
let run = (n: i64) -> Maybe<i64> => {
  let t = ("a" ++ b, {
    var last = "x" ++ b
    for i in 0..<3 {
      if i == 1 { break }
      last = "y" ++ b
    }
    last
  }, one(n)?)
  Some(t.0.byte_len() + t.1.byte_len())
}`},

		// What holding must leave alone.
		{"a generic body, at a managed type", `
let pair<t> = (x: t, m: Maybe<t>) -> Maybe<(t, t)> => Some((x, m?))
let run = (n: i64) -> Maybe<i64> => {
  let t = pair("a" ++ b, one(n))?
  let u = pair(n, num(n))?
  Some(t.0.byte_len() + u.0)
}`},
		{"a default lambda, in a literal that leaves and one that doesn't", `
struct Cb { f: (i64) -> i64 = (x) => x + 1, n: i64 }
let run = (n: i64) -> Maybe<i64> => {
  let plain = Cb { n: 1 }
  let t = Cb { n: num(n)? }
  Some(plain.f(t.n))
}`},
		{"the same default inside the literal that leaves", `
struct Cb { f: (i64) -> i64 = (x) => x + 1, n: i64 }
struct Two2 { a: Cb, n: i64 }
let run = (n: i64) -> Maybe<i64> => {
  let t = Two2 { a: Cb { n: 1 }, n: num(n)? }
  Some(t.a.f(t.n))
}`},
		{"a reused box", `
data L = Cons(string, shared L) | End
let keep = (l: shared L) -> shared L => l
let swap = (l: own shared L, n: i64) -> shared L => match l {
  Cons(h, t) => Cons("a" ++ h, {
    if n > 1 { return End }
    keep(t)
  }),
  End => End,
}
let run = (n: i64) -> Maybe<i64> => {
  let l: shared L = Cons("x" ++ b, End)
  let m = swap(l, n)
  let len = match m { Cons(h, _) => h.byte_len(), End => 0 }
  if len > 0 { Some(len) } else { None }
}`},
		{"an inlined sequence function, an argument owned", `
let upto = gen (s: own string, m: i64) -> Seq<i64> => {
  for i in 0..<m { yield i + s.byte_len() }
}
let run = (n: i64) -> Maybe<i64> => {
  var seen = 0
  for i in upto("a" ++ b, 2) {
    let t = ("x" ++ b, one(n + i - i)?)
    seen = seen + t.0.byte_len()
  }
  Some(seen)
}`},
		{"a consumer's `return`, an inlined callee's argument held nowhere", `
let upto = gen (s: own string, m: i64) -> Seq<i64> => {
  for i in 0..<m { yield i + s.byte_len() }
}
let run = (n: i64) -> Maybe<i64> => {
  for i in upto("a" ++ b, 2) {
    if n > 1 { return None }
  }
  Some(0)
}`},
		{"a sequence value beside the `?`", `
let upto = gen (m: i64) -> Seq<i64> => {
  for i in 0..<m { yield i }
}
let run = (n: i64) -> Maybe<i64> => {
  let t = (upto(3), one(n)?)
  Some(t.1.byte_len())
}`},
		{"an operand in a generator's own body", `
let words = gen (n: i64) -> Seq<i64> => {
  let t = ("a" ++ b, if n > 1 { return } else { "z" ++ b })
  yield t.0.byte_len()
}
let run = (n: i64) -> Maybe<i64> => {
  var seen = 0
  for w in words(n) { seen = seen + w }
  if n > 1 { None } else { Some(seen) }
}`},

		// Beside the operands, not among them.
		{"a concatenation", `
let run = (n: i64) -> Maybe<i64> => Some((("a" ++ b) ++ one(n)?).byte_len())`},
		{"an interpolation", `
let run = (n: i64) -> Maybe<i64> => Some("${"a" ++ b}-${one(n)?}".byte_len())`},
		{"an assignment's index", `
let run = (n: i64) -> Maybe<i64> => {
  var xs = ["p" ++ b, "q" ++ b]
  xs[num(n)?] = "a" ++ b
  Some(xs.len())
}`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			got := buildAndRunASanStdout(t, lib+c.src+`
let main = () -> void => {
  print("${(run(0) ?? -1) >= 0} ${(run(1) ?? -1) >= 0} ${run(5) ?? -1}")
}`)
			if want := "true true -1"; strings.TrimSpace(got) != want {
				t.Errorf("got %q, want %q", got, want)
			}
		})
	}
}
