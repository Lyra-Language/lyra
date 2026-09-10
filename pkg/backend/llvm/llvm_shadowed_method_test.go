package llvm

import (
	"strings"
	"testing"
)

// **A method name is resolved against the receiver, so a local of that name is irrelevant
// to it** — and the backend must not decide otherwise by asking `l.locals`.
//
// `data.data()` on a parameter named `data` is `std.ffi`'s free function, which the
// typechecker resolves and desugars into `data(data)` before the backend sees anything. The
// backend then looked the *synthesized* callee identifier up by name, found the parameter,
// and lowered a call through a `[]u8` as though it were a function value — hazard 9 in its
// plainest form. `lyrac check` passed and `lyrac build` failed with "no type recorded for
// the callee of an indirect call", naming neither the call nor the file.
//
// The symptom is what makes it worth a test: **the whole module stopped lowering**, so
// every program importing `bindings.raylib` failed over one parameter name in one file it
// did not touch, and bisection was the only way in (09/10).
func TestExec_MethodCallShadowedByALocalOfTheSameName(t *testing.T) {
	t.Parallel()
	out := buildAndRunWithPrelude(t, `
module main
import std.ffi.{ data }
let head = (data: []u8) -> u8 => unsafe { let p = data.data(); p^ }
let main = () -> void => {
  var xs: []u8 = [65, 66]
  print("${head(xs)}")
}
`, "")
	if got := strings.TrimSpace(out); got != "65" {
		t.Errorf("got %q; want \"65\"", got)
	}
}

// The other direction, and the reason the local branch exists at all: a local binding
// holding a function value shadows a top-level function of the same name, and a call
// through it is still the indirect call it always was. The typechecker records no callee
// declaration for one, which is what tells the two cases apart.
func TestExec_LocalClosureStillShadowsATopLevelFunction(t *testing.T) {
	t.Parallel()
	out := buildAndRunWithPrelude(t, `
module main
let twice = pure (n: i64) -> i64 => n * 2
let main = () -> void => {
  let twice = pure (n: i64) -> i64 => n + 100
  print("${twice(5)}")
}
`, "")
	if got := strings.TrimSpace(out); got != "105" {
		t.Errorf("got %q; want \"105\"", got)
	}
}

// The same hazard one pass over: a closure over a parameter whose name a **top-level
// declaration** also has. The captures pass subtracted its globals set by bare name, so
// the closure carried no slot for it and the backend lowered the read as a reference to
// the global — a function value where an aggregate belonged, which killed `lyrac build`
// with a Go panic out of llir (09/10, `bindings/raylib`'s `tint`).
//
// Written as a value the program can print, because the miscompile is silent: with the
// capture missing the closure reads something that is not the argument, and only running
// it says which.
func TestExec_ClosureOverAParameterNamedLikeATopLevelFunction(t *testing.T) {
	t.Parallel()
	out := buildAndRunWithPrelude(t, `
module main
let tint = pure (n: i64) -> i64 => n * 1000
let apply = pure (f: (i64) -> i64, n: i64) -> i64 => f(n)
let shifted = pure (tint: i64) -> i64 => apply(pure (n: i64) -> i64 => n + tint, 5)
let main = () -> void => {
  print("${shifted(7)} ${tint(2)}")
}
`, "")
	if got := strings.TrimSpace(out); got != "12 2000" {
		t.Errorf("got %q; want \"12 2000\"", got)
	}
}
