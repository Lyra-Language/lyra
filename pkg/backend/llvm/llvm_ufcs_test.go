package llvm

import (
	"testing"
)

// UFCS lowering — of which there is none, and that is the claim under test.
//
// `m.unwrap(0)` is rewritten by the typechecker into `unwrap(m, 0)` before this package
// sees it, so the backend lowers an ordinary direct call and needed no change at all. A
// claim of "nothing to do" is only worth as much as the run that demonstrates it, and the
// generic case is the one with something to get wrong: the specialization is keyed by the
// call node, which the rewrite has to leave intact.

func TestExec_UFCSCallRuns(t *testing.T) {
	t.Parallel()
	const src = `let scaled = pure (self: u8, by: u8) -> u8 => self * by

let main = () -> u8 => {
  let x: u8 = 21
  x.scaled(2)
}
`
	if got := buildAndRun(t, src); got != 42 {
		t.Errorf("exited %d; want 42", got)
	}
}

// A generic receiver monomorphizes through the same path a written-out call takes.
func TestExec_UFCSGenericReceiver(t *testing.T) {
	t.Parallel()
	const src = `data Opt<t> = Nil | Just t

let unwrap<t> = pure (self: Opt<t>, fallback: t) -> t => match self {
  Just v => v,
  Nil => fallback,
}

let main = () -> u8 => {
  let m: Opt<i64> = Just 7
  let n: Opt<i64> = Nil
  u8(m.unwrap(0) + n.unwrap(35))
}
`
	if got := buildAndRun(t, src); got != 42 {
		t.Errorf("exited %d; want 42 (7 from the Just, 35 from the Nil's fallback)", got)
	}
}

// The two spellings emit **identical IR**, which is the strongest form of "the backend
// does not know about UFCS": if the rewrite left anything behind — a different callee, a
// re-ordered argument, a missing instantiation — the modules would diverge.
func TestEmit_UFCSAndCallFormAreIdentical(t *testing.T) {
	t.Parallel()
	const method = `data Opt<t> = Nil | Just t

let unwrap<t> = pure (self: Opt<t>, fallback: t) -> t => match self {
  Just v => v,
  Nil => fallback,
}

let main = () -> u8 => {
  let m: Opt<i64> = Just 7
  u8(m.unwrap(0))
}
`
	const direct = `data Opt<t> = Nil | Just t

let unwrap<t> = pure (self: Opt<t>, fallback: t) -> t => match self {
  Just v => v,
  Nil => fallback,
}

let main = () -> u8 => {
  let m: Opt<i64> = Just 7
  u8(unwrap(m, 0))
}
`
	methodIR, err := emitSource(t, method)
	if err != nil {
		t.Fatalf("emit method form: %v", err)
	}
	directIR, err := emitSource(t, direct)
	if err != nil {
		t.Fatalf("emit call form: %v", err)
	}
	if string(methodIR) != string(directIR) {
		t.Errorf("the two spellings must emit the same module\n--- method form ---\n%s\n--- call form ---\n%s",
			methodIR, directIR)
	}
}
