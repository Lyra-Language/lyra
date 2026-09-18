package llvm

import "testing"

// Two modules may each declare a private type of the same name, and the backend must
// emit **two** LLVM struct types for them.
//
// The registry of emitted struct types was keyed by bare name, which is the backend half
// of the program-wide type namespace (todo.md, Modules). Keyed that way the second
// `Point` finds the first one's entry and is lowered against the wrong layout — and the
// shapes here are deliberately different (one i64 field vs. two, reading the *second*
// field of the second) so a collision cannot accidentally produce the right answer.
func TestExec_PrivateTypesInTwoModulesGetDistinctLayouts(t *testing.T) {
	got := buildAndRunModules(t, map[string]string{
		"one.lyra": `module one
struct Point { x: i64 }
pub let getOne = () -> i64 => {
  let p = Point { x: 3 }
  p.x
}`,
		"two.lyra": `module two
struct Point { y: i64, z: i64 }
pub let getTwo = () -> i64 => {
  let p = Point { y: 4, z: 5 }
  p.z
}`,
		"app.lyra": `import one
import two
let main = () -> u8 => u8(one.getOne() + two.getTwo())`,
	})
	if got != 8 {
		t.Errorf("expected 3 + 5 = 8, got %d", got)
	}
}

// A *generic* private type in two modules instantiates to two layouts, not one.
//
// An instantiation is registered under its own mangled symbol (`Box$i64`), which is
// derived from the bare declared name — so without qualifying it by the declaring module
// the two modules' `Box<i64>` collide exactly as the non-generic case did, and this is
// the path that would still have collided after the plain-declaration fix.
func TestExec_PrivateGenericTypesInTwoModulesGetDistinctLayouts(t *testing.T) {
	got := buildAndRunModules(t, map[string]string{
		"one.lyra": `module one
struct Box<t> { v: t }
pub let getOne = () -> i64 => {
  let b = Box { v: 6 }
  b.v
}`,
		"two.lyra": `module two
struct Box<t> { first: t, second: t }
pub let getTwo = () -> i64 => {
  let b = Box { first: 1, second: 9 }
  b.second
}`,
		"app.lyra": `import one
import two
let main = () -> u8 => u8(one.getOne() + two.getTwo())`,
	})
	if got != 15 {
		t.Errorf("expected 6 + 9 = 15, got %d", got)
	}
}

// A trait impl on a private type lowers against that module's declaration. The trait
// method's body is lowered through defineFunctionInto, which is where the asking module
// is entered — putting it on declareFunction instead left this path resolving names
// against whichever module was lowered last.
func TestExec_TraitMethodOnPrivateTypeResolvesItsOwnModule(t *testing.T) {
	got := buildAndRunModules(t, map[string]string{
		"one.lyra": `module one
struct Point { x: i64 }
trait Size { measure: (Self) -> i64 }
impl Size for Point { measure = (self) => self.x }
pub let getOne = () -> i64 => {
  let p = Point { x: 4 }
  p.measure()
}`,
		"two.lyra": `module two
struct Point { a: i64, b: i64 }
pub let getTwo = () -> i64 => {
  let p = Point { a: 1, b: 6 }
  p.b
}`,
		"app.lyra": `import one
import two
let main = () -> u8 => u8(one.getOne() + two.getTwo())`,
	})
	if got != 10 {
		t.Errorf("expected 4 + 6 = 10, got %d", got)
	}
}

// The same for a private `data` type: two modules' same-named sum types must not share
// a tag numbering or a payload layout.
func TestExec_PrivateDataTypesInTwoModulesGetDistinctLayouts(t *testing.T) {
	got := buildAndRunModules(t, map[string]string{
		"one.lyra": `module one
data Shape = Circle(i64)
pub let getOne = () -> i64 => match Circle(7) {
  Circle(r) => r,
}`,
		"two.lyra": `module two
data Shape = Square | Rect(i64, i64)
pub let getTwo = () -> i64 => match Rect(2, 5) {
  Square => 0,
  Rect(w, h) => w * h,
}`,
		"app.lyra": `import one
import two
let main = () -> u8 => u8(one.getOne() + two.getTwo())`,
	})
	if got != 17 {
		t.Errorf("expected 7 + 10 = 17, got %d", got)
	}
}

// **A name declared in two modules keeps its identity inside an array inside a generic.**
//
// `Maybe<[]Entry>` crashed the backend (09/18) whenever another module also declared an
// `Entry` — `examples/calendar` named its events `Entry`, and `std.collections` has one for
// its HashMap. A name declared twice must carry its declaration key to be spelled
// (types.IdentityString), and the same `Entry` reached instantiationSymbol as a keyed
// `NamedStructType` from one path and a bare `UnresolvedType` from another. At the top level
// of an argument that did not matter: resolveForLayout resolves the name. Inside a `[]T` it
// did — layout never looks inside a pointer, which is what keeps recursive types finite —
// so two spellings named two instantiations, and llir panicked storing one into the other.
//
// The generic is the test's own `Opt<t>`, because this harness has no prelude and because
// the bug is any generic `data` type's, not `Maybe`'s. Every composite a type argument is
// spelled from is here, since the missing case was one of them: a dynamic array, a fixed
// array, a tuple holding one, a nested generic, and a type recursive through `[]`, which
// must still terminate.
func TestExec_SameNamedTypeInsideAnArrayInsideAGeneric(t *testing.T) {
	other := `module other
pub struct Entry { k: i64 }
pub let pair = () -> Entry => Entry { k: 1 }`
	for _, c := range []struct {
		name, body string
		want       int
	}{
		{"a dynamic array", `
let make = () -> Opt<[]Entry> => Yep([Entry { n: 7, kids: [] }])
let main = () -> u8 => match make() { Yep(xs) => u8(xs[0].n), Nope => 1 }`, 7},
		{"an annotated local", `
let main = () -> u8 => { let r: Opt<[]Entry> = Nope
  match r { Yep(_) => 1, Nope => 5 } }`, 5},
		{"a fixed array", `
let make = () -> Opt<[2]Entry> => Yep(#[Entry { n: 1, kids: [] }, Entry { n: 4, kids: [] }])
let main = () -> u8 => match make() { Yep(xs) => u8(xs[1].n), Nope => 1 }`, 4},
		{"a tuple holding an array", `
let make = () -> Opt<(i64, []Entry)> => Yep((5, [Entry { n: 2, kids: [] }]))
let main = () -> u8 => match make() { Yep((a, xs)) => u8(a + xs[0].n), Nope => 1 }`, 7},
		{"a nested generic", `
let make = () -> Opt<[]Opt<Entry>> => Yep([Yep(Entry { n: 6, kids: [] })])
let main = () -> u8 => match make() { Yep(xs) => match xs[0] { Yep(e) => u8(e.n), Nope => 1 }, Nope => 2 }`, 6},
		{"a type recursive through an array", `
let make = () -> Opt<[]Entry> => Yep([Entry { n: 8, kids: [Entry { n: 1, kids: [] }] }])
let main = () -> u8 => match make() { Yep(xs) => u8(xs[0].n + xs[0].kids[0].n), Nope => 1 }`, 9},
	} {
		t.Run(c.name, func(t *testing.T) {
			app := "import other\ndata Opt<t> = Nope | Yep(t)\nstruct Entry { n: i64, kids: []Entry }\n" +
				c.body + "\nlet unused = () -> i64 => other.pair().k"
			if got := buildAndRunModules(t, map[string]string{"other.lyra": other, "app.lyra": app}); got != c.want {
				t.Errorf("exit %d, want %d", got, c.want)
			}
		})
	}
}
