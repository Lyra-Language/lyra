package llvm

import (
	"os/exec"
	"testing"
)

// Reassigning a parameter binding (`n = n + 1`) lowers. Only `own` and `mut`
// parameters may be reassigned at all — a borrowed one is lyra-E025, since the write
// could never reach the caller (see TestTypeCheck_ParamReassign_Borrowed_Error) — so
// these cases use `own`, whose by-value copy is the callee's to rebind. It used to fail the build
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
			"self-referential arithmetic", `let inc = (n: own i64) -> i64 => { n = n + 1; n }
let main = () -> u8 => u8(inc(41))`, 42,
		},
		{
			"reads another parameter", `let acc = (n: i64, k: own i64) -> i64 => { k = n * 2; k = k + 1; k }
let main = () -> u8 => u8(acc(10, 0))`, 21,
		},
		{
			"narrow width", `let inc = (n: own u8) -> u8 => { n = n + 1; n }
let main = () -> u8 => inc(7)`, 8,
		},
		{
			// The float path always worked (no signedness lookup); pinned so the
			// shared fix doesn't regress it.
			"float", `let f = (x: own f64) -> f64 => { x = x + 1.5; x }
let main = () -> u8 => u8(f(1.5).floor())`, 3,
		},
		{
			// An `own` parameter's binding is the callee's own storage.
			"own parameter", `let f = (n: own i64) -> i64 => { n = n + 2; n }
let main = () -> u8 => u8(f(40))`, 42,
		},
		{
			// An `own` parameter is passed by value, so rebinding it cannot reach the
			// caller — which is exactly why `own` may be reassigned and a borrow may
			// not: the caller gave the value away, so it has nothing left to be
			// surprised about.
			"own rebind does not reach the caller", `let bump = (n: own i64) -> void => { n = 99 }
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
// a reference to the new one, on both sides of the convention: an `own string`
// parameter (the callee's own copy, transferred to it) and a by-reference `mut string` (the caller's
// slot, written through twice here so a missed release would show as a leak and a
// double release as a use-after-free).
func TestExec_ParamReassignment_ManagedIsLeakFree(t *testing.T) {
	t.Parallel()
	src := `let setStr = (s: mut string) -> void => { s = "n" ++ "1" }
let localStr = (s: own string) -> string => { s = "l" ++ "1"; s }
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

// `own` and `mut` are the two modes a parameter may be reassigned in, and they release
// the overwritten value for different reasons: an `own` parameter owns its copy
// outright, while a `mut` parameter's slot *is* the caller's storage.
//
// Until 07/30 the release happened for a *borrowed* parameter too, whose copy shares
// the caller's reference — freeing a value the caller would release again
// (ASan-confirmed heap-use-after-free). That is now unreachable from source, because
// reassigning a borrow is lyra-E025; the backend keeps the `slotIsOwning` guard as
// defense in depth, on the standing rule that it errors or does nothing rather than
// emitting a wrong release.
func TestExec_OwningParamReassignment_ReleasesExactlyOnce(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		src  string
		want int
	}{
		{
			// The shape that was the use-after-free, now with the mode that makes it
			// legal: the caller transferred the value, so the callee may free it.
			"own string parameter reassigned",
			`let localStr = (s: own string) -> string => { s = "l" ++ "1"; s }
			 let main = () -> u8 => if localStr("z" ++ "z") == "l1" { 0 } else { 1 }`,
			0,
		},
		{
			// `mut` is by reference: the write must reach the caller, and the
			// overwritten value must genuinely be released (twice over, here).
			"mut string parameter writes through and releases",
			`let setStr = (s: mut string) -> void => { s = "n" ++ "1" }
			 let main = () -> u8 => {
			   var t = "a" ++ "b"
			   setStr(t)
			   setStr(t)
			   if t == "n1" { 0 } else { 1 }
			 }`,
			0,
		},
		{
			// A borrowed parameter is untouched by the callee, so the caller's value
			// survives the call intact.
			"a borrow leaves the caller's value intact",
			`let peek = (s: string) -> i64 => if s == "ab" { 1 } else { 0 }
			 let main = () -> u8 => {
			   let keep = "a" ++ "b"
			   let seen = peek(keep)
			   if keep == "ab" { 0 } else { 1 }
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
