package llvm

import "testing"

// **`Type.name(…)` calls a member of `impl Type` with no `self`** (11/03). The call is rewritten
// to the direct call it is, as a method call is, so these exercise what that rewrite has to
// carry: the declaration it resolved to, through an alias or a second module's namesake, and a
// function value where there is no call to rewrite.
func TestExec_AssociatedFunctions(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		src  string
		want int
	}{
		{
			"a constructor on a struct",
			`struct Pt { x: i64, y: i64 }
impl Pt {
  let at = pure (x: i64, y: i64) -> Pt => Pt { x: x, y: y }
  let sum = pure (self) -> i64 => self.x + self.y
}
let main = () -> u8 => u8(Pt.at(3, 4).sum())`,
			7,
		},
		{
			"two types may each declare a name",
			`struct Aa { v: i64 }
struct Bb { v: i64 }
impl Aa { let make = pure () -> Aa => Aa { v: 1 } }
impl Bb { let make = pure () -> Bb => Bb { v: 2 } }
let make = pure () -> i64 => 40
let main = () -> u8 => u8(Aa.make().v + Bb.make().v * 10 + make())`,
			61,
		},
		{
			"one calls another, unqualified nowhere",
			`struct Pt { x: i64 }
impl Pt {
  let origin = pure () -> Pt => Pt.at(0)
  let at = pure (x: i64) -> Pt => Pt { x: x }
}
let main = () -> u8 => u8(Pt.origin().x + 9)`,
			9,
		},
		{
			"on a data type",
			`data Shape = Circle(i64) | Square(i64)
impl Shape {
  let unit = pure () -> Shape => Square(1)
  let side = pure (self) -> i64 => match self { Circle(r) => r, Square(s) => s }
}
let main = () -> u8 => u8(Shape.unit().side() + 4)`,
			5,
		},
		{
			"through an alias, written at the call or at the block",
			`struct Pt { x: i64 }
alias P2 = Pt
impl Pt { let one = pure () -> Pt => Pt { x: 1 } }
impl P2 { let two = pure () -> Pt => Pt { x: 2 } }
let main = () -> u8 => u8(P2.one().x + Pt.two().x * 10)`,
			21,
		},
		{
			"a generic target, solved from the arguments",
			`struct Wrap<t> { value: t }
impl Wrap<t> {
  let of = (value: t) -> Wrap<t> => Wrap { value: value }
  let get = (self) -> t => self.value
}
let main = () -> u8 => {
  let s = Wrap.of("abc")
  u8(Wrap.of(5).get() + s.get().len())
}`,
			8,
		},
		{
			"a generic target, solved from the context",
			`struct Wrap<t> { items: []t }
impl Wrap<t> { let empty = () -> Wrap<t> => Wrap { items: [] } }
let main = () -> u8 => {
  let w: Wrap<i64> = Wrap.empty()
  u8(w.items.len() + 2)
}`,
			2,
		},
		{
			"a turbofish binds the member's own list",
			`struct Pt { x: i64 }
impl Pt { let widen<n> = pure (v: n) -> (n, n) => (v, v) }
let main = () -> u8 => {
  let t = Pt.widen::<u8>(3)
  t.0 + t.1
}`,
			6,
		},
		{
			"a block's where reaches the member",
			`struct Wrap<t> { value: t }
impl Wrap<t> where t: Show { let label = (value: t) -> string => "${value}" }
let main = () -> u8 => u8(Wrap.label(123).len())`,
			3,
		},
		{
			"on a builtin",
			`impl i64 { let answer = pure () -> i64 => 42 }
impl string { let dash = pure () -> string => "-" }
let main = () -> u8 => u8(i64.answer() + string.dash().len())`,
			43,
		},
		{
			"a default argument",
			`struct Pt { x: i64 }
impl Pt { let at = pure (x: i64 = 6) -> Pt => Pt { x: x } }
let main = () -> u8 => u8(Pt.at().x + Pt.at(1).x)`,
			7,
		},
		{
			"an all-caps type",
			`data IO = Fd(i64)
impl IO { let stdin = pure () -> IO => Fd(9) }
let main = () -> u8 => match IO.stdin() { Fd(n) => u8(n) }`,
			9,
		},
		{
			"a constant of an all-caps name keeps its reading",
			`const IO = 3
let main = () -> u8 => u8(IO.abs())`,
			3,
		},
		{
			"a nullary constructor's value keeps its methods",
			`data Unit = Unit
impl Unit { let code = pure (self) -> i64 => 5 }
let main = () -> u8 => u8(Unit.code())`,
			5,
		},
		{
			"named as a value",
			`struct Pt { x: i64 }
impl Pt { let at = pure (x: i64) -> Pt => Pt { x: x } }
let apply = (f: (i64) -> Pt, v: i64) -> i64 => f(v).x
let main = () -> u8 => {
  let f = Pt.at
  u8(f(1).x + apply(Pt.at, 2) * 10)
}`,
			21,
		},
		{
			"a trait function at the impl the type names",
			`trait Zero { zero: () -> Self }
struct Pt { x: i64 }
impl Zero for Pt { zero = () => Pt { x: 7 } }
impl Zero for i64 { zero = () => 8 }
let main = () -> u8 => u8(Pt.zero().x + i64.zero() * 10)`,
			87,
		},
		{
			"a trait function on a generic target, from the context",
			`trait Zero { zero: () -> Self }
struct Wrap<t> { items: []t }
impl Zero for Wrap<t> { zero = () => Wrap { items: [] } }
let main = () -> u8 => {
  let w: Wrap<i64> = Wrap.zero()
  u8(w.items.len() + 4)
}`,
			4,
		},
		{
			"a trait function's argument",
			`trait Parse { parse: (i64) -> Self }
struct Pt { x: i64 }
impl Parse for Pt { parse = (v) => Pt { x: v + 1 } }
let main = () -> u8 => u8(Pt.parse(4).x)`,
			5,
		},
		{
			"an inherent function outranks a trait's",
			`trait Zero { zero: () -> Self }
struct Pt { x: i64 }
impl Zero for Pt { zero = () => Pt { x: 7 } }
impl Pt { let zero = pure () -> Pt => Pt { x: 9 } }
let main = () -> u8 => u8(Pt.zero().x)`,
			9,
		},
		{
			"called inside a closure, and from a generic",
			`struct Pt { x: i64 }
impl Pt { let at = pure (x: i64) -> Pt => Pt { x: x } }
let twice<t> = (v: t, f: (t) -> i64) -> i64 => f(v) + f(v)
let main = () -> u8 => u8(twice(2, (n) => Pt.at(n).x))`,
			4,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if got := buildAndRunASanWithPrelude(t, c.src); got != c.want {
				t.Errorf("exited %d; want %d", got, c.want)
			}
		})
	}
}

// A managed value through the rewritten call: the callee's result is owned by its caller, as any
// call's is.
func TestExec_AssociatedFunctionManagedValues(t *testing.T) {
	t.Parallel()
	src := `struct Doc { title: string, lines: []string }
impl Doc {
  let titled = (title: string) -> Doc => Doc { title: title ++ "!", lines: [title] }
  let named = (self) -> string => self.title
}
data List = Cons(string, shared List) | Nil
impl List { let of = (s: string) -> List => Cons(s ++ "?", Nil) }
let main = () -> u8 => {
  let d = Doc.titled("a" ++ "b")
  let f = Doc.titled
  let e = f("cd")
  let l = List.of("x")
  match l {
    Cons(s, _) if s == "x?" && d.named() == "ab!" && e.lines.len() == 1 => 0,
    _ => 1,
  }
}`
	if got := buildAndRunASanWithPrelude(t, src); got != 0 {
		t.Errorf("exited %d; want 0", got)
	}
}

// A generic literal infers its fields twice, and the second pass meets the call already rewritten.
// Importing only the type is the case to hold, since the function's name is in no scope.
func TestExec_AssociatedFunctionInAGenericLiteral(t *testing.T) {
	t.Parallel()
	src := `import std.collections.{ HashMap }
struct Tagged<e> { seen: HashMap<string, e>, tag: e }
let main = () -> u8 => {
  var t = Tagged { seen: HashMap.new(), tag: 7 }
  t.seen.insert("b", t.tag)
  if t.seen.get_or("b", 0) == 7 { 0 } else { 1 }
}`
	if got := buildAndRunASanWithPrelude(t, src); got != 0 {
		t.Errorf("exited %d; want 0", got)
	}
}

// Two modules each declare a `Dice` and a `Dice.make`, one name to a key that does not say whose.
// The call has to reach the module its type name says, which the bare-name path cannot tell.
func TestExec_AssociatedFunctionsAcrossModules(t *testing.T) {
	t.Parallel()
	files := map[string]string{
		"a.lyra": `module a
pub struct Dice { n: i64 }
impl Dice {
  pub let make = () -> Dice => Dice { n: Dice.seed() }
  let seed = pure () -> i64 => 1
}`,
		"b.lyra": `module b
pub struct Dice { n: i64 }
impl Dice { pub let make = () -> Dice => Dice { n: 2 } }`,
		"c.lyra": `module c
import a.{ Dice }
pub let from_c = () -> i64 => Dice.make().n * 100`,
		"app.lyra": `import a
import b
import c.{ from_c }
let main = () -> u8 => u8(a.Dice.make().n + b.Dice.make().n * 10 + from_c())`,
	}
	if got := buildAndRunModulesASan(t, files); got != 121 {
		t.Errorf("exited %d; want 121", got)
	}
}
