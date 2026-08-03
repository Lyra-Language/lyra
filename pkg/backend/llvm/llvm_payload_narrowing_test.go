package llvm

import (
	"strings"
	"testing"
)

// A data payload narrowed by an annotation must *lower* at that width, not merely
// type-check at it. The front-end fix (08/03) leaves an untyped payload leaf untyped so a
// context can narrow it, and this package is where getting that wrong shows up: the first
// version deferred concrete-field payloads too, and the backend refused to store an i64
// into a u8 slot. These run the narrowed program rather than inspecting a table, because
// the width has to survive all the way to the store.

func TestExec_NarrowedDataPayloadRoundTrips(t *testing.T) {
	t.Parallel()
	const src = `data Opt<t> = Nil | Just t

let main = () -> u8 => {
  let m: Opt<u8> = Just 42
  match m {
    Just v => v,
    Nil => 0,
  }
}
`
	if got := buildAndRun(t, src); got != 42 {
		t.Errorf("exited %d; want 42", got)
	}
}

// The narrowed payload is genuinely a u8 in the emitted module — a program that
// type-checked at u8 and lowered at i64 would still exit 42 above, so the width itself is
// asserted here.
func TestEmit_NarrowedPayloadUsesTheAnnotatedWidth(t *testing.T) {
	t.Parallel()
	const src = `data Opt<t> = Nil | Just t

let main = () -> u8 => {
  let m: Opt<u8> = Just 42
  match m {
    Just v => v,
    Nil => 0,
  }
}
`
	ir, err := emitSource(t, src)
	if err != nil {
		t.Fatalf("emit: %v", err)
	}
	if strings.Contains(string(ir), "i64 42") {
		t.Errorf("the payload should be stored at u8, not the i64 default:\n%s", ir)
	}
}

// The unannotated form still defaults to i64 and runs — the control, since a fix that
// narrowed everything to the smallest width would pass the tests above.
func TestExec_UnannotatedDataPayloadStillDefaults(t *testing.T) {
	t.Parallel()
	const src = `data Opt<t> = Nil | Just t

let main = () -> u8 => {
  let m = Just 300
  u8(match m {
    Just v => v - 258,
    Nil => 0,
  })
}
`
	if got := buildAndRun(t, src); got != 42 {
		t.Errorf("exited %d; want 42 (300 fits only because the payload defaulted to i64)", got)
	}
}
