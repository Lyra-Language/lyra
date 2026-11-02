package llvm

import (
	"regexp"
	"testing"
)

// A void value is the empty struct, everywhere but a function's result: a generic body at `e =
// void` binds one, passes it, returns it, captures it and holds it in an array and a payload, and
// each of those was invalid IR or a refusal until 11/01 ("void type only allowed for function
// results"). Under ASan: a `[]void` holds nothing but its length, so a stride of zero would have
// it read and write off its buffer.
func TestExec_AVoidValueGoesWhereAValueGoes(t *testing.T) {
	t.Parallel()
	const common = `let noop = () -> void => {}
`
	cases := []struct {
		name string
		src  string
		want int
	}{
		{"a lone payload bound, and put in an array", `data Kind<e> = Pressable(e) | Plain
let events<e> = (k: Kind<e>) -> []e => match k { Pressable(ev) => [ev], Plain => [] }
let main = () -> u8 => {
  let a: Kind<void> = Pressable()
  let b: Kind<void> = Pressable(noop())
  let c: Kind<void> = Plain
  u8(events(a).len() + events(b).len() + events(c).len())
}`, 2},
		{"a lone payload bound without parentheses", `data Kind<e> = Pressable(e) | Plain
let events<e> = (k: Kind<e>) -> []e => match k { Pressable ev => [ev], Plain => [] }
let main = () -> u8 => {
  let a: Kind<void> = Pressable()
  u8(events(a).len())
}`, 1},
		{"a lone payload nested in a pattern", `data Kind<e> = Pressable(e) | Plain
let first<e> = (p: (Kind<e>, i64)) -> []e => match p { (Pressable(ev), _) => [ev], _ => [] }
let main = () -> u8 => {
  let a: Kind<void> = Pressable()
  u8(first((a, 1)).len())
}`, 1},
		{"a payload beside another field", `data Kind<e> = Checkable(bool, e) | Plain
let events<e> = (k: Kind<e>) -> []e => match k { Checkable(_, ev) => [ev], Plain => [] }
let main = () -> u8 => {
  let a: Kind<void> = Checkable(true, noop())
  u8(events(a).len())
}`, 1},
		{"a binding of a void call", `let keep<e> = (f: () -> e) -> i64 => {
  let v = f()
  let xs = [v, v, v]
  xs.len()
}
let main = () -> u8 => u8(keep(noop))`, 3},
		{"returned, its effects kept", `var calls: i64 = 0
let tick = () -> void => { calls += 1 }
let same<e> = (x: e) -> e => x
let main = () -> u8 => {
  same(tick())
  same(tick())
  u8(calls)
}`, 2},
		{"a parameter handed on", `let wrap<e> = (x: e) -> []e => [x]
let hand<e> = (x: e) -> []e => wrap(x)
let main = () -> u8 => u8(hand(noop()).len())`, 1},
		{"a generic struct's field", `struct Holder<e> { ev: e, n: i64 }
let unwrap<e> = (h: Holder<e>) -> []e => [h.ev]
let main = () -> u8 => {
  let h = Holder { ev: noop(), n: 4 }
  u8(unwrap(h).len() + h.n)
}`, 5},
		{"iterated and passed to a function value", `let each<e> = (xs: []e, f: (e) -> i64) -> i64 => {
  var t = 0
  for x in xs { t += f(x) }
  t
}
let one = (x: void) -> i64 => 1
let wrap<e> = (x: e) -> []e => [x, x]
let main = () -> u8 => u8(each(wrap(noop()), one))`, 2},
		{"pushed", `let gather<e> = (x: e) -> []e => {
  var xs: []e = []
  xs.push(x)
  xs.push(x)
  xs
}
let main = () -> u8 => u8(gather(noop()).len())`, 2},
		{"captured and returned through a closure", `let later<e> = (x: e) -> () -> e => () -> e => x
let main = () -> u8 => {
  let f = later(noop())
  f()
  6
}`, 6},
		{"a Maybe's payload", `let opt<e> = (x: e) -> Maybe<e> => Some(x)
let main = () -> u8 => match opt(noop()) { Some(_) => 7, None => 0 }`, 7},
		{"a Maybe's payload bound", `let opt<e> = (x: e) -> Maybe<e> => Some(x)
let again<e> = (m: Maybe<e>) -> []e => match m { Some(v) => [v, v], None => [] }
let main = () -> u8 => u8(again(opt(noop())).len())`, 2},
		{"if and match joined on it", `let pick<e> = (c: bool, a: e, b: e) -> []e => {
  let v = if c { a } else { b }
  let w = match c { true => a, false => b }
  [v, w]
}
let main = () -> u8 => u8(pick(true, noop(), noop()).len())`, 2},
		{"a function through an alias of void", `alias Nothing = void
let quiet = () -> Nothing => {}
let same<e> = (x: e) -> e => x
let main = () -> u8 => {
  same(quiet())
  9
}`, 9},
		{"an if and a match used as statements, one arm void and one a value", `var n: i64 = 0
let add = (by: i64) -> void => { n += by }
let main = () -> u8 => {
  if n == 0 { add(1) } else { n + 1 }
  if n == 9 { n + 1 } else { add(2) }
  match n { 3 => add(4), _ => n * 2 }
  match n { 3 => n * 2, _ => add(8) }
  u8(n)
}`, 15},
		{"a plain void function, for its effects", `var n: i64 = 0
let add = (by: i64) -> void => { n += by }
let main = () -> u8 => {
  add(2)
  if n > 1 { add(3) } else { add(4) }
  match n { 5 => add(1), _ => add(0) }
  u8(n)
}`, 6},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := buildAndRunASanWithPrelude(t, common+tc.src); got != tc.want {
				t.Errorf("exit %d, want %d", got, tc.want)
			}
		})
	}
}

// Control flow that merges a value taking no space builds no phi: the merge has one possible
// value, and at -O0 clang 15's AArch64 IRTranslator crashes on a phi with no registers (11/02:
// every `if` over two void calls, once a void call's value was `{}`). Whether it faults varies
// with the program, and Apple's clang takes the phi, so the test reads the IR rather than
// trusting a build to crash.
func TestEmit_ZeroSizedMergeBuildsNoPhi(t *testing.T) {
	t.Parallel()
	const common = `let noop = () -> void => {}
struct Marker {}
struct Holder<e> { ev: e }
let same<e> = (x: e) -> e => x
`
	zeroSizedPhi := regexp.MustCompile(`= phi (\{[ {},]*\}|%"?(Marker|Holder))`)
	cases := []struct {
		name string
		src  string
		want int
	}{
		{"an if over two void calls", `let main = () -> u8 => {
  var b = true
  if b { noop() } else { noop() }
  b = false
  1
}`, 1},
		{"a match over two void calls", `let main = () -> u8 => {
  let n = 3
  match n { 3 => noop(), _ => noop() }
  2
}`, 2},
		{"one arm diverging", `let f = (c: bool) -> u8 => {
  if c { return 3 } else { noop() }
  4
}
let main = () -> u8 => f(true)`, 3},
		{"a generic body's if and match at void", `let pick<e> = (c: bool, a: e, b: e) -> []e => {
  let v = if c { a } else { b }
  let w = match c { true => a, false => b }
  [v, w]
}
let main = () -> u8 => u8(pick(true, noop(), noop()).len())`, 2},
		{"?? over a Maybe's void payload", `let opt<e> = (x: e, c: bool) -> Maybe<e> => if c { Some(x) } else { None }
let or<e> = (m: Maybe<e>, d: e) -> []e => [m ?? d]
let main = () -> u8 => u8(or(opt(noop(), true), noop()).len() + 4)`, 5},
		{"?? with a void call for its default", `let main = () -> u8 => {
  let m: Maybe<void> = None
  let v = m ?? noop()
  u8([v, v].len() + 3)
}`, 5},
		{"? over a Maybe's void payload", `let opt<e> = (x: e) -> Maybe<e> => Some(x)
let up<e> = (x: e) -> Maybe<[]e> => {
  let v = opt(x)?
  Some([v, v])
}
let main = () -> u8 => match up(noop()) { Some(xs) => u8(xs.len()), None => 0 }`, 2},
		{"? over a Result's void payload", `let res<e> = (x: e) -> Result<e, string> => Ok(x)
let up<e> = (x: e) -> Result<[]e, string> => {
  let v = res(x)?
  Ok([v])
}
let main = () -> u8 => match up(noop()) { Ok(xs) => u8(xs.len()), Err(_) => 0 }`, 1},
		{"an empty struct", `let same_marker = (c: bool) -> []Marker => {
  let m = if c { Marker {} } else { Marker {} }
  [m, m, m]
}
let main = () -> u8 => u8(same_marker(true).len() + 3)`, 6},
		{"a struct of nothing but void", `let hold<e> = (c: bool, x: e) -> []Holder<e> => {
  let h = if c { Holder { ev: x } } else { Holder { ev: x } }
  [h]
}
let main = () -> u8 => u8(hold(true, noop()).len() + 6)`, 7},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if phi := zeroSizedPhi.FindString(emitWithPrelude(t, common+tc.src)); phi != "" {
				t.Errorf("the IR holds a phi over nothing: %q", phi)
			}
			if got := buildAndRunASanWithPrelude(t, common+tc.src); got != tc.want {
				t.Errorf("exit %d, want %d", got, tc.want)
			}
		})
	}
}
