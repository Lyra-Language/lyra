package llvm

import (
	"regexp"
	"testing"
)

// **A large aggregate moves through memory, never as a whole value** (aggregate_copies.go):
// a copy, a record update, a value stored twice and a field read from a whole load leave no
// load or store of the whole struct in the IR — which LLVM splits into one access a field,
// and was 82 s of compiling a part of Vega's checks (10/30) — and the program still means
// what it says, under ASan, which sees a copy made after the memory it read was freed.
func TestEmit_LargeAggregatesMoveThroughMemory(t *testing.T) {
	t.Parallel()
	const src = `module main
struct Big {
  name: string,
  tag: string = "t",
  a0: i64 = 0,
  a1: i64 = 1,
  a2: i64 = 2,
  a3: i64 = 3,
  a4: i64 = 4,
  a5: i64 = 5,
  a6: i64 = 6,
  a7: i64 = 7,
  a8: i64 = 8,
  a9: i64 = 9,
  a10: i64 = 10,
  a11: i64 = 11,
  a12: i64 = 12,
  a13: i64 = 13,
  a14: i64 = 14,
  a15: i64 = 15,
  a16: i64 = 16,
  a17: i64 = 17,
  a18: i64 = 18,
  a19: i64 = 19,
  a20: i64 = 20,
  a21: i64 = 21,
  a22: i64 = 22,
  a23: i64 = 23,
  a24: i64 = 24,
  a25: i64 = 25,
  a26: i64 = 26,
  a27: i64 = 27,
  a28: i64 = 28,
  a29: i64 = 29,
  a30: i64 = 30,
  a31: i64 = 31,
  a32: i64 = 32,
  a33: i64 = 33,
  a34: i64 = 34,
  a35: i64 = 35,
  a36: i64 = 36,
  a37: i64 = 37,
  a38: i64 = 38,
  a39: i64 = 39,
}
let make = (n: string) -> Big => Big { name: n }
let renamed = (b: Big, n: string) -> Big => Big { b | name: n }
let main = () -> u8 => {
  var b = make("first")
  let c = b
  b = renamed(b, "second")
  let d = Big { c | a5: 70 }
  let xs: []Big = [b, c, d]
  var last = xs[0]
  for x in xs { last = x }
  if b.name == "second" && c.name == "first" && d.a5 == 70 && d.name == "first" && c.a5 == 5 && last.a39 == 39 && last.a5 == 70 && xs[1].tag == "t" { 3 } else { 1 }
}
`
	ir := emitWithPrelude(t, src)
	if whole := regexp.MustCompile(`(load %main__Big,|store %main__Big %)`).FindAllString(ir, -1); len(whole) > 0 {
		t.Errorf("%d whole loads and stores of a 336-byte struct are left; want none", len(whole))
	}
	if got := buildAndRunASanWithPrelude(t, src); got != 3 {
		t.Errorf("under ASan: exited %d; want 3", got)
	}
}
