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

// **A table of structs or tuples of literals is constant data too** (09/30): their
// literals lower to `insertvalue` chains, folded into one constant. Read at a run-time
// index, a default field and a nested struct come out as written; a table with a leaf that
// is not a constant (a string) is still inlined, and still right.
func TestExec_ConstStructTables(t *testing.T) {
	t.Parallel()
	got := strings.TrimSpace(buildAndRunWithPrelude(t, `module main
struct Step {
  frame: u16,
  ticks: u16 = 8,
  at: Point,
}

struct Point {
  x: i16,
  y: i16,
}

const STEPS: [3]Step = #[
  Step { frame: 1, at: Point { x: -1, y: 2 } },
  Step { frame: 4, ticks: 30, at: Point { x: 3, y: -4 } },
  Step { frame: 2, at: Point { x: 5, y: 6 } },
]
const PAIRS: [2](u8, bool) = #[(7, true), (9, false)]
const NAMES: [2](string, u8) = #[("one", 1), ("two", 2)]

let main = () -> void => {
  var total = 0
  for s in STEPS { total += i64(s.ticks) }
  let i = total / 46
  let s = STEPS[i]
  println("${total} ${s.frame} ${s.ticks} ${s.at.x} ${s.at.y}")
  println("${PAIRS[i - 1].0} ${PAIRS[i].1} ${NAMES[i].0}")
}
`, ""))
	if want := "46 4 30 3 -4\n7 false two"; got != want {
		t.Errorf("got %q; want %q", got, want)
	}
}

// The struct table is one constant global, indexed in place: no `insertvalue` rebuilds it,
// and no whole-table load copies it to the stack.
func TestEmit_AConstStructTableIsConstantData(t *testing.T) {
	t.Parallel()
	ir := emitWithPrelude(t, `module main
struct Step {
  frame: u16,
  ticks: u16,
}
const STEPS: [2]Step = #[Step { frame: 1, ticks: 8 }, Step { frame: 0, ticks: 6 }]
let main = () -> u8 => {
  var s: u16 = 0
  for i in 0..<2 { s += STEPS[i].ticks }
  u8(s)
}
`)
	if !strings.Contains(ir, "@lyra_const_main.STEPS = private constant [2 x %main__Step] [%main__Step { i16 1, i16 8 }, %main__Step { i16 0, i16 6 }]") {
		t.Errorf("expected STEPS as a private constant global; IR:\n%s", ir)
	}
	if strings.Contains(ir, "insertvalue %main__Step") || strings.Contains(ir, "load [2 x %main__Step], [2 x %main__Step]* @lyra_const_main.STEPS") {
		t.Errorf("the table is rebuilt or copied where it is read")
	}
}

// A `const` may hold data values (09/30): a bare constructor, an applied one, a table of
// them, and a struct with one inside — read at a run-time index and matched as written.
func TestExec_ConstDataValues(t *testing.T) {
	t.Parallel()
	got := strings.TrimSpace(buildAndRunWithPrelude(t, `module main
data Dir = Left | Right | Jump(u8)

struct Move {
  dir: Dir,
  frames: u16,
}

const START: Dir = Left
const NONE_YET: Maybe<u8> = None
const THREE: Maybe<u8> = Some(3)
const MOVES: [3]Dir = #[Left, Jump(12), Right]
const PLAN: [2]Move = #[Move { dir: Jump(4), frames: 10 }, Move { dir: Right, frames: 20 }]

let describe = (d: Dir) -> string => match d {
  Left => "left",
  Right => "right",
  Jump(h) => "jump ${h}",
}

let main = () -> void => {
  var n = 0
  for m in PLAN { n += i64(m.frames) }
  let i = n / 30
  println("${describe(START)} ${NONE_YET.unwrap_or(0)} ${THREE.unwrap_or(0)}")
  println("${describe(MOVES[i])} ${describe(MOVES[i + 1])} ${describe(PLAN[i - 1].dir)} ${PLAN[i].frames}")
}
`, ""))
	if want := "left 0 3\njump 12 right jump 4 20"; got != want {
		t.Errorf("got %q; want %q", got, want)
	}
}
