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
