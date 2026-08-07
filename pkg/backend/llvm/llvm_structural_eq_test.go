package llvm

import (
	"strings"
	"testing"
)

// Structural `==` on an aggregate. The typechecker has always accepted it and the
// backend always refused — "comparison of non-integer operands not implemented" — which
// is hazard 5 inverted, and the third instance of that shape dug out this week after the
// type-name member call and the `where`-bound call.
//
// Equality is field-wise and total. A type wanting different equality implements the
// prelude's `Eq`, which is dispatched before this is reached.

// Struct, tuple, and the string field that makes it more than an integer memcmp.
func TestExec_StructuralEqualityOnStructsAndTuples(t *testing.T) {
	t.Parallel()
	const src = `
module main
struct Pt { x: i64, y: string }
let main = () -> void => {
  let a = Pt { x: 1, y: "n" };
  let b = Pt { x: 1, y: "n" };
  let c = Pt { x: 2, y: "n" };
  let d = Pt { x: 1, y: "m" };
  println("${a == b} ${a == c} ${a == d} ${a != c}");
  println("${(1, "a") == (1, "a")} ${(1, "a") == (2, "a")}");
}
`
	want := "true false false true\ntrue false"
	if got := strings.TrimSpace(buildAndRunWithPrelude(t, src, "")); got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

// A `data` value compares its tag and then that variant's payload — the case that needs
// a branch, and the reason equality is emitted as a per-type function rather than
// inlined: a branching call site hands back a merge block the pending-temporaries
// machinery does not handle.
func TestExec_StructuralEqualityOnDataValues(t *testing.T) {
	t.Parallel()
	const src = `
module main
data Shape = Circle(i64) | Rect(i64, i64) | Dot
let main = () -> void => {
  println("${Circle(1) == Circle(1)} ${Circle(1) == Circle(2)} ${Circle(1) == Dot} ${Dot == Dot}");
  println("${Rect(1, 2) == Rect(1, 2)} ${Rect(1, 2) == Rect(1, 3)}");
}
`
	want := "true false false true\ntrue false"
	if got := strings.TrimSpace(buildAndRunWithPrelude(t, src, "")); got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

// Nesting and inline arrays: the recursion has to reach a struct inside a struct and
// every element of a `[N]T`.
func TestExec_StructuralEqualityNestsAndCoversArrays(t *testing.T) {
	t.Parallel()
	const src = `
module main
struct Inner { v: i64 }
struct Outer { a: Inner, b: [2]i64 }
let main = () -> void => {
  let p = Outer { a: Inner { v: 1 }, b: [1, 2] };
  let q = Outer { a: Inner { v: 1 }, b: [1, 2] };
  let r = Outer { a: Inner { v: 1 }, b: [1, 3] };
  let s = Outer { a: Inner { v: 9 }, b: [1, 2] };
  println("${p == q} ${p == r} ${p == s} ${[1,2,3] == [1,2,3]} ${[1,2,3] == [1,9,3]}");
}
`
	if got := strings.TrimSpace(buildAndRunWithPrelude(t, src, "")); got != "true false false true false" {
		t.Errorf("got %q; want \"true false false true false\"", got)
	}
}

// The float-equality warning did not survive substitution: a direct `f64 == f64` warned
// and the identical comparison through a type variable did not, though both do IEEE
// equality and both answer false for `0.1 + 0.2 == 0.3`. Genericity silently stripped
// the safety net, which is worse than never having had one — the check looks present and
// is not.
//
// Reported at the *call*, because the comparison is correct where it is written: `t` is
// not a float there, and the body is sensible at every other type.
func TestCheck_FloatEqualityWarnsThroughAGenericInstantiation(t *testing.T) {
	t.Parallel()
	const src = `
module main
let same<t> = (a: t, b: t) -> bool => a == b
let main = () -> void => {
  println("${same(1, 2)}");
  println("${same("a", "b")}");
  println("${same(1.0, 2.0)}");
}
`
	diags := checkWithPreludeDiagnostics(t, src)
	var floatWarnings int
	for _, d := range diags {
		if strings.Contains(d, "floating-point precision") {
			floatWarnings++
		}
	}
	if floatWarnings != 1 {
		t.Errorf("want exactly one float-equality warning (the f64 call), got %d: %v", floatWarnings, diags)
	}
}
