package llvm

import (
	"strings"
	"testing"
)

// Overflow-trap elision: when the value-range analysis proves an `+`/`-`/`*` (or a
// `+=`-style compound) cannot overflow, the backend emits the plain instruction
// instead of the `llvm.*.with.overflow` intrinsic + trap. The elided op is
// identical to the checked op on its no-overflow path, so results are unchanged.

func hasCheck(ir string) bool {
	return strings.Contains(ir, "with.overflow") || strings.Contains(ir, "lyra_panic_overflow")
}

func TestEmit_OverflowElision(t *testing.T) {
	emit := func(src string) string {
		out, err := emitSource(t, src)
		if err != nil {
			t.Fatalf("emit: %v", err)
		}
		return out
	}

	// Constant operands whose result fits the type → provably safe → elided.
	elided := []struct {
		name string
		src  string
	}{
		{"const add", `let main = () -> u8 => {
		  let a: u8 = 5
		  let b: u8 = 3
		  a + b
		}`},
		{"const mul", `let main = () -> u8 => {
		  let a: u8 = 10
		  let b: u8 = 5
		  a * b
		}`},
		{"compound add", `let main = () -> u8 => {
		  var a: u8 = 5
		  a += 3
		  a
		}`},
		// Refinement proves the range: x ∈ [0,99] here, so x + 1 ∈ [1,100] ⊆ u8.
		{"refined add", `let f = (x: u8) -> u8 => if x < 100 { x + 1 } else { 0 }
		let main = () -> u8 => f(1)`},
		// Loop widening tracks the counter precisely (i ∈ [0,2]), so both the body's
		// `i + 1` and the post's `i += 1` are provably safe — the whole loop is
		// trap-free. (Havoc'd, i would be ⊤ and neither would elide.)
		{"loop counter arithmetic", `let main = () -> u8 => {
		  var r: u8 = 0
		  for var i: u8 = 0; i < 3; i += 1 {
		    r = i + 1
		  }
		  r
		}`},
	}
	for _, c := range elided {
		if hasCheck(emit(c.src)) {
			t.Errorf("%s: expected the overflow check to be elided, but it's present", c.name)
		}
	}

	// Full-range operands (via parameters) can overflow → the check stays.
	kept := `let add = (a: u8, b: u8) -> u8 => a + b
	let main = () -> u8 => add(1, 2)`
	if !hasCheck(emit(kept)) {
		t.Error("an unprovable add must keep its overflow check")
	}
}

// Elision changes IR but never semantics: a provably-safe op computes the same
// value as the checked form would.
func TestExec_ElisionPreservesResults(t *testing.T) {
	cases := []struct {
		src  string
		want int
	}{
		{`let main = () -> u8 => {
		  let a: u8 = 20
		  let b: u8 = 22
		  a + b
		}`, 42},
		{`let main = () -> u8 => {
		  let a: u8 = 6
		  let b: u8 = 7
		  a * b
		}`, 42},
		{`let main = () -> u8 => {
		  var a: u8 = 40
		  a += 2
		  a
		}`, 42},
		// A loop whose counter arithmetic is elided still computes correctly:
		// r ends at i+1 for the last iteration i=2 → 3.
		{`let main = () -> u8 => {
		  var r: u8 = 0
		  for var i: u8 = 0; i < 3; i += 1 {
		    r = i + 1
		  }
		  r
		}`, 3},
	}
	for _, c := range cases {
		if got := buildAndRun(t, c.src); got != c.want {
			t.Errorf("%q exited %d, want %d", c.src, got, c.want)
		}
	}
}

// Soundness: an op the analysis can't prove safe still traps at runtime if it
// overflows — elision never hides a real overflow. (The value comes through a
// parameter, so the analysis widens to the full range and keeps the check.)
func TestExec_ElisionKeepsRealTrap(t *testing.T) {
	src := `let add = (a: u8, b: u8) -> u8 => a + b
	let main = () -> u8 => add(200, 100)`
	if got := buildAndRun(t, src); got != trapExitCode {
		t.Errorf("a real (unprovable) overflow must still trap: exited %d, want %d", got, trapExitCode)
	}
}

// Divide-by-zero elision: a provably-nonzero divisor drops the check; a full-range
// (parameter) divisor keeps it. The module under test has exactly one division, so
// the presence/absence of the trap string is decisive.
func TestEmit_DivisionElision(t *testing.T) {
	emit := func(src string) string {
		out, err := emitSource(t, src)
		if err != nil {
			t.Fatalf("emit: %v", err)
		}
		return out
	}

	// Constant nonzero divisor → the divide-by-zero (and, being unsigned, the
	// overflow) check are both elided → neither trap is emitted.
	elided := emit(`let main = () -> u8 => {
	  let a: u8 = 84
	  a / 2
	}`)
	if strings.Contains(elided, "lyra_panic_divide_by_zero") {
		t.Error("a provably-nonzero divisor should elide the divide-by-zero check")
	}
	if strings.Contains(elided, "lyra_panic_overflow") {
		t.Error("an unsigned division can't overflow — no overflow check expected")
	}

	// A parameter divisor spans 0 → the divide-by-zero check stays.
	kept := emit(`let d = (a: u8, b: u8) -> u8 => a / b
	let main = () -> u8 => d(84, 2)`)
	if !strings.Contains(kept, "lyra_panic_divide_by_zero") {
		t.Error("a full-range divisor must keep the divide-by-zero check")
	}
}

// Array-bounds elision: an index the range analysis proves is within [0, size)
// drops the bounds check; a full-range (parameter) index keeps it.
func TestEmit_BoundsElision(t *testing.T) {
	emit := func(src string) string {
		out, err := emitSource(t, src)
		if err != nil {
			t.Fatalf("emit: %v", err)
		}
		return out
	}

	// The loop counter i ∈ [0,2] (widening fixpoint) indexes a size-3 array → the
	// bounds check is elided (and no negative-index adjustment is emitted).
	elided := emit(`let main = () -> u8 => {
	  let xs: [3]u8 = [10, 20, 12]
	  var sum: u8 = 0
	  for var i: u8 = 0; i < 3; i += 1 {
	    sum += xs[i]
	  }
	  sum
	}`)
	if strings.Contains(elided, "lyra_panic_index_out_of_bounds") {
		t.Error("a loop counter provably in [0,size) should elide the bounds check")
	}

	// A parameter index spans the whole type → the bounds check stays.
	kept := emit(`let get = (xs: [3]u8, i: u8) -> u8 => xs[i]
	let main = () -> u8 => {
	  let arr: [3]u8 = [1, 2, 3]
	  get(arr, 0)
	}`)
	if !strings.Contains(kept, "lyra_panic_index_out_of_bounds") {
		t.Error("a full-range index must keep the bounds check")
	}
}

// Elision changes IR but never semantics — an elided div / index reads the same
// value the checked form would.
func TestExec_DivBoundsElisionPreservesResults(t *testing.T) {
	cases := []struct {
		src  string
		want int
	}{
		{`let main = () -> u8 => {
		  let a: u8 = 84
		  a / 2
		}`, 42},
		{`let main = () -> u8 => {
		  let xs: [3]u8 = [10, 20, 12]
		  var sum: u8 = 0
		  for var i: u8 = 0; i < 3; i += 1 {
		    sum += xs[i]
		  }
		  sum
		}`, 42},
	}
	for _, c := range cases {
		if got := buildAndRun(t, c.src); got != c.want {
			t.Errorf("%q exited %d, want %d", c.src, got, c.want)
		}
	}
}

// Soundness: a divide-by-zero / out-of-bounds the analysis can't rule out still
// traps at runtime — the operand comes through a parameter, so the check stays.
func TestExec_ElisionKeepsRealDivBoundsTrap(t *testing.T) {
	divzero := `let d = (a: u8, b: u8) -> u8 => a / b
	let main = () -> u8 => d(84, 0)`
	if got := buildAndRun(t, divzero); got != trapExitCode {
		t.Errorf("a real divide-by-zero must still trap: exited %d, want %d", got, trapExitCode)
	}

	oob := `let get = (xs: [3]u8, i: u8) -> u8 => xs[i]
	let main = () -> u8 => {
	  let arr: [3]u8 = [1, 2, 3]
	  get(arr, 5)
	}`
	if got := buildAndRun(t, oob); got != trapExitCode {
		t.Errorf("a real out-of-bounds index must still trap: exited %d, want %d", got, trapExitCode)
	}
}

// u64 tracking soundness: the range analysis models a u64's upper bound as +∞ (its
// true max doesn't fit int64). These behavioral tests guard that the fake upper
// never causes a wrong elision — a real u64 overflow still traps, and a provably-
// safe u64 op that IS elided still computes the right value.
func TestExec_U64_ElisionSoundness(t *testing.T) {
	// A full-range u64 subtraction can underflow; passed 5 - 10 it must still trap
	// (the params are opaque, so the analysis can't — and must not — elide the check).
	underflow := `let sub = (a: u64, b: u64) -> u64 => a - b
	let main = () -> u8 => u8(sub(5, 10))`
	if got := buildAndRun(t, underflow); got != trapExitCode {
		t.Errorf("a real u64 underflow must still trap: exited %d, want %d", got, trapExitCode)
	}

	// A u64 subtraction proven safe (a >= b) is elided and computes the same value.
	safe := `let main = () -> u8 => {
	  let a: u64 = 100
	  let b: u64 = 58
	  u8(a - b)
	}`
	if got := buildAndRun(t, safe); got != 42 {
		t.Errorf("a provably-safe u64 subtraction should compute 42, got %d", got)
	}
}

// Match-arm scrutinee refinement drives the same elision as branch refinement: a
// pattern that bounds the index into range elides its bounds check, and this must
// preserve semantics. Here `2` matches the `0..=2` arm and reads xs[2] == 30.
func TestExec_MatchRefinementElisionPreservesResults(t *testing.T) {
	src := `let at = (xs: [3]u8, i: u8) -> u8 => match i {
	  0..=2 => xs[i],
	  _ => 0,
	}
	let main = () -> u8 => {
	  let xs: [3]u8 = [10, 20, 30]
	  at(xs, 2)
	}`
	if got := buildAndRun(t, src); got != 30 {
		t.Errorf("match-refined in-bounds index should read xs[2] == 30, got %d", got)
	}
}
