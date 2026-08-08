package llvm

import (
	"strings"
	"testing"
)

// The anonymous struct, end to end (08/08). It type-checked and had **no backend at
// all** — `lowerType`, construction, field access and the ownership glue all lacked an
// arm — which stayed invisible because the type was not assignable to itself, so no
// value ever got far enough to be lowered.

func TestExec_AnonymousStructRoundTrip(t *testing.T) {
	t.Parallel()
	const src = `
module main
let main = () -> void => {
  let a: { x: i64, y: string } = { x: 1, y: "s" };
  println("${a.x} ${a.y}");
}
`
	if got := strings.TrimSpace(buildAndRunWithPrelude(t, src, "")); got != "1 s" {
		t.Errorf("anonymous struct = %q; want \"1 s\"", got)
	}
}

// **Fields are placed by name, in the type's order.** An anonymous struct's identity is
// its fields, so a literal may write them in any order and mean the same value — the one
// thing this construction path does that the named one does not have to, since there a
// declaration fixes the order for every literal.
func TestExec_AnonymousStructFieldsAreOrderIndependent(t *testing.T) {
	t.Parallel()
	const src = `
module main
let mk = () -> { x: i64, y: string } => { y: "hi", x: 7 }
let main = () -> void => {
  let a: { x: u8, y: string } = { y: "s", x: 200 };
  let b = mk();
  println("${a.x} ${a.y} ${b.x} ${b.y}");
}
`
	if got := strings.TrimSpace(buildAndRunWithPrelude(t, src, "")); got != "200 s 7 hi" {
		t.Errorf("field order = %q; want \"200 s 7 hi\"", got)
	}
}

// A **managed** field, which is what the retain/drop arms are for: they were missing
// from `OwnsManaged`, `emitRetainValue` and `emitDropValue` alike, so the string leaked
// one reference per value. Run under LeakSanitizer by the Linux harness, which is the
// test that actually proves it — the text below was already right while it leaked.
func TestExec_AnonymousStructWithAManagedField(t *testing.T) {
	t.Parallel()
	const src = `
module main
let hold = () -> string => {
  let c: { m: string } = { m: "ab" ++ "cd" };
  let d = c;
  c.m ++ d.m
}
let main = () -> void => {
  for i in 0..<3 { println(hold()) };
}
`
	// The struct owns a **temporary** — the `++` result is transferred into it, with no
	// binding of its own to be released at scope exit. That is what makes this
	// discriminating: an earlier draft bound the string first (`let s = …; { m: s }`),
	// and its release at the binding's scope exit balanced the books whether or not the
	// struct's glue existed. The loop is there so a per-call leak accumulates.
	want := "abcdabcd\nabcdabcd\nabcdabcd"
	if got := strings.TrimSpace(buildAndRunWithPrelude(t, src, "")); got != want {
		t.Errorf("managed field = %q; want %q", got, want)
	}
}

// Passing, returning, reassigning, and structural equality — the operations that make
// it a value rather than a shape the checker knows about.
func TestExec_AnonymousStructAsAValue(t *testing.T) {
	t.Parallel()
	const src = `
module main
let read = (r: { x: i64 }) -> i64 => r.x
let main = () -> void => {
  var v: { x: i64 } = { x: 1 };
  v = { x: 2 };
  let same = v == v;
  println("${read(v)} ${same}");
}
`
	if got := strings.TrimSpace(buildAndRunWithPrelude(t, src, "")); got != "2 true" {
		t.Errorf("anonymous struct as a value = %q; want \"2 true\"", got)
	}
}

// Inside the aggregates that made the gap visible: an array of them, and a tuple
// carrying one. Both were parse or assignability failures before 08/08.
func TestExec_AnonymousStructInsideAggregates(t *testing.T) {
	t.Parallel()
	const src = `
module main
let main = () -> void => {
  let xs: []{ x: i64 } = [{ x: 1 }, { x: 2 }];
  let fixed: [2]{ x: i64 } = [{ x: 3 }, { x: 4 }];
  let t: ({ x: i64 }, i64) = ({ x: 5 }, 6);
  println("${xs[1].x} ${fixed[0].x} ${t.0.x} ${t.1}");
}
`
	if got := strings.TrimSpace(buildAndRunWithPrelude(t, src, "")); got != "2 3 5 6" {
		t.Errorf("nested anonymous structs = %q; want \"2 3 5 6\"", got)
	}
}
