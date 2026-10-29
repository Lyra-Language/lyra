package llvm

import (
	"strings"
	"testing"
)

// An array type sized by a named `const` is the array of that size, in every position a
// type is written: a `mut` parameter, a return type, a local, a struct field, a payload
// and a callback's parameter — and through a `const` naming another. A function's own
// `const N` shadows the module's `N` (driver's foldConstArraySizes, 10/29).
func TestExec_ConstSizedArrays(t *testing.T) {
	t.Parallel()
	got := strings.TrimSpace(buildAndRunWithPrelude(t, `module main
const MAX_BYTES: i64 = 4
const ALSO = MAX_BYTES
const N = 9

struct Buf {
  bytes: [ALSO]u8
}

data Packet = Small([2]u8) | Big([MAX_BYTES]u16)

let fill = (out: mut [MAX_BYTES]u8) -> void => {
  out[3] = 7
}

let make = pure () -> [MAX_BYTES]u8 => #[1; MAX_BYTES]

let first = pure (xs: [MAX_BYTES]u8, f: ([MAX_BYTES]u8) -> u8) -> u8 => f(xs)

// N here is the parameter, not the module's const.
let total<const N: i64> = pure (xs: ref [N]i64) -> i64 => {
  var s = 0
  for x in xs { s += x }
  s
}

let main = () -> void => {
  var buf: [MAX_BYTES]u8 = #[0; MAX_BYTES]
  fill(buf)
  let b = Buf { bytes: make() }
  let n = match Big(#[5; 4]) {
    Small(_) => 0,
    Big(xs) => xs.len(),
  }
  let nine: [N]u8 = #[0; 9]
  let head = first(buf, (xs: [MAX_BYTES]u8) -> u8 => xs[3])
  println("${buf[3]} ${b.bytes.len()} ${b.bytes[3]} ${n} ${total(#[1, 2, 3])} ${nine.len()} ${head}")
}
`, ""))
	if want := "7 4 1 4 6 9 7"; got != want {
		t.Errorf("got %q; want %q", got, want)
	}
}

// A `const` another module exports sizes an array here as it does there — the folding asks
// the module the type is written in what the name reaches.
func TestExec_ImportedConstSizesAnArray(t *testing.T) {
	t.Parallel()
	code := buildAndRunModules(t, map[string]string{
		"sizes.lyra": `module sizes
pub const WIDTH: i64 = 3

pub let zeros = pure () -> [WIDTH]u8 => #[0; WIDTH]
`,
		"app.lyra": `import sizes.{ WIDTH, zeros }

let main = () -> u8 => {
  var row: [WIDTH]u8 = zeros()
  row[2] = 40
  if row.len() == 3 { row[2] + 2 } else { 0 }
}
`,
	})
	if code != 42 {
		t.Errorf("exit code %d; want 42", code)
	}
}
