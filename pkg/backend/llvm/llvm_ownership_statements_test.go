package llvm

import (
	"os/exec"
	"strings"
	"testing"
)

// Ownership under the statement kinds its walk had no case for.
//
// `analyzer.stmt` switched over five statement kinds out of eighteen. Six of the missing
// ones carry an expression, so nothing — no retain, release or transfer — was recorded for
// it. That is hazard 8 in the shape the UnsafeBlockExpr hole already took once: the walk is
// exhaustive by convention rather than by a type, and a missing arm is silent.
//
// Three of the six were *harmless* and one was not, which is why each test below states
// which it is. The distinction is worth keeping in view: a missing **release** defers to
// scope exit, where the managed frame is a leak-safe backstop, while a missing **retain**
// has no backstop at all.

// `p^ = v` on a managed value: the pointee slot takes ownership, and without a retain two
// names hold one box with one reference between them.
//
// This is the one that was memory-unsafe. ASan reports a heap-use-after-free inside
// `lyra_rc_release` — the second release of a box the first already freed — and the emitted
// module carries two `lyra_rc_release` calls for one live value where it should carry one.
func TestExec_DerefAssignmentRetainsWhatItStores(t *testing.T) {
	t.Parallel()
	const src = `let main = () -> u8 => {
  let a: string = "a" ++ "b"
  var t: string = "x" ++ "y"
  unsafe {
    let p = &mut t
    p^ = a
  }
  u8(t.len() - 2)
}`
	if got := buildAndRun(t, src); got != 0 {
		t.Errorf("exited %d; want 0", got)
	}
	clang, err := exec.LookPath("clang")
	if err != nil {
		t.Skip("clang not found on PATH; skipping ASan test")
	}
	if !asanAvailable(t, clang) {
		t.Skip("ASan runtime not available; skipping")
	}
	if code := buildAndRunASan(t, clang, src); code != 0 {
		t.Errorf("ASan run: exit %d — a write through a pointer that mints no retain "+
			"leaves the box with one reference and two owners", code)
	}
	// The count is what names the fault precisely: two releases against one live value.
	// Left in because the ASan half needs a working clang and skips without one, while
	// this half runs everywhere.
	ir, err := emitSource(t, src)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(ir, "call void @lyra_rc_release"); got != 1 {
		t.Errorf("%d releases; want 1 — two means the value written through the pointer "+
			"is released by both the binding it came from and the slot it went into", got)
	}
}

// A destructuring **borrows** out of its scrutinee — the bindings read the payload for the
// duration of the branch, and the reference belongs to whatever already held it.
//
// So the arm's absence was invisible whenever the scrutinee was a binding, which is nearly
// every `if let` anyone writes, and TestExec_DestructuringManagedPayload pins that no retain
// appears. The case it was not invisible for is a scrutinee **nothing else names**: only a
// walk records the release that disposes of a temporary, so `if let Some(s) = make()` leaked
// its box while `if let Some(s) = m` did not.
//
// Which is the general lesson about a missing walk: "borrowed" and "unvisited" produce the
// same +0 for a value someone else owns, and differ for one nobody does.
func TestEmit_DestructuringFromATemporaryReleasesIt(t *testing.T) {
	t.Parallel()
	for _, c := range []struct{ name, src string }{
		{"if let", `data Box = Full(string) | Empty
let mk = () -> Box => Full("a" ++ "b")
let main = () -> u8 => {
  var out: i64 = 0
  if let Full(s) = mk() { out = s.len() }
  u8(out - 2)
}`},
		{"tuple destructuring", `let mk = () -> (string, i64) => ("a" ++ "b", 1)
let main = () -> u8 => {
  let (p, n) = mk()
  u8(p.len() + n - 3)
}`},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if got := buildAndRun(t, c.src); got != 0 {
				t.Errorf("exited %d; want 0", got)
			}
			ir, err := emitSource(t, c.src)
			if err != nil {
				t.Fatal(err)
			}
			if got := strings.Count(ir, "call void @lyra_rc_release"); got != 1 {
				t.Errorf("%d releases; want 1 — a scrutinee nothing else names is disposed "+
					"of by this statement or by nobody", got)
			}
		})
	}
}
