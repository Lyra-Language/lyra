package llvm

import (
	"strings"
	"testing"
)

// A literal index into an array with storage — a `ref [N]T` parameter, a module's table, a
// local — reads the element in place. It loaded the whole array and took the element with
// `extractvalue`: `xs[5]` on a `ref [28672]u32` was a 112 KB load, and the aggregates such
// loads make are what LLVM's optimizer is slowest on (10/11, a game's tile art). A runtime
// index already went through the array's address; now a literal one does too — and still
// reads the right elements.
func TestExec_ALiteralIndexReadsAnArrayInPlace(t *testing.T) {
	t.Parallel()
	src := `var table: [4096]u32 = #[0; 4096]

let fifth<const N: i64> = pure (xs: ref [N]u32) -> u32 => xs[5]

let main = () -> u8 => {
  table[5] = 7
  var local: [3]u32 = #[1, 2, 3]
  local[2] = 9
  u8(fifth(table) + table[5] + local[2])
}
`
	ir, err := emitSource(t, src)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(ir, "load [4096 x i32]") {
		t.Errorf("a literal index loaded the whole array:\n%s", ir)
	}
	if got := buildAndRun(t, src); got != 23 {
		t.Errorf("exit code: got %d, want 23 (7 through the reference, 7 from the table, 9)", got)
	}
}

// A module's `var` of a fixed array of zeros needs no start-up code: every global is defined
// zeroed. Lowered, `#[0; 28672]` was built on the stack and stored whole — a 112 KB
// aggregate value for nothing, and most of a nine-second build. One of anything else is
// still stored.
func TestEmit_AZeroArrayGlobalNeedsNoStartup(t *testing.T) {
	t.Parallel()
	src := `var art: [4096]u32 = #[0; 4096]
var ones: [4]u32 = #[1; 4]

let main = () -> u8 => u8(art[3] + ones[2])
`
	ir, err := emitSource(t, src)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(ir, "store [4096 x i32]") {
		t.Errorf("a zero array global was stored at start-up:\n%s", ir)
	}
	if got := buildAndRun(t, src); got != 1 {
		t.Errorf("exit code: got %d, want 1 (0 from the zeroed global, 1 from the ones)", got)
	}
}
