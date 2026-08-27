package llvm

import "testing"

// `??` — null coalescing (null_coalescing.go). `a ?? b` unwraps a Maybe's payload
// or lazily evaluates the default. It type-checked and failed to lower from the
// day it was collected — the 07/30 `?` shape — so these are the operator's first
// behavioral tests.

const coalescePrelude = `data Maybe<t> = None | Some(t)

let positive = (n: i64) -> Maybe<i64> => if n > 0 { Some(n) } else { None }
`

func TestExec_NullCoalescing(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		main string
		want int
	}{
		{"some yields the payload", `u8(positive(40) ?? 2)`, 40},
		{"none yields the default", `u8(positive(0) ?? 7)`, 7},
		// The default is an expression, not a constant — evaluated on the None path.
		{"computed default", `u8(positive(-3) ?? 5 * 8)`, 40},
		// Chained: the first ?? feeds a value, so the second never fires.
		{"chained", `u8(positive(9) ?? positive(1) ?? 3)`, 9},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			src := coalescePrelude + "let main = () -> u8 => " + c.main + "\n"
			if got := buildAndRun(t, src); got != c.want {
				t.Errorf("exited %d; want %d\n%s", got, c.want, src)
			}
		})
	}
}

// The default is lazy — an arm, not an operand. On the Some path a diverging
// default must never run; on the None path it diverges the whole expression.
// This is the property that rules out lowering both sides eagerly and selecting.
func TestExec_NullCoalescingDefaultIsLazy(t *testing.T) {
	t.Parallel()
	src := coalescePrelude + `let main = () -> u8 => u8(positive(5) ?? panic("unreachable"))` + "\n"
	if got := buildAndRun(t, src); got != 5 {
		t.Errorf("Some path evaluated the diverging default: exited %d; want 5", got)
	}
	trap := coalescePrelude + `let main = () -> u8 => u8(positive(0) ?? panic("missing"))` + "\n"
	stderr, code := buildAndRunPanic(t, trap)
	if code != trapExitCode {
		t.Errorf("None path should reach the diverging default: exited %d, want %d (stderr %q)",
			code, trapExitCode, stderr)
	}
}

// A managed payload: the Some arm's payload is duplicated out of the scrutinee
// (the scrutinee still owns its copy), the None arm's default is an owned
// temporary of its own, and the merged value is released once. ASan plus the
// harness's retain/release conservation check is what verifies the +1s balance
// on both paths.
func TestExec_NullCoalescingManagedPayload_ASan(t *testing.T) {
	t.Parallel()
	src := `data Maybe<t> = None | Some(t)

let pick = (b: bool) -> Maybe<string> => if b { Some("yes" ++ "!") } else { None }

let main = () -> u8 => {
  let a = pick(true) ?? ("no" ++ "?")
  let b = pick(false) ?? ("no" ++ "?")
  if a == "yes!" { if b == "no?" { 3 } else { 2 } } else { 1 }
}
`
	clang := lookClang(t)
	if got := buildAndRun(t, src); got != 3 {
		t.Errorf("exited %d; want 3", got)
	}
	if got := buildAndRunASan(t, clang, src); got != 3 {
		t.Errorf("under ASan: exited %d; want 3", got)
	}
}

// An untyped default narrows to the payload's width — `?? 7` on a Maybe<u8> lowers
// the 7 at u8, which the phi requires. This is the typechecker's
// propagateExpectedType call on the default; without it the literal lowered at the
// i64 default and the phi's incoming types disagreed (invalid IR, loud but wrong).
func TestExec_NullCoalescingNarrowsUntypedDefault(t *testing.T) {
	t.Parallel()
	src := `data Maybe<t> = None | Some(t)

let get = (b: bool) -> Maybe<u8> => if b { Some(200) } else { None }

let main = () -> u8 => get(false) ?? 7
`
	if got := buildAndRun(t, src); got != 7 {
		t.Errorf("exited %d; want 7", got)
	}
	some := `data Maybe<t> = None | Some(t)

let get = (b: bool) -> Maybe<u8> => if b { Some(200) } else { None }

let main = () -> u8 => get(true) ?? 7
`
	if got := buildAndRun(t, some); got != 200 {
		t.Errorf("exited %d; want 200", got)
	}
}
