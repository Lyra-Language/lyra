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
