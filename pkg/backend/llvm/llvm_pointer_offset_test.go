package llvm

import "testing"

// `p.offset(n)` — the one form of pointer arithmetic the language has. What is built on
// it lives in llvm_ffi_test.go: a raw pointer carries no length so nothing about it can
// be checked, and `std.ffi` is where a length joins one and the checking becomes possible.

// **Elements, not bytes**, which is the decision most easily got wrong and least easily
// noticed: on a `^i64` the scaling is by 8, so a byte-offset lowering answers garbage
// from inside the first element rather than failing. Chosen to match the rest of the
// language — a string is rune-indexed so `len` and `[i]` agree about the unit.
func TestExec_PointerOffsetIsInElements(t *testing.T) {
	t.Parallel()
	src := `module main
let main = () -> u8 => {
  var xs: []i64 = [10, 20, 30, 40]
  unsafe {
    let p = &xs[0]
    u8(p.offset(2)^)
  }
}
`
	if got := buildAndRun(t, src); got != 30 {
		t.Errorf("p.offset(2)^ on []i64 exited %d; want 30 (elements, not bytes)", got)
	}
}

// The write direction. `^` stays the only load and the only store, so `offset` composes
// with both rather than adding a second way to reach memory.
func TestExec_PointerOffsetWrites(t *testing.T) {
	t.Parallel()
	src := `module main
let main = () -> u8 => {
  var xs: []u8 = [1, 2, 3, 4]
  unsafe {
    let p = &mut xs[0]
    p.offset(3)^ = 42
  }
  xs[3]
}
`
	if got := buildAndRun(t, src); got != 42 {
		t.Errorf("p.offset(3)^ = 42 left xs[3] = %d; want 42", got)
	}
}

// Signed, because a negative offset is meaningful in C and refusing it buys nothing when
// nothing here is checked anyway.
func TestExec_PointerOffsetAcceptsANegativeIndex(t *testing.T) {
	t.Parallel()
	src := `module main
let main = () -> u8 => {
  var xs: []u8 = [5, 6, 7]
  unsafe {
    let p = &xs[2]
    u8(p.offset(-2)^)
  }
}
`
	if got := buildAndRun(t, src); got != 5 {
		t.Errorf("p.offset(-2)^ from xs[2] exited %d; want 5", got)
	}
}
