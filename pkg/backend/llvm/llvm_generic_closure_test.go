package llvm

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Lyra-Language/lyra/pkg/driver"
	"github.com/Lyra-Language/lyra/pkg/modules"
)

// A **lambda written inside a generic body** is lifted to a function whose signature
// mentions the enclosing type variables, so it has no single representation: `(x: t) -> t`
// is a different function at `t = i64` than at `t = string`. Until 08/26 the lifted
// functions were collected once, program-wide, and one was emitted per lambda *node* — so
// the simplest form of this failed to build with *"type variable t has no concrete type
// here"*, a message naming neither the lambda nor the function containing it, on a program
// the front end checks clean.
//
// One lifted function per (lambda, specialization) now, which is the same
// substitute-don't-clone arrangement the enclosing generic function already had.
func TestExec_AClosureInsideAGenericBody(t *testing.T) {
	t.Parallel()
	out, _ := buildAndRunCapture(t, `
let up<t> = pure (v: t) -> t => {
  let f = (x: t) -> t => x
  f(v)
}
let main = () -> void => println(up(7))
`)
	if got := strings.TrimSpace(out); got != "7" {
		t.Errorf("closure in a generic body = %q; want \"7\"", got)
	}
}

// Two instantiations of one body, at types whose representation differs — a scalar and a
// reference-counted string. This is what a single emitted function could not have served
// even if its signature had somehow been lowered: the environment's layout, its byte size
// and whether its fields need drop glue are all answers about the type argument.
//
// `pair` captures a `t`, which is the case that reaches every one of those decisions;
// `twice_over` passes its closure on to another generic, so the specialization is composed
// rather than requested directly.
func TestExec_AGenericClosureAtTwoInstantiations(t *testing.T) {
	t.Parallel()
	out, _ := buildAndRunCapture(t, `
let apply<t> = pure (self: t, f: (t) -> t) -> t => f(self)
let pair<t> = pure (a: t, b: t) -> t => {
  let pick = (x: t) -> t => b
  apply(a, pick)
}
let twice_over<t> = pure (v: t) -> t => {
  let same = (x: t) -> t => x
  apply(apply(v, same), same)
}
let main = () -> void => {
  print("${pair(1, 2)} ${pair("left", "right")} ")
  print("${twice_over(9)} ${twice_over("kept")}")
}
`)
	if got := strings.TrimSpace(out); got != "2 right 9 kept" {
		t.Errorf("two instantiations = %q; want \"2 right 9 kept\"", got)
	}
}

// The memory half, which the two above cannot see: a captured `t = string` is retained
// when the environment is built and released by the environment's drop glue, while the
// same node at `t = i64` needs neither. Both decisions read the capture's type through
// `capturesOf`, so a substitution missing there is a leak at one instantiation or a double
// free at the other — neither of which changes the printed output.
func TestExec_AGenericClosureCapturesAManagedValueASan(t *testing.T) {
	t.Parallel()
	src := `let apply<t> = (self: t, f: (t) -> t) -> t => f(self)
let held<t> = (a: t, b: t) -> t => {
  let pick = (x: t) -> t => b
  apply(a, pick)
}
let main = () -> u8 => {
  let s = held("ab" ++ "cd", "ef" ++ "gh")
  let n = held(1, 2)
  if s == "efgh" && n == 2 { 3 } else { 1 }
}`
	clang := lookClang(t)
	if got := buildAndRun(t, src); got != 3 {
		t.Errorf("exited %d; want 3", got)
	}
	if got := buildAndRunASan(t, clang, src); got != 3 {
		t.Errorf("under ASan: exited %d; want 3", got)
	}
}

// **A trait default body is checked in its own module's scope**, which a *setup* pass does
// not get for free: the per-statement loop wraps each top-level statement in
// `checkInModule`, and default bodies are checked before that loop runs. So `tc.scope` was
// the global scope, which holds only what modules export — and a bare reference to one of
// the module's own top-level names resolved to nothing.
//
// The miss was silent. The "undefined function" arm is guarded by a visibility check that
// answers *found but private* for a name the global scope cannot see, so the call was
// abandoned with no diagnostic and no recorded instantiation, and the program failed in the
// backend as `call to unknown function`.
//
// **A single-module program hid it completely**: with no prelude the global scope holds the
// program's own declarations, so every reproduction small enough to paste worked. That is
// hazard 13's module header again, in a different pass — which is why this test is here
// rather than in the typechecker's, where the harness has no prelude.
func TestExec_ATraitDefaultBodyReachesItsModulesOwnNames(t *testing.T) {
	t.Parallel()
	out := buildAndRunWithPrelude(t, `
module main
const OPEN = "["
let boxed<t> = pure (self: t) -> string => OPEN
trait Tag {
  pure raw: (Self) -> string
  pure tag: (Self) -> string = (self) => boxed(self) ++ self.raw()
}
impl Tag for i64 { raw = pure (self) => "1" }
impl Tag for string { raw = pure (self) => self }
let main = () -> void => print("${(1).tag()} ${"s".tag()}")
`, "")
	if got := strings.TrimSpace(out); got != "[1 [s" {
		t.Errorf("default body reaching its module = %q; want \"[1 [s\"", got)
	}
}

// The second half of the same failure, and it survives the scope fix on its own: the call
// now resolves and records `boxed<t=Self>` — a *template*, whose binding is the type
// variable a default body's `Self` is. Nothing composed it, because the instantiation
// closure seeded only from generic *functions*, and a trait method is not one.
//
// Seeded from `MethodTable.Specializations()` now, the same reached set the per-method
// ownership pass reads. A non-default impl method escaped the bug only by accident — its
// receiver is the concrete impl type, so its calls were concrete already and needed no
// composing.
//
// `trait Doubled: Show` is how the callee's `where t: Show` is satisfied from a default
// body — a supertrait *is* the bound on `Self`, there being nowhere on a trait method to
// write a `where` clause of its own.
func TestExec_ATraitDefaultBodyCallingAGenericAtTwoTypes(t *testing.T) {
	t.Parallel()
	out := buildAndRunWithPrelude(t, `
module main
let twice<t> where t: Show = pure (self: t, other: t) -> string => "${self}${other}"
trait Doubled: Show {
  pure one: (Self) -> Self
  pure both: (Self) -> string = (self) => twice(self, self.one())
}
impl Doubled for i64 { one = pure (self) => self + 1 }
impl Doubled for string { one = pure (self) => self ++ "!" }
let main = () -> void => print("${(4).both()} ${"a".both()}")
`, "")
	if got := strings.TrimSpace(out); got != "45 aa!" {
		t.Errorf("default body calling a generic = %q; want \"45 aa!\"", got)
	}
}

// A name that genuinely is not there must still be reported — the scope fix restores the
// diagnostic the silent miss had been swallowing, rather than trading one silence for
// another.
func TestCheck_AnUndefinedNameInADefaultBodyIsReported(t *testing.T) {
	t.Parallel()
	diags := analyzeWithPreludeErrors(t, `
module main
trait Tag {
  pure raw: (Self) -> string
  pure tag: (Self) -> string = (self) => nosuch(self) ++ self.raw()
}
impl Tag for i64 { raw = pure (self) => "1" }
let main = () -> void => println((1).tag())
`)
	if !strings.Contains(diags, `undefined function "nosuch"`) {
		t.Errorf("diagnostics = %q; want the undefined-function error", diags)
	}
}

// analyzeWithPreludeErrors runs the front end over src *with the real prelude* and returns
// its errors joined. The prelude is the point: a diagnostic that depends on which module a
// name is looked up from cannot be reproduced in a single-module program, where the global
// scope happens to hold the program's own declarations.
func analyzeWithPreludeErrors(t *testing.T, src string) string {
	t.Helper()
	dir := t.TempDir()
	entry := filepath.Join(dir, "app.lyra")
	if err := os.WriteFile(entry, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	units, diags := modules.Resolve(entry, []string{dir, repoStdRoot(t)}, modules.Options{Prelude: modules.PreludeModule})
	if len(diags) != 0 {
		t.Fatalf("resolve: %v", diags)
	}
	var msgs []string
	for _, e := range driver.AnalyzeUnits(units).Errors() {
		msgs = append(msgs, e.Message)
	}
	return strings.Join(msgs, "\n")
}
