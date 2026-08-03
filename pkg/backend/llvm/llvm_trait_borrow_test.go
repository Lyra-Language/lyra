package llvm

import (
	"strings"
	"testing"
)

// Borrow modifiers on a trait method's parameters (`bump: (mut Self) -> void`).
//
// The grammar always accepted these; the collector dropped them, so every trait method
// parameter was by value however it was written. The danger in fixing that is not the
// parsing but the *agreement*: Resolution.Lambda binds the body against the mode, and the
// call site must pass an address for the same parameters — a body expecting a pointer handed
// a value is a wild load, not a type error anything catches.

// A `mut` receiver writes through to the caller. By value the writes went to a private copy
// and were silently discarded, which is the failure mode paramIsByRef exists to prevent for
// free functions.
func TestExec_TraitMutReceiverWritesThrough(t *testing.T) {
	t.Parallel()
	got := buildAndRun(t, `
struct Counter { n: i64 }
trait Bump { bump: (mut Self) -> void }
impl Bump for Counter {
  bump = (self) => { self.n = self.n + 1 }
}
let main = () -> u8 => {
  var c = Counter { n: 5 }
  c.bump()
  c.bump()
  u8(c.n)
}`)
	if got != 7 {
		t.Errorf("exit = %d, want 7 — a `mut` receiver's writes must reach the caller", got)
	}
}

// A `ref` receiver reads the caller's live value. It is by reference for cost rather than
// semantics, so the observable property is simply that it still computes correctly — a
// receiver passed as a pointer where the body expects a value would not.
func TestExec_TraitRefReceiver(t *testing.T) {
	t.Parallel()
	got := buildAndRun(t, `
struct Big { a: i64, b: i64, c: i64 }
trait Peek { peek: (ref Self) -> i64 }
impl Peek for Big {
  peek = (self) => self.a + self.c
}
let main = () -> u8 => {
  let big = Big { a: 3, b: 0, c: 4 }
  u8(big.peek())
}`)
	if got != 7 {
		t.Errorf("exit = %d, want 7", got)
	}
}

// A borrow modifier on a non-receiver parameter, so the receiver offset is exercised:
// signature parameter 1 is `call.Arguments[0]`. Reading the modes one place over would pass
// the argument by value while the body expects a pointer.
func TestExec_TraitMutArgumentAfterReceiver(t *testing.T) {
	t.Parallel()
	got := buildAndRun(t, `
struct Cell { v: i64 }
struct Tag { id: i64 }
trait Fill { fill: (Self, mut Cell) -> void }
impl Fill for Tag {
  fill = (self, target) => { target.v = self.id }
}
let main = () -> u8 => {
  let t = Tag { id: 9 }
  var c = Cell { v: 0 }
  t.fill(c)
  u8(c.v)
}`)
	if got != 9 {
		t.Errorf("exit = %d, want 9 — the write must reach the caller's cell", got)
	}
}

// The modes reach the emitted IR: a by-reference parameter is a pointer in the signature.
// This is the half that cannot be observed by running a correct program — a value and a
// pointer both "work" until the body writes.
func TestEmit_TraitMutReceiverIsAPointer(t *testing.T) {
	t.Parallel()
	got, err := emitSource(t, `
struct Counter { n: i64 }
trait Bump { bump: (mut Self) -> void }
impl Bump for Counter {
  bump = (self) => { self.n = self.n + 1 }
}
let main = () -> u8 => {
  var c = Counter { n: 1 }
  c.bump()
  u8(c.n)
}`)
	if err != nil {
		t.Fatal(err)
	}
	body := funcBody(got, "bump")
	if body == "" {
		t.Fatalf("no emitted bump method:\n%s", got)
	}
	if !strings.Contains(body, "*") {
		t.Errorf("a `mut` receiver should be passed as a pointer:\n%s", body)
	}
}
