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
