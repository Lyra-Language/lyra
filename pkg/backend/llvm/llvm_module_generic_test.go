package llvm

import (
	"strings"
	"testing"
)

// A generic instantiated at a type declared in a **named module** must lower.
//
// It did not: `llvm: unknown named type "Tag"` for the identity function over a struct,
// in any program with a `module` header. The specialization path was the one function
// path that never called `enterModuleOf`, so `l.currentLoc` held whatever the previous
// item left behind and the type argument was looked up under its bare name — while a
// **private** module-scoped declaration is keyed `<module>::<name>` (rule 4).
//
// A `pub` type has a bare key and worked, which is exactly what made this look like a
// bug about generics rather than about visibility, and why the tests that existed did
// not catch it: they declare their types `pub`, or in no module at all.
func TestExec_GenericOverAPrivateModuleType(t *testing.T) {
	t.Parallel()
	const src = `
module main
struct Tag { n: i64 }
let idf<t> = (a: t) -> t => a
let main = () -> void => {
  let a = idf(Tag { n: 7 });
  println("${a.n}");
}
`
	if got := strings.TrimSpace(buildAndRunWithPrelude(t, src, "")); got != "7" {
		t.Errorf("got %q; want \"7\"", got)
	}
}

// The same through a *prelude* generic and a data constructor — the type argument
// crosses a module boundary in both directions, which is where a fix that merely
// entered the call site's module rather than the generic's would come apart.
func TestExec_PrivateModuleTypeThroughPreludeGenerics(t *testing.T) {
	t.Parallel()
	const src = `
module main
struct Tag { n: i64 }
data Box = Empty | Full(Tag)
let idf<t> = (a: t) -> t => a
let main = () -> void => {
  let m = Some(Tag { n: 8 });
  match m { Some(v) => println("${v.n}"), None => println("0") };
  let b = idf(Full(Tag { n: 9 }));
  match b { Full(v) => println("${v.n}"), Empty => println("0") };
}
`
	if got := strings.TrimSpace(buildAndRunWithPrelude(t, src, "")); got != "8\n9" {
		t.Errorf("got %q; want \"8\\n9\"", got)
	}
}

// **A prelude generic specialized at a *privately* declared type.** Lowering a
// specialization enters the generic function's module, because the names in its own
// signature live there — but a type **argument** comes from the caller, and a private
// declaration is keyed `<module>::<name>`, so one module cannot serve both. Until 08/22
// `Some(card)` plus `unwrap_or` on a private `struct Card` failed with
// `llvm: unknown named type "Card"`, and adding `pub` fixed it, which is what made the bug
// look like it was about generics rather than about visibility.
//
// No `pub` anywhere here, which is the whole assertion: this is what an ordinary program
// looks like before anyone has had a reason to export anything.
func TestExec_PreludeGenericAtAPrivateType(t *testing.T) {
	t.Parallel()
	out := buildAndRunWithPrelude(t, `
module main
struct Card { rank: i64, suit: string }
let main = () -> void => {
  let a = Card { rank: 7, suit: "hearts" }
  let b = Card { rank: 8, suit: "spades" }
  let m = Some(a)
  print("${m.unwrap_or(b).suit}")
}
`, "")
	if got := strings.TrimSpace(out); got != "hearts" {
		t.Errorf("prelude generic at a private struct = %q; want \"hearts\"", got)
	}
}

// The **composed** path, which resolves through a different site: a generic calling a
// generic records a *template*, and the driver composes the caller's bindings into it. The
// concrete types in that composition came from the **outer** call, so the site travels
// outward with them — keeping the template's own site would name a module inside the
// library, where a caller's private type is not visible.
func TestExec_ComposedGenericAtAPrivateType(t *testing.T) {
	t.Parallel()
	out := buildAndRunWithPrelude(t, `
module main
struct Card { rank: i64, suit: string }
let get_or<t> = pure (o: Maybe<t>, d: t) -> t => o.unwrap_or(d)
let main = () -> void => {
  let a = Card { rank: 7, suit: "hearts" }
  let b = Card { rank: 8, suit: "spades" }
  print("${get_or(Some(a), b).suit} ${get_or(None, b).suit}")
}
`, "")
	if got := strings.TrimSpace(out); got != "hearts spades" {
		t.Errorf("composed generic at a private struct = %q; want \"hearts spades\"", got)
	}
}
