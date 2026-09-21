package llvm

import (
	"strings"
	"testing"
)

// **`rotate_left`/`rotate_right`, the fourth integer family** (09/21), and the one whose
// absence a program could feel: `wrapping_*`, `saturating_*` and `checked_*` cover what
// overflow should do, and none of them moves a bit from one end to the other. Every hash,
// every block cipher and every CRC is mostly rotations, and `examples/checksum` wrote
// `(x >> n) | (x << (32 - n))` by hand because there was nothing to call.
//
// **That hand-written line is wrong at one point, and the test says where**: at `n == 0`
// it shifts by the full width, which LLVM and the hardware leave undefined — the case that
// never comes up until a loop's first iteration passes zero. The builtin takes its amount
// modulo the width, so `rotate_right(0)` is the identity and `rotate_right(32)` is too.
//
// The cases are chosen where a rotate is told from a shift: a bit at the end that must
// reappear at the other, a signed receiver (a rotate is bit-level and knows no sign, so a
// negative value's top bit travels like any other), and every width, since the amount's
// modulus is the width and a `u8` rotating by 9 is rotating by 1.
func TestExec_Rotate(t *testing.T) {
	t.Parallel()
	const src = `
module main
let main = () -> void => {
  // The bit that leaves one end arrives at the other, which is the whole difference
  // from a shift: 0x80000001 rotated right by one is 0xC0000000.
  let x: u32 = 0x80000001
  println("${x.rotate_right(1)} ${x.rotate_left(1)} ${x.rotate_right(7)}")
  // Zero and the width itself: both the identity, and both undefined in the
  // shift-and-or form this replaces.
  println("${x.rotate_right(0)} ${x.rotate_left(0)} ${x.rotate_right(32)} ${x.rotate_left(32)}")
  // An amount past the width wraps around it: 33 is 1, and for a u8, 9 is 1.
  let b: u8 = 0b10000001
  println("${x.rotate_right(33)} ${b.rotate_left(1)} ${b.rotate_right(1)} ${b.rotate_left(9)}")
  // Signed receivers rotate their bits, sign included: -2 is 0xFFFFFFFE, whose low zero
  // travels to the top and leaves a positive number one way, and -3 the other. The
  // expected values here were computed independently rather than read off this program.
  let s: i32 = 0 - 2
  println("${s.rotate_right(1)} ${s.rotate_left(1)}")
  // The two directions are each other's inverse at every amount.
  let round_trip = x.rotate_left(13).rotate_right(13) == x
  let u: u64 = 0x0123456789abcdef
  println("${round_trip} ${u.rotate_left(8)} ${u.rotate_right(8)}")
}
`
	want := strings.Join([]string{
		"3221225472 3 50331648",
		"2147483649 2147483649 2147483649 2147483649",
		"3221225472 3 192 3",
		"2147483647 -3",
		"true 2541551405711093505 17222085231038278605",
	}, "\n")
	if got := strings.TrimSpace(buildAndRunWithPrelude(t, src, "")); got != want {
		t.Errorf("rotate =\n%s\nwant\n%s", got, want)
	}
}
