package llvm

import (
	"os/exec"
	"testing"
)

// Reassigning a parameter binding (`n = n + 1`) lowers. It used to fail the build
// with "type not found for *ast.IdentifierExpr": the typechecker skipped the whole
// statement when the target was a parameter, so the RHS was never inferred and its
// subexpressions had no recorded types for getIntSignedness to read. Only integer
// arithmetic tripped it — a bare literal RHS needs no recorded type, and the float
// path doesn't consult signedness — which is why it hid for so long.

func TestExec_ParamReassignment(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		src  string
		want int
	}{
		{
			"self-referential arithmetic", `let inc = (n: i64) -> i64 => { n = n + 1  n }
let main = () -> u8 => u8(inc(41))`, 42,
		},
		{
			"reads another parameter", `let acc = (n: i64, k: i64) -> i64 => { k = n * 2  k = k + 1  k }
let main = () -> u8 => u8(acc(10, 0))`, 21,
		},
		{
			"narrow width", `let inc = (n: u8) -> u8 => { n = n + 1  n }
let main = () -> u8 => inc(7)`, 8,
		},
		{
			// The float path always worked (no signedness lookup); pinned so the
			// shared fix doesn't regress it.
			"float", `let f = (x: f64) -> f64 => { x = x + 1.5  x }
let main = () -> u8 => u8(f(1.5).floor())`, 3,
		},
		{
			// An `own` parameter's binding is the callee's own storage.
			"own parameter", `let f = (n: own i64) -> i64 => { n = n + 2  n }
let main = () -> u8 => u8(f(40))`, 42,
		},
		{
			// A by-value parameter rebinds only the callee's copy — the caller is
			// unaffected. (A `mut` scalar stays by value; lyra-W010 warns that the
			// modifier is inert there, so the split is diagnosed rather than silent.)
			"by-value rebind does not reach the caller", `let bump = (n: i64) -> void => { n = 99 }
let main = () -> u8 => {
  var k = 5
  bump(k)
  u8(k)
}`, 5,
		},
		{
			// Through a by-reference `mut` parameter, a whole-binding reassignment
			// *does* reach the caller — that is what the modifier means.
			"mut aggregate rebind reaches the caller", `struct Pt { x: i64 }
let replace = (p: mut Pt) -> void => { p = Pt { x: 42 } }
let main = () -> u8 => {
  var q = Pt { x: 1 }
  replace(q)
  u8(q.x)
}`, 42,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if got := buildAndRun(t, c.src); got != c.want {
				t.Errorf("exited %d; want %d", got, c.want)
			}
		})
	}
}

// Reassigning a *managed* parameter releases the value being overwritten and takes
// a reference to the new one, on both sides of the convention: a by-value `string`
// parameter (the callee's own copy) and a by-reference `mut string` (the caller's
// slot, written through twice here so a missed release would show as a leak and a
// double release as a use-after-free).
func TestExec_ParamReassignment_ManagedIsLeakFree(t *testing.T) {
	t.Parallel()
	src := `let setStr = (s: mut string) -> void => { s = "n" ++ "1" }
let localStr = (s: string) -> string => { s = "l" ++ "1"  s }
let main = () -> u8 => {
  var t = "a" ++ "b"
  setStr(t)
  setStr(t)
  if t == "n1" {
    if localStr("z" ++ "z") == "l1" { 0 } else { 1 }
  } else { 1 }
}`
	if got := buildAndRun(t, src); got != 0 {
		t.Fatalf("exited %d; want 0", got)
	}
	clang, err := exec.LookPath("clang")
	if err != nil || !asanAvailable(t, clang) {
		t.Skip("ASan runtime not available; ran without it")
	}
	if got := buildAndRunASan(t, clang, src); got != 0 {
		t.Errorf("under ASan: exited %d; want 0", got)
	}
}

// Reassigning a *borrowed* parameter must not release the value it held.
//
// A by-value `string` parameter is a borrow: the callee's copy shares the caller's
// reference, and the caller is still the owner. Releasing through it therefore frees a
// value the caller will release again — a heap-use-after-free, not a leak. This was
// live until 07/30 (`lowerVarReassignment` released whenever the type needed a drop,
// without asking whether the binding owned it), and the reason it survived is that the
// ASan suite was not instrumenting memory accesses at all; the moment it was, this
// aborted. See slotIsOwning.
//
// The contrast case is in the same program: `mut` is by reference, so its slot *is* the
// caller's owning storage and writing through it *should* release. Both conventions in
// one test, because the bug was picking the wrong rule for one of them.
func TestExec_BorrowedParamReassignment_NoUseAfterFree(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		src  string
		want int
	}{
		{
			// The minimal fault: the argument is an owned temporary the caller
			// releases after the call, and the callee overwrote (and freed) it first.
			"by-value string parameter reassigned",
			`let localStr = (s: string) -> string => { s = "l" ++ "1"  s }
			 let main = () -> u8 => if localStr("z" ++ "z") == "l1" { 0 } else { 1 }`,
			0,
		},
		{
			// The caller's binding outlives the call and is read afterwards, so a
			// premature free shows up as a corrupted read rather than only under ASan.
			"borrowed reassignment leaves the caller's value intact",
			`let shadow = (s: string) -> i64 => { s = "x" ++ "y"  0 }
			 let main = () -> u8 => {
			   let keep = "a" ++ "b"
			   let ignored = shadow(keep)
			   if keep == "ab" { 0 } else { 1 }
			 }`,
			0,
		},
		{
			// `mut` is by reference: the write must reach the caller, and the
			// overwritten value must genuinely be released (twice over, here).
			"mut string parameter still writes through and releases",
			`let setStr = (s: mut string) -> void => { s = "n" ++ "1" }
			 let main = () -> u8 => {
			   var t = "a" ++ "b"
			   setStr(t)
			   setStr(t)
			   if t == "n1" { 0 } else { 1 }
			 }`,
			0,
		},
	}
	clang := lookClang(t)
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if got := buildAndRun(t, c.src); got != c.want {
				t.Errorf("exited %d; want %d", got, c.want)
			}
			if got := buildAndRunASan(t, clang, c.src); got != c.want {
				t.Errorf("under ASan: exited %d; want %d", got, c.want)
			}
		})
	}
}
