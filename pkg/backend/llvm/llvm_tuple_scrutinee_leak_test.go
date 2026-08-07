package llvm

import (
	"strings"
	"testing"
)

// The leak that failed CI on 08/07, and the two independent defects behind it.
//
// A **multi-clause function desugars to `match (p0, p1) { … }`**, so its scrutinee
// is a stack tuple built over the parameters. That tuple deep-retains its managed
// elements on construction — and nothing released them, so every call to the
// prelude's `unwrap_or` leaked one reference to its receiver's box. `line.unwrap_or("")`
// in a read loop therefore leaked one box per iteration, which is exactly what
// TestExec_ReadLineUnderASan does.
//
// A single-scrutinee `match m { … }` never leaked, because there is no tuple to
// build. That is what hid it: the construct that leaks looks like pure sugar.
//
// These are compile-and-run under ASan+LSan (Linux) — the leak is invisible to the
// package's usual path-sensitive conservation check, which counts lyra_rc_alloc calls
// *within* one function and cannot see a box that arrives as a return value.
func TestExec_TupleScrutineeReleasesItsElements(t *testing.T) {
	t.Parallel()
	const src = `
module main
let pick = (m: Maybe<string>, fb: string) -> string {
  (Some v, _) => v,
  (None, f) => f,
}
let main = () -> void => {
  var i = 0;
  for i < 3 {
    let line = read_line();
    println(pick(line, "!"));
    i = i + 1;
  }
}
`
	if got := strings.TrimSpace(buildAndRunWithPrelude(t, src, "a\nb\nc\n")); got != "a\nb\nc" {
		t.Errorf("got %q; want \"a\\nb\\nc\"", got)
	}
}

// The prelude's own combinator, which is the shape the CI failure actually took.
func TestExec_PreludeUnwrapOrInALoopDoesNotLeak(t *testing.T) {
	t.Parallel()
	const src = `
module main
let main = () -> void => {
  var i = 0;
  for i < 3 {
    let line = read_line();
    println(line.unwrap_or("?"));
    i = i + 1;
  }
}
`
	if got := strings.TrimSpace(buildAndRunWithPrelude(t, src, "x\ny\nz\n")); got != "x\ny\nz" {
		t.Errorf("got %q; want \"x\\ny\\nz\"", got)
	}
}

// The second defect, which the first fix *exposed* rather than caused: a box's own
// `drop_fn` can drop the last **weak** reference to that same box, through a cycle —
// a `Node` whose child holds `Maybe<weak Node>` back at its parent.
//
// lyra_rc_release used to decrement strong, run drop_fn, and only then test the weak
// count to decide whether to free. So the glue freed the memory from underneath it
// (weak hit 0 with strong already 0) and the release freed it a second time: an
// ASan-confirmed double free. The strong side now holds one **implicit weak
// reference**, dropped after drop_fn returns, so the count cannot reach zero while
// the glue is running. Rust's Arc does the same, for the same reason.
//
// It only became reachable once the retain/drop glue walked a `Maybe<shared T>` field
// at all — before that the cycle's fields were never released, so this test passed by
// leaking. That is worth keeping in mind: a memory-safety test can be green because
// the code under it does nothing.
func TestExec_DropGlueMayDropTheLastWeakRefToItsOwnBox(t *testing.T) {
	t.Parallel()
	const src = `
data Maybe<t> = None | Some(t)
struct Node { n: i64, parent: Maybe<weak Node>, kid: Maybe<shared Node> }
let main = () -> u8 => {
  let mut parent: shared Node = Node { n: 3, parent: None, kid: None }
  let child: shared Node = Node { n: 4, parent: Some(parent.weak()), kid: None }
  parent.kid = Some(child)
  var out = 0
  match child.parent {
    Some(w) => { if let p = w { out = p.n } ; out },
    None => 0,
  }
  u8(out)
}
`
	if got := buildAndRun(t, src); got != 3 {
		t.Errorf("exited %d; want 3", got)
	}
	if got := buildAndRunASan(t, lookClang(t), src); got != 3 {
		t.Errorf("ASan run exited %d; want 3 (a double free aborts)", got)
	}
}
