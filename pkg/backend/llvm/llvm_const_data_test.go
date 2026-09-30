package llvm

import (
	"strings"
	"testing"
)

// **A `const` fixed array of literals is a constant global, read in place** (09/30,
// const_data.go): tile and palette tables for the Genesis, where an inlined table was one
// store per element onto a 4 KB stack at every use.

func TestExec_ConstTables(t *testing.T) {
	t.Parallel()
	got := strings.TrimSpace(buildAndRunWithPrelude(t, `module main
const WORDS: [6]u16 = #[10, 20, 30, 40, 50, 60]
const GRID: [2][3]i8 = #[#[1, -2, 3], #[-4, 5, -6]]
const ZEROS: [4]u8 = #[0; 4]

let sum_from = pure (p: ^u16, n: i64) -> i64 => {
  var total = 0
  for i in 0..<n { total += i64(unsafe { p.offset(i)^ }) }
  total
}

let main = () -> void => {
  var s = 0
  for w in WORDS { s += i64(w) }
  let i = s / 100
  println("${s} ${WORDS[i]} ${WORDS[3]} ${GRID[1][2]} ${ZEROS[3]}")
  let p: ^u16 = unsafe { &WORDS[2] }
  println("${sum_from(p, 3)}")
}
`, ""))
	if want := "210 30 40 -6 0\n120"; got != want {
		t.Errorf("got %q; want %q", got, want)
	}
}

// The table is emitted once as constant data, and a loop over it indexes the global — no
// whole-array load, which is what became a stack copy.
func TestEmit_AConstTableIsConstantData(t *testing.T) {
	t.Parallel()
	ir := emitWithPrelude(t, `module main
const TABLE: [4]u32 = #[1, 2, 3, 4]
let main = () -> u8 => {
  var s: u32 = 0
  for v in TABLE { s += v }
  u8(s)
}
`)
	if !strings.Contains(ir, "@lyra_const_main.TABLE = private constant [4 x i32] [i32 1, i32 2, i32 3, i32 4]") {
		t.Errorf("expected TABLE as a private constant global; IR:\n%s", ir)
	}
	if strings.Contains(ir, "load [4 x i32], [4 x i32]* @lyra_const_main.TABLE") {
		t.Errorf("the loop loads the whole table (a copy) instead of indexing it")
	}
}
