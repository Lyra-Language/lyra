package llvm

import (
	"strings"
	"testing"
)

// Fixed-size arrays (`[N]T`): construction lowers to an LLVM `[N x T]` aggregate
// value (insertvalue over undef, like a tuple), and indexing reads an element —
// `extractvalue` for a compile-time-constant index, or a bounds-checked
// getelementptr+load for a runtime index. Arrays round-trip through `let`
// bindings, function params/args, and returns as by-value aggregates.

func TestExec_StaticArray(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want int
	}{
		{"constant index sum", `let main = () -> u8 => {
		   let xs: [3]u8 = [10, 20, 30]
		   xs[0] + xs[2]
		 }`, 40},
		{"runtime index loop sum", `let main = () -> u8 => {
		   let xs: [3]u8 = [10, 20, 30]
		   var sum: u8 = 0
		   for var i = 0; i < 3; i += 1 {
		     sum += xs[i]
		   }
		   sum
		 }`, 60},
		{"array param and arg, indexed by a param", `let at = (xs: [3]u8, i: i64) -> u8 => xs[i]
		 let main = () -> u8 => {
		   let arr: [3]u8 = [10, 20, 30]
		   at(arr, 1)
		 }`, 20},
		{"array returned from a function", `let make = () -> [3]u8 => [4, 5, 6]
		 let main = () -> u8 => {
		   let xs = make()
		   xs[0] + xs[1] + xs[2]
		 }`, 15},
		{"wider element type (i32)", `let main = () -> u8 => {
		   let xs: [4]i32 = [1000, 2000, 3000, 60]
		   u8(xs[3])
		 }`, 60},
		{"no-annotation array (i64 default element)", `let main = () -> u8 => {
		   let xs = [100, 20, 30]
		   u8(xs[0])
		 }`, 100},
		{"bool array, runtime index", `let main = () -> u8 => {
		   let flags: [2]bool = [true, false]
		   var i: i64 = 0
		   if flags[i] { 1 } else { 0 }
		 }`, 1},
	}
	for _, c := range cases {
		if got := buildAndRun(t, c.src); got != c.want {
			t.Errorf("%s: exited %d, want %d", c.name, got, c.want)
		}
	}
}

// A runtime index outside [0, N) traps (Pit-of-Success: an out-of-bounds read
// aborts rather than reading past the array). The index goes through a function
// parameter so the typechecker can't fold it to a constant and reject it at
// compile time — that path is checked separately (inferIndexExpr). A negative
// index traps too: the signed value widens to a large unsigned, caught by the
// same unsigned `>= size` check (`i` is out of range only past *both* ends —
// `i >= size` or `i < -size`, since a negative index counts from the end).
func TestExec_ArrayBoundsCheck(t *testing.T) {
	trap := []struct {
		name string
		src  string
	}{
		{"index past the end", `let at = (xs: [3]u8, i: i64) -> u8 => xs[i]
		 let main = () -> u8 => at([10, 20, 30], 5)`},
		{"negative index past the start", `let at = (xs: [3]u8, i: i64) -> u8 => xs[i]
		 let main = () -> u8 => at([10, 20, 30], -4)`},
	}
	for _, c := range trap {
		if got := buildAndRun(t, c.src); got != trapExitCode {
			t.Errorf("%s: exited %d, want %d (trap)", c.name, got, trapExitCode)
		}
	}

	// The in-bounds boundaries (last positive index and -size) do not trap.
	for _, c := range []struct {
		src  string
		want int
	}{
		{`let at = (xs: [3]u8, i: i64) -> u8 => xs[i]
		  let main = () -> u8 => at([7, 8, 9], 2)`, 9},
		{`let at = (xs: [3]u8, i: i64) -> u8 => xs[i]
		  let main = () -> u8 => at([7, 8, 9], -3)`, 7},
	} {
		if got := buildAndRun(t, c.src); got != c.want {
			t.Errorf("boundary index: exited %d, want %d", got, c.want)
		}
	}
}

// A negative index counts from the end (Python-style): -1 is the last element,
// -2 the second-to-last, down to -size for the first. Works for a runtime index
// (through a param) and a constant one (a `-1` literal is a NegationExpr, so it
// takes the runtime path — the typechecker range-checks a constant negative).
func TestExec_NegativeArrayIndex(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want int
	}{
		{"runtime -1 is the last element", `let at = (xs: [4]u8, i: i64) -> u8 => xs[i]
		 let main = () -> u8 => at([10, 20, 30, 40], -1)`, 40},
		{"runtime -2 is second-to-last", `let at = (xs: [4]u8, i: i64) -> u8 => xs[i]
		 let main = () -> u8 => at([10, 20, 30, 40], -2)`, 30},
		{"runtime -size is the first element", `let at = (xs: [4]u8, i: i64) -> u8 => xs[i]
		 let main = () -> u8 => at([10, 20, 30, 40], -4)`, 10},
		{"constant -1 literal", `let main = () -> u8 => {
		   let xs: [3]u8 = [10, 20, 30]
		   xs[-1]
		 }`, 30},
	}
	for _, c := range cases {
		if got := buildAndRun(t, c.src); got != c.want {
			t.Errorf("%s: exited %d, want %d", c.name, got, c.want)
		}
	}
}

// TestEmit_ArrayIR pins the lowering shapes: construction is insertvalue into an
// [N x T]; a constant index is a bare extractvalue with no bounds trap; a runtime
// index is a bounds compare + trap + getelementptr; the OOB trap is defined once.
func TestEmit_ArrayIR(t *testing.T) {
	emit := func(src string) string {
		out, err := emitSource(t, src)
		if err != nil {
			t.Fatalf("emit: %v", err)
		}
		return out
	}

	// Construction + a constant index: [3 x i8], insertvalue, extractvalue, no trap.
	constIdx := emit(`let main = () -> u8 => {
	   let xs: [3]u8 = [10, 20, 30]
	   xs[0]
	 }`)
	if !strings.Contains(constIdx, "[3 x i8]") {
		t.Error("expected a [3 x i8] array type")
	}
	if !strings.Contains(constIdx, "insertvalue [3 x i8]") {
		t.Error("expected insertvalue array construction")
	}
	if !strings.Contains(constIdx, "extractvalue") {
		t.Error("expected a constant index to use extractvalue")
	}
	if strings.Contains(constIdx, "@lyra_panic_index_out_of_bounds") {
		t.Error("a constant index (already range-checked by the typechecker) must not emit a bounds trap")
	}

	// A runtime index: bounds compare + trap call + getelementptr + load.
	runtimeIdx := emit(`let at = (xs: [3]u8, i: i64) -> u8 => xs[i]
	 let main = () -> u8 => at([1, 2, 3], 0)`)
	if !strings.Contains(runtimeIdx, "@lyra_panic_index_out_of_bounds") {
		t.Error("a runtime index must guard against out-of-bounds")
	}
	if !strings.Contains(runtimeIdx, "getelementptr") {
		t.Error("a runtime index must getelementptr into the array")
	}
	if n := strings.Count(runtimeIdx, "define void @lyra_panic_index_out_of_bounds"); n != 1 {
		t.Errorf("want one bounds-trap definition, got %d", n)
	}

	// An array-returning function's signature and return match the array type.
	arrayRet := emit(`let make = () -> [3]u8 => [4, 5, 6]
	 let main = () -> u8 => make()[0]`)
	if !strings.Contains(arrayRet, "define [3 x i8] @make()") {
		t.Error("expected @make to return [3 x i8]")
	}
	if !strings.Contains(arrayRet, "ret [3 x i8]") {
		t.Error("expected a [3 x i8] return")
	}
}
