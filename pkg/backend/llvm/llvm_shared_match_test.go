package llvm

import (
	"os/exec"
	"strings"
	"testing"
)

// A `shared` aggregate value lowers to a pointer to a ref-counted box
// `{ i64 rc, payload }`, so a `match` on it must first unbox — load the inline
// payload out of the box (unboxSharedData) — before the tag/pattern machinery,
// which works on the first-class payload, can run. These cases cover the data,
// struct, and (via the recursive list) FBIP-shaped forms, plus the two ladder
// fallbacks (a payload value test and a guard). The prerequisite for Perceus
// reuse/FBIP on `shared` values: you can't reuse a box you can't destructure.
var sharedMatchCases = []struct {
	name string
	src  string
	want int
}{
	{
		// The core case: destructure a `shared data` value through its box and read
		// a payload field.
		"data payload through the box",
		`data Box = Empty | Full(i64)
		 let main = () -> u8 => {
		   let b: shared Box = Full(42)
		   match b {
		     Empty => 0,
		     Full(n) => u8(n)
		   }
		 }`,
		42,
	},
	{
		// A nullary variant of a `shared data` value (no payload to reinterpret).
		"nullary variant",
		`data Box = Empty | Full(i64)
		 let main = () -> u8 => {
		   let b: shared Box = Empty
		   match b {
		     Empty => 5,
		     Full(n) => u8(n)
		   }
		 }`,
		5,
	},
	{
		// An identifier catch-all binds the *whole* shared value — the box pointer,
		// not the unboxed union — so re-matching it must still work.
		"identifier catch-all keeps the box pointer",
		`data Box = Empty | Full(i64)
		 let first = (b: shared Box) -> i64 => match b {
		   Empty => 0,
		   other => match other { Full(n) => n, Empty => 0 }
		 }
		 let main = () -> u8 => {
		   let b: shared Box = Full(7)
		   u8(first(b))
		 }`,
		7,
	},
	{
		// A recursive `shared` list summed through the box on every level — the
		// map/fold shape reuse analysis will later optimize. The list is owned by
		// main; each `sum` borrows (bare `shared` param) so the whole chain frees once.
		"recursive shared list sum",
		`data List = Nil | Cons(i64, shared List)
		 let sum = (xs: shared List) -> i64 => match xs {
		   Nil => 0,
		   Cons(h, t) => h + sum(t)
		 }
		 let main = () -> u8 => {
		   let xs: shared List = Cons(10, Cons(20, Cons(12, Nil)))
		   u8(sum(xs))
		 }`,
		42,
	},
	{
		// A value-testing payload sub-pattern (`Full(0)`) rules out the compact tag
		// switch, so the unboxed union flows into the shared aggregate ladder.
		"payload value test (ladder fallback)",
		`data Box = Empty | Full(i64)
		 let main = () -> u8 => {
		   let b: shared Box = Full(0)
		   match b {
		     Full(0) => 9,
		     Full(n) => u8(n),
		     Empty => 0
		   }
		 }`,
		9,
	},
	{
		// A guard also forces the ladder fallback; it references a binding pulled out
		// of the box.
		"arm guard (ladder fallback)",
		`data Box = Empty | Full(i64)
		 let main = () -> u8 => {
		   let b: shared Box = Full(3)
		   match b {
		     Full(n) if n > 5 => 1,
		     Full(n) => u8(n * 2),
		     Empty => 0
		   }
		 }`,
		6,
	},
	{
		// The same unbox handles a `shared` struct scrutinee (single shape, no tag).
		"shared struct match",
		`struct Pair { x: u8, y: u8, }
		 let main = () -> u8 => {
		   let p: shared Pair = Pair { x: 40, y: 2 }
		   match p {
		     Pair { x, y } => x + y
		   }
		 }`,
		42,
	},
}

// TestExec_SharedMatch runs each shared-scrutinee match and checks the result — a
// wrong answer means the box was mis-unboxed, a payload mis-read, or an arm
// mis-selected.
func TestExec_SharedMatch(t *testing.T) {
	for _, c := range sharedMatchCases {
		if got := buildAndRun(t, c.src); got != c.want {
			t.Errorf("%s: exited %d; want %d", c.name, got, c.want)
		}
	}
}

// TestExec_SharedMatchASan runs each case under AddressSanitizer: matching a
// shared value only reads through the box (no reference consumed), and the box's
// own last-use release must fire exactly once — a double free or use-after-free
// from a mis-placed drop aborts here.
func TestExec_SharedMatchASan(t *testing.T) {
	clang, err := exec.LookPath("clang")
	if err != nil {
		t.Skip("clang not found on PATH")
	}
	if !asanAvailable(t, clang) {
		t.Skip("AddressSanitizer not available in this toolchain")
	}
	for _, c := range sharedMatchCases {
		if got := buildAndRunASan(t, clang, c.src); got != c.want {
			t.Errorf("%s (asan): exited %d; want %d", c.name, got, c.want)
		}
	}
}

// TestEmit_SharedMatchIR pins the mechanics: the scrutinee is unboxed (a load of
// the box's payload struct) and, since the match is the binding's last use, the
// box is allocated once and released once — no leak, no double free.
func TestEmit_SharedMatchIR(t *testing.T) {
	got, err := emitSource(t, `data Box = Empty | Full(i64)
	 let main = () -> u8 => {
	   let b: shared Box = Full(42)
	   match b {
	     Empty => 0,
	     Full(n) => u8(n)
	   }
	 }`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "load %Box") {
		t.Errorf("expected the scrutinee to be unboxed (load %%Box):\n%s", got)
	}
	allocs := strings.Count(got, "call i8* @lyra_rc_alloc")
	rels := strings.Count(got, "call void @lyra_rc_release")
	if allocs != 1 {
		t.Errorf("want 1 box allocation, got %d", allocs)
	}
	if rels != allocs {
		t.Errorf("conservation: %d releases != %d allocations (leak or double free)", rels, allocs)
	}
}

// TestEmit_NestedSharedDataPatternDeferred documents the boundary of this slice: a
// nested `shared data` sub-pattern (destructuring the tail through *its* box, not
// just the top-level scrutinee) is not handled and must error loudly rather than
// miscompile.
func TestEmit_NestedSharedDataPatternDeferred(t *testing.T) {
	_, err := emitSource(t, `data List = Nil | Cons(i64, shared List)
	 let main = () -> u8 => {
	   let xs: shared List = Cons(1, Cons(2, Nil))
	   match xs {
	     Cons(h, Cons(h2, t2)) => u8(h + h2),
	     Cons(h, t) => u8(h),
	     Nil => 0
	   }
	 }`)
	if err == nil {
		t.Errorf("expected a loud error for a nested shared-data sub-pattern")
	}
}
