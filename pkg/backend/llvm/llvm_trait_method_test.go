package llvm

import (
	"strings"
	"testing"
)

// Trait-impl methods lower to ordinary functions taking the receiver first, and a
// method call lowers to a direct call to one. Dispatch is entirely static — the
// typechecker already chose the impl — so there are no vtables and nothing is resolved
// at run time.
//
// Before this, a trait impl type-checked and then failed the build with "unsupported
// method call", which is why the standard library's combinators had to be written as
// free functions.
func TestExec_TraitMethods(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		src  string
		want int
	}{
		{
			"method on a data value",
			`data Maybe = None | Some(i64)
			 trait Unwrap { unwrapOr: (Self, i64) -> i64 }
			 impl Unwrap for Maybe {
			   unwrapOr = (self, fallback) => match self { Some(v) => v, None => fallback }
			 }
			 let main = () -> u8 => {
			   let m = Some(40)
			   let n = None
			   u8(m.unwrapOr(0) + n.unwrapOr(2))
			 }`,
			42,
		},
		{
			// The receiver is just the first parameter, and an argument follows it.
			"method on a struct, with an argument",
			`struct Counter { n: i64 }
			 trait Bump { bumped: (Self, i64) -> i64 }
			 impl Bump for Counter { bumped = (self, by) => self.n + by }
			 let main = () -> u8 => {
			   let c = Counter { n: 4 }
			   u8(c.bumped(5))
			 }`,
			9,
		},
		{
			// One method calling another queues a second emission while the first is
			// still being lowered — which is why bodies are deferred rather than
			// lowered re-entrantly.
			"a method calling another method",
			`struct Box { v: i64 }
			 trait Twice { one: (Self) -> i64, two: (Self) -> i64 }
			 impl Twice for Box {
			   one = (self) => self.v,
			   two = (self) => self.one() + self.one(),
			 }
			 let main = () -> u8 => {
			   let b = Box { v: 6 }
			   u8(b.two())
			 }`,
			12,
		},
		{
			// Two traits implemented for one type: the emitted symbol carries the
			// trait as well as the type, or these two would collide.
			"two traits on one type",
			`struct Box { v: i64 }
			 trait A { asize: (Self) -> i64 }
			 trait B { bsize: (Self) -> i64 }
			 impl A for Box { asize = (self) => 3 }
			 impl B for Box { bsize = (self) => 4 }
			 let main = () -> u8 => {
			   let b = Box { v: 1 }
			   u8(b.asize() + b.bsize())
			 }`,
			7,
		},
		{
			"a method returning a managed value",
			`struct Tag { n: i64 }
			 trait Show { show: (Self) -> string }
			 impl Show for Tag { show = (self) => "hi" ++ "!" }
			 let main = () -> u8 => {
			   let t = Tag { n: 1 }
			   if t.show() == "hi!" { 3 } else { 1 }
			 }`,
			3,
		},
		{
			"a receiver that owns a managed value",
			`struct Holder { s: string }
			 trait Len { plen: (Self) -> i64 }
			 impl Len for Holder { plen = (self) => if self.s == "ab" { 2 } else { 0 } }
			 let main = () -> u8 => {
			   let h = Holder { s: "a" ++ "b" }
			   u8(h.plen())
			 }`,
			2,
		},
		{
			// A generic impl needs no extra machinery: methods are emitted at the
			// *call site*, where dispatch has already substituted Self with the
			// concrete receiver type.
			"generic impl",
			`struct Box<t> { value: t }
			 trait Get { get: (Self) -> i64 }
			 impl Get<t> for Box<t> { get = (self) => 5 }
			 let main = () -> u8 => {
			   let b = Box { value: 1 }
			   u8(b.get())
			 }`,
			5,
		},
		{
			// `Self` nested in a signature is the receiver's type at every depth: an array,
			// a tuple, a data payload, a callback, a generic impl, a default method. The
			// typechecker used to replace only a top-level `Self`, so none of these built.
			"Self nested in the signature",
			`data Opt<t> = Nothing | Just(t)
			 struct Box<t> { value: t }
			 trait Dup { dup: (Self) -> []Self }
			 impl Dup for i64 { dup = (self) => [self, self + 1] }
			 impl Dup for Box<t> { dup = (self) => [self, self] }
			 trait Two { two: (Self) -> (Self, Self) }
			 impl Two for i64 { two = (self) => (self, self * 2) }
			 trait Dec { dec: (Self) -> Opt<Self> }
			 impl Dec for i64 { dec = (self) => if self > 0 { Just(self - 1) } else { Nothing } }
			 trait Apply { apply: (Self, (Self) -> Self) -> Self }
			 impl Apply for i64 { apply = (self, f) => f(self) }
			 trait Wrap {
			   one: (Self) -> Self
			   wrap: (Self) -> []Self = (self) => [self.one()]
			 }
			 impl Wrap for i64 { one = (self) => self * 3 }
			 let main = () -> u8 => {
			   let d = 4.dup()
			   let bs = Box { value: 7 }.dup()
			   let p = 5.two()
			   let n = match 6.dec() { Just(v) => v, Nothing => 0 }
			   let z = match 0.dec() { Just(v) => v, Nothing => 100 }
			   let a = 2.apply((x: i64) -> i64 => x + 1)
			   let w = 2.wrap()
			   u8(d[0] + d[1] + bs[1].value + p.0 + p.1 + n + z + a + w[0])
			 }`,
			// 4+5 + 7 + 5+10 + 5 + 100 + 3 + 6
			145,
		},
		{
			// An unannotated lambda argument takes its types from the method's slot, and the
			// planted types are what the backend lowers its parameters at — including `t`
			// planted at a bound receiver inside a generic function, lowered per specialization.
			"untyped lambda arguments",
			`struct Box<t> { v: t }
			 trait Apply { apply: (Self, (i64) -> i64) -> i64 }
			 impl Apply for i64 { apply = (self, f) => f(self) }
			 trait Ap { ap: (Self, (Self) -> Self) -> Self }
			 impl Ap for i64 { ap = (self, f) => f(self) }
			 trait Get<e> { get: (Self, (e) -> e) -> e }
			 impl Get<t> for Box<t> { get = (self, f) => f(self.v) }
			 struct S { f: (i64, (i64) -> i64) -> i64 }
			 let twice<t> where t: Ap = (v: t) -> t => v.ap((x) => x.ap((y) => y))
			 let main = () -> u8 => {
			   let s = S { f: (n: i64, h: (i64) -> i64) -> i64 => h(n) }
			   let a = 3.apply((x) => x * 7)
			   let b = 4.ap((x) => x + 1)
			   let c = Box { v: 10 }.get((x) => x * 2)
			   let d = s.f(6, (x) => x - 1)
			   let e = twice(9)
			   u8(a + b + c + d + e)
			 }`,
			// 21 + 5 + 20 + 5 + 9
			60,
		},
		{
			// A method's own type variable is solved per call and the impl body is emitted
			// once per solution: `mapv` at `b = i64`, at `b = bool` and at `b = []i64` (a
			// managed result), `pair` on a generic impl at two `b`s. `Pair`'s method variable
			// is named `t` like the impl's; conflated, the second `pair` stored a `Box<i64>`
			// into a `Box<string>` slot and llir panicked.
			"a method's own type variables",
			`struct Box<t> { v: t }
			 trait Mapper { mapv: (Self, (i64) -> b) -> b }
			 impl Mapper for i64 { mapv = (self, f) => f(self) }
			 trait Pair { pair: (Self, t) -> (Self, t) }
			 impl Pair for Box<t> { pair = (self, x) => (self, x) }
			 let main = () -> u8 => {
			   let a = 3.mapv((x) => x * 2)
			   let b = if 3.mapv((x) => x > 2) { 10 } else { 0 }
			   let xs = 4.mapv((x) => [x, x, x])
			   let p = Box { v: 7 }.pair(true)
			   let q = Box { v: true }.pair(20)
			   let c = if p.1 { p.0.v } else { 0 }
			   let d = if q.0.v { q.1 } else { 0 }
			   u8(a + b + xs.len() + c + d)
			 }`,
			// 6 + 10 + 3 + 7 + 20
			46,
		},
		{
			// `Self<b>` is the impl target's head at `b`: `map` emits once per (a, b), on a
			// struct and on a data type, chained, and `swap` exchanges two arguments. `Box<a>`
			// names its variable like `map`'s own `a`, which must stay two variables.
			"Self applied to type arguments",
			`struct Box<a> { v: a }
			 data Opt<t> = No | Yes(t)
			 struct Pair<k, v> { a: k, b: v }
			 trait Functor { map: (Self<a>, (a) -> b) -> Self<b> }
			 impl Functor for Box<a> { map = (self, f) => Box { v: f(self.v) } }
			 impl Functor for Opt<t> { map = (self, f) => match self { Yes(v) => Yes(f(v)), No => No } }
			 trait Swap { swap: (Self<a, b>) -> Self<b, a> }
			 impl Swap for Pair<k, v> { swap = (self) => Pair { a: self.b, b: self.a } }
			 let main = () -> u8 => {
			   let b = Box { v: 3 }.map((x) => x > 2).map((y) => if y { 30 } else { 0 })
			   let o = match Yes(4).map((x) => [x, x]) { Yes(xs) => xs.len(), No => 0 }
			   let p = Pair { a: true, b: 5 }.swap()
			   let q = if p.b { p.a } else { 0 }
			   u8(b.v + o + q)
			 }`,
			// 30 + 2 + 5
			37,
		},
		{
			// Through a `where` bound the solution is in the enclosing body's terms and is
			// composed per specialization: `viaB` emits `mapv` at `u = bool`, `u = []i64`
			// and `u = string` (both managed, so the ownership tables must exist — ASan
			// sees a missing one), `idm` at `b = t` for two `t`s, `go`'s receiver is named
			// like the method's `b`, `mk` reaches a generic impl, and `outer` composes
			// through a second generic. Refused by name until 09/14/26.
			"a method's own type variables through a where bound",
			`struct Box<t> { v: t }
			 trait Mapper { mapv: (Self, (i64) -> b) -> b }
			 impl Mapper for i64 { mapv = (self, f) => f(self) }
			 trait M2 { m2: (Self, (Self) -> b) -> b }
			 impl M2 for i64 { m2 = (self, f) => f(self) }
			 impl M2 for string { m2 = (self, f) => f(self) }
			 trait Pair { pair: (Self, t) -> (Self, t) }
			 impl Pair for Box<t> { pair = (self, x) => (self, x) }
			 trait Loud: Mapper { loud: (Self) -> []i64 = (self) => self.mapv((x: i64) -> []i64 => [x]) }
			 impl Loud for i64
			 let viaB<t, u> where t: Mapper = (v: t, f: (i64) -> u) -> u => v.mapv(f)
			 let idm<t> where t: M2 = (v: t) -> t => v.m2((x) => x)
			 let go<b> where b: Mapper = (v: b) -> i64 => v.mapv((x) => x + 1)
			 let mk<s, u> where s: Pair = (v: s, x: u) -> u => v.pair(x).1
			 let outer<w> = (f: (i64) -> w) -> w => viaB(9, f)
			 let main = () -> u8 => {
			   let a = if viaB(3, (x: i64) -> bool => x > 2) { 1 } else { 0 }
			   let xs = viaB(4, (x: i64) -> []i64 => [x, x])
			   let s = viaB(5, (x: i64) -> string => "abc")
			   let n = idm(6)
			   let t = idm("hello")
			   let g = go(9)
			   let m = mk(Box { v: true }, 20)
			   let o = outer((x: i64) -> string => "xy")
			   u8(a + xs.len() + s.len() + n + t.len() + g + m + o.len() + 7.loud().len())
			 }`,
			// 1 + 2 + 3 + 6 + 5 + 10 + 20 + 2 + 1
			50,
		},
		{
			// The ownership half. `twice` copies a runtime string (and an array of them) into
			// an array, which needs a retain per copy; the specializations `twice<b=string>`
			// and `twice<b=[]string>` exist only once `viaT`'s bindings are composed into the
			// bound call's solution. Without that composition in the driver the backend asked
			// for tables nobody built, lowered with no retains, and ASan saw the double free.
			"a where-bound method's specialization has its ownership table",
			`trait Twice { twice: (Self, b) -> []b }
			 impl Twice for i64 { twice = (self, x) => [x, x] }
			 let viaT<t, u> where t: Twice = (v: t, x: u) -> []u => v.twice(x)
			 let main = () -> u8 => {
			   let s = "ab".slice(0, 1) ++ "cd".slice(1, 2)
			   let xs = viaT(3, s)
			   let ys = viaT(4, [s, s])
			   u8(xs.len() + xs[0].len() + xs[1].len() + ys[1].len())
			 }`,
			8,
		},
	}
	clang := lookClang(t)
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if got := buildAndRun(t, c.src); got != c.want {
				t.Errorf("exited %d; want %d", got, c.want)
			}
			if got := buildAndRunASan(t, clang, c.src); got != c.want {
				t.Errorf("under ASan: exited %d; want %d", got, c.want)
			}
		})
	}
}

// A method that is never called is never emitted, the same property an uninstantiated
// generic has — and the reason emission is driven by the call site rather than by
// walking the impls.
func TestEmit_UncalledTraitMethodIsNotEmitted(t *testing.T) {
	t.Parallel()
	src := `struct Box { v: i64 }
	 trait Unused { neverCalled: (Self) -> i64 }
	 impl Unused for Box { neverCalled = (self) => 1 }
	 let main = () -> u8 => 0`
	ir, err := emitSource(t, src)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(ir, "neverCalled") {
		t.Errorf("an uncalled trait method reached the module:\n%s", ir)
	}
}

// The emitted symbol names the type, the trait and the method. Neither pair alone is
// unique: one type may implement two traits declaring the same method name, and one
// trait may be implemented by many types.
func TestEmit_TraitMethodSymbolIsQualified(t *testing.T) {
	t.Parallel()
	src := `struct Box { v: i64 }
	 trait Show { show: (Self) -> i64 }
	 impl Show for Box { show = (self) => self.v }
	 let main = () -> u8 => {
	   let b = Box { v: 1 }
	   u8(b.show())
	 }`
	ir, err := emitSource(t, src)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Box", "Show", "show"} {
		if !strings.Contains(ir, want) {
			t.Errorf("expected the emitted symbol to mention %q:\n%s", want, ir)
		}
	}
}

// A trait method's body is narrowed by its declared return type, exactly as a free
// function's is. The check that does this was written four times and the trait-impl
// copy had drifted: it ran neither contextualType nor propagateExpectedType, so the
// body computed at the i64 default and was truncated at the return boundary.
//
// That was a *semantic* difference, not only a width one, because Lyra's arithmetic
// is checked: `200 + 100` returning `u8` traps in a free function and, before 08/05,
// silently produced 44 in the identical trait method. The exec pair below is the
// assertion — same expression, same declared return, one answer.
func TestExec_TraitMethodNarrowsToItsDeclaredReturn(t *testing.T) {
	t.Parallel()
	// Runtime operands, not `200 + 100`: a *constant* overflowing expression in
	// return position is a compile error now (08/13, alongside the pattern-literal
	// family — decl sites always refused it, and returns joined them), so the
	// trap-parity this test exists to pin needs values the fold cannot see.
	const traitSrc = `struct Pt { x: u8 }
	 trait Small { get: (Self) -> u8 }
	 impl Small for Pt { get = (self) => self.x + 100 }
	 let main = () -> u8 => {
	   let p = Pt { x: 200 }
	   p.get()
	 }`
	const freeSrc = `let get = (x: u8) -> u8 => x + 100
	 let main = () -> u8 => get(200)`

	traitStderr, traitCode := buildAndRunPanic(t, traitSrc)
	freeStderr, freeCode := buildAndRunPanic(t, freeSrc)
	if traitCode != freeCode {
		t.Errorf("trait method exited %d but the identical free function exited %d "+
			"(trait stderr %q, free stderr %q)", traitCode, freeCode, traitStderr, freeStderr)
	}
	if traitCode != trapExitCode {
		t.Errorf("u8 arithmetic overflowing should trap: exited %d, want %d (stderr %q)",
			traitCode, trapExitCode, traitStderr)
	}
}

// The narrowing itself, not only its overflow consequence: the body's literals lower
// at the declared u8 rather than the i64 default.
func TestEmit_TraitMethodBodyUsesTheDeclaredWidth(t *testing.T) {
	t.Parallel()
	src := `struct Pt { x: i64 }
	 trait Small { get: (Self) -> u8 }
	 impl Small for Pt { get = (self) => 5 + 3 }
	 let main = () -> u8 => {
	   let p = Pt { x: 1 }
	   p.get()
	 }`
	ir, err := emitSource(t, src)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(ir, "i64 5") || strings.Contains(ir, "i64 3") {
		t.Errorf("the body should compute at the declared u8, not the i64 default:\n%s", ir)
	}
}
