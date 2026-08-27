package llvm

import (
	"strings"
	"testing"
)

// **An argument position is a context that determines a construction's allocation
// flavor**, and it was the one such context left out.
//
// A construction has no flavor of its own — `Node { v: 2 }` is inline or heap-boxed
// depending on what it is being used *as* — so the flavor is pushed down from the context
// (`propagateExpectedType`). An annotated binding and a declared return already did that; an
// argument did not, so `take(Node { v: 2 })` against a `shared Node` parameter built the
// struct inline and passed it **by value** to a callee expecting a box pointer.
//
// The front end accepted it. On macOS it segfaulted; on Linux, whose clang still uses typed
// pointers, it was a compile error naming the mismatch exactly — `'@take' defined with type
// 'i64 ({i64, i64, %Node}*)*' but expected 'i64 (%Node)*'` — which is the class of fault
// `asan.sh` exists to catch and did.
func TestExec_SharedArgumentFromABareConstruction(t *testing.T) {
	t.Parallel()
	out, _ := buildAndRunCapture(t, `
struct Node { v: i64 }
data List = Nil | Cons(i64)
let s_struct = pure (n: shared Node) -> i64 => n.v
let s_data = pure (l: shared List) -> i64 => match l { Nil => 0, Cons(n) => n }
let s_arr = pure (a: shared [3]i64) -> i64 => a[1]
let plain = pure (n: Node) -> i64 => n.v
let main = () -> void => {
  // A construction inside a match arm reaches the argument through the same recursion
  // propagateExpectedType already used for a binding.
  let viaArm = s_data(match 1 { 0 => Nil, _ => Cons(9) })
  println("${s_struct(Node { v: 1 })} ${s_data(Cons(2))} ${s_arr([0, 4, 0])} ${plain(Node { v: 5 })} ${viaArm}")
}
`)
	if got := strings.TrimSpace(out); got != "1 2 4 5 9" {
		t.Errorf("got %q; want \"1 2 4 5 9\"", got)
	}
}

// The annotated-binding path, which already worked, must keep working — the two spellings
// of the same thing now agree instead of one of them crashing.
func TestExec_SharedArgumentAgreesWithAnAnnotatedBinding(t *testing.T) {
	t.Parallel()
	out, _ := buildAndRunCapture(t, `
struct Node { v: i64 }
let take = pure (n: shared Node) -> i64 => n.v
let main = () -> void => {
  let bound: shared Node = Node { v: 7 }
  println("${take(bound)} ${take(Node { v: 7 })}")
}
`)
	if got := strings.TrimSpace(out); got != "7 7" {
		t.Errorf("got %q; want \"7 7\" — the two spellings must agree", got)
	}
}

// A `shared` argument is still one box, aliased rather than copied: writing through the
// callee's view is visible to the caller, which is what the flavor means and what passing
// the struct by value would have silently broken.
func TestExec_SharedArgumentIsOneBoxASan(t *testing.T) {
	t.Parallel()
	src := `struct Cell { n: i64 }
let bump = det (c: mut shared Cell) -> void => { c.n = c.n + 1 }
let main = () -> u8 => {
  var c: shared Cell = Cell { n: 0 }
  bump(c)
  bump(c)
  if c.n == 2 { 3 } else { 1 }
}`
	clang := lookClang(t)
	if got := buildAndRun(t, src); got != 3 {
		t.Errorf("exited %d; want 3", got)
	}
	if got := buildAndRunASan(t, clang, src); got != 3 {
		t.Errorf("under ASan: exited %d; want 3", got)
	}
}
