package llvm

import (
	"os/exec"
	"testing"
)

// The ownership pass must recurse into *arithmetic* operands. Arithmetic itself
// never owns a managed value — its operands are numbers — but a managed value can
// sit inside one: `consume(p.name) + 1` passes a managed field to an `own`
// parameter, which is an owning position that needs a retain.
//
// MathBinaryOpExpr/MathAssignOpExpr/NegationExpr used to fall through to the
// pass's default case and record nothing, justified as safe because "a missed
// release only leaks". That premise was wrong in the same way the stack-aggregate
// use-after-free was: a missed *retain* at an owning position dangles rather than
// leaks — the callee released a reference the caller never granted, so the
// struct's own drop then freed an already-freed box.

// The reduced repro: without the retain this aborts under ASan with
// heap-use-after-free on the read of p.name (and silently double-releases even
// when nothing reads it).
func TestExec_OwnershipArithmetic_ManagedFieldIntoOwnParam(t *testing.T) {
	t.Parallel()
	src := `struct Person { name: string }
let consume = (s: own string) -> i64 => 1
let main = () -> u8 => {
  let p = Person { name: "x" ++ "y" }
  let n = consume(p.name) + 1
  if p.name == "xy" { u8(n) } else { 99 }
}`
	if got := buildAndRun(t, src); got != 2 {
		t.Fatalf("exited %d; want 2 (the field must survive the call)", got)
	}
	clang, err := exec.LookPath("clang")
	if err != nil || !asanAvailable(t, clang) {
		t.Skip("ASan runtime not available; ran without it")
	}
	if got := buildAndRunASan(t, clang, src); got != 2 {
		t.Errorf("under ASan: exited %d; want 2 (heap-use-after-free on the freed field)", got)
	}
}

// Every arithmetic form that can carry an owning position beneath it: a binary
// operator, a compound assignment, and a unary negation. Each consumes a managed
// field, then all three are read back.
func TestExec_OwnershipArithmetic_AllForms(t *testing.T) {
	t.Parallel()
	src := `struct Person { name: string }
let consume = (s: own string) -> i64 => 1
let main = () -> u8 => {
  let p = Person { name: "a" ++ "b" }
  var total = 0
  total += consume(p.name)
  let q = Person { name: "c" ++ "d" }
  let neg = -consume(q.name)
  let r = Person { name: "e" ++ "f" }
  let nested = consume(r.name) + 1 * 2
  if p.name == "ab" && q.name == "cd" && r.name == "ef" {
    u8(total + nested - neg)
  } else { 99 }
}`
	if got := buildAndRun(t, src); got != 5 {
		t.Fatalf("exited %d; want 5", got)
	}
	clang, err := exec.LookPath("clang")
	if err != nil || !asanAvailable(t, clang) {
		t.Skip("ASan runtime not available; ran without it")
	}
	if got := buildAndRunASan(t, clang, src); got != 5 {
		t.Errorf("under ASan: exited %d; want 5", got)
	}
}

// A whole managed *binding* (not a field) consumed inside arithmetic: here the
// last-use machinery transfers rather than retains, which happened to keep this
// shape working even before the fix — pinned so the recursion doesn't turn a
// transfer into a double retain.
func TestExec_OwnershipArithmetic_BindingTransfersOnce(t *testing.T) {
	t.Parallel()
	src := `let consume = (s: own string) -> i64 => 1
let main = () -> u8 => {
  let a = "x" ++ "y"
  let n = consume(a) + 1
  u8(n)
}`
	if got := buildAndRun(t, src); got != 2 {
		t.Fatalf("exited %d; want 2", got)
	}
	clang, err := exec.LookPath("clang")
	if err != nil || !asanAvailable(t, clang) {
		t.Skip("ASan runtime not available; ran without it")
	}
	if got := buildAndRunASan(t, clang, src); got != 2 {
		t.Errorf("under ASan: exited %d; want 2", got)
	}
}
