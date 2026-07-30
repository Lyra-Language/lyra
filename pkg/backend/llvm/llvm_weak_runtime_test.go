package llvm

import (
	"strings"
	"testing"
)

// `weak` gets runtime semantics: it can be created, it can be upgraded, and the
// upgrade *fails* once the referent is gone.
//
// Before this, `weak T` was a type and nothing more — it collected, resolved, and
// broke E014 size cycles, but no expression produced one, so a struct with a weak
// field could be declared and never built. What makes the feature real is the
// two-count box header: the payload dies when the strong count hits 0, and the
// memory is freed only when the weak count also hits 0, so a weak reference always
// has a live header to read a strong count out of. That is what lets the failed
// upgrade below be safe rather than a read of freed memory.
func TestExec_WeakRuntime(t *testing.T) {
	t.Parallel()
	clang := lookClang(t)
	cases := []struct {
		name string
		src  string
		want int
	}{
		{
			// The upgrade succeeds while the referent is alive, and the branch sees a
			// real `shared T`.
			"upgrade succeeds while the referent lives",
			`struct Node { value: i64 }
			 let main = () -> u8 => {
			   let n: shared Node = Node { value: 7 }
			   let w = n.weak()
			   var out = 0
			   if let s = w { out = s.value } else { out = 42 }
			   u8(out)
			 }`,
			7,
		},
		{
			// The case the whole design exists for: the strong reference died at the
			// helper's exit, the weak one outlived it, and the upgrade reports that
			// rather than handing back a dangling pointer. Reading the strong count
			// here is safe precisely because the weak count kept the header alive.
			"upgrade fails once the referent is gone",
			`struct Node { value: i64 }
			 let makeDead = () -> weak Node => {
			   let n: shared Node = Node { value: 7 }
			   n.weak()
			 }
			 let main = () -> u8 => {
			   let w = makeDead()
			   var out = 0
			   if let s = w { out = s.value } else { out = 42 }
			   u8(out)
			 }`,
			42,
		},
		{
			// An upgrade with no else branch: nothing happens when the referent is gone.
			"upgrade with no else branch",
			`struct Node { value: i64 }
			 let main = () -> u8 => {
			   let n: shared Node = Node { value: 5 }
			   let w = n.weak()
			   var out = 1
			   if let s = w { out = out + s.value }
			   u8(out)
			 }`,
			6,
		},
		{
			// Two upgrades of the same weak reference: each takes and gives back its
			// own strong reference, so the second still succeeds.
			"upgraded twice",
			`struct Node { value: i64 }
			 let main = () -> u8 => {
			   let n: shared Node = Node { value: 3 }
			   let w = n.weak()
			   var out = 0
			   if let a = w { out = out + a.value }
			   if let b = w { out = out + b.value }
			   u8(out)
			 }`,
			6,
		},
		{
			// A weak reference passed into and out of a function: it is an ordinary
			// value, so it flows through parameters and returns like any other.
			"passed through a function",
			`struct Node { value: i64 }
			 let peek = (w: weak Node) -> i64 => {
			   var out = 0
			   if let s = w { out = s.value } else { out = 42 }
			   out
			 }
			 let main = () -> u8 => {
			   let n: shared Node = Node { value: 5 }
			   u8(peek(n.weak()))
			 }`,
			5,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if got := buildAndRun(t, c.src); got != c.want {
				t.Errorf("%s: exited %d; want %d", c.name, got, c.want)
			}
			if got := buildAndRunASan(t, clang, c.src); got != c.want {
				t.Errorf("%s: ASan run exited %d; want %d", c.name, got, c.want)
			}
		})
	}
}

// A weak reference has a lifecycle of its own: it holds a weak count, so its death
// gives one back. Getting this wrong leaks the box's *memory* — the payload is
// already gone, so nothing observable misbehaves and ASan (which cannot see leaks
// on macOS) stays quiet. The counts are the detector.
func TestEmit_WeakReferenceIsReleased(t *testing.T) {
	t.Parallel()
	src := `struct Node { value: i64 }
	 let makeDead = () -> weak Node => {
	   let n: shared Node = Node { value: 7 }
	   n.weak()
	 }
	 let main = () -> u8 => {
	   let w = makeDead()
	   var out = 0
	   if let s = w { out = s.value } else { out = 42 }
	   u8(out)
	 }`
	ir, err := emitSource(t, src)
	if err != nil {
		t.Fatal(err)
	}
	weakRetains := strings.Count(ir, "call void @lyra_rc_weak_retain")
	weakReleases := strings.Count(ir, "call void @lyra_rc_weak_release")
	if weakRetains != 1 {
		t.Errorf("expected 1 weak retain (the `n.weak()` downgrade), got %d", weakRetains)
	}
	if weakReleases != weakRetains {
		t.Errorf("weak accounting: %d retains != %d releases — the box's memory leaks",
			weakRetains, weakReleases)
	}
	// The upgrade goes through the runtime rather than dereferencing the pointer.
	if !strings.Contains(ir, "call i8* @lyra_rc_upgrade") {
		t.Errorf("expected the upgrade to call lyra_rc_upgrade:\n%s", ir)
	}
}

// (The two refusals that keep an unsound read unexpressible — a weak reference has
// no fields, and weak() needs a `shared` receiver — are typechecker diagnostics;
// see typechecker/tests/weak_test.go.)
