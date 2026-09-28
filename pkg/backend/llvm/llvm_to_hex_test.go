package llvm

import "testing"

// `to_hex` on the unsigned integers, beside the byte-array one under the same name
// (receiver-keyed overloading): lowercase, no prefix, at least `width` digits. Zero is "0",
// not "", and a width only ever pads — a wider value keeps every digit. Added 09/28 for
// Sheliak's test runner, which had written its own.
func TestExec_IntegerToHex(t *testing.T) {
	t.Parallel()
	src := `let main = () -> u8 => {
  let pc: u32 = 0x2700
  let b: u8 = 255
  let zero: u16 = 0
  let big: u64 = 0xFFFFFFFFFFFFFFFF
  let bytes: []u8 = [1, 171]
  println(pc.to_hex())
  println(pc.to_hex(8))
  println(b.to_hex())
  println(zero.to_hex())
  println(zero.to_hex(4))
  println(big.to_hex())
  println(u32(0x12345).to_hex(2))
  println(bytes.to_hex())
  0
}`
	want := "2700\n00002700\nff\n0\n0000\nffffffffffffffff\n12345\n01ab\n"
	if got := buildAndRunWithPrelude(t, src, ""); got != want {
		t.Errorf("printed %q; want %q", got, want)
	}
}
