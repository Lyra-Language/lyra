package llvm

import (
	"os/exec"
	"strings"
	"testing"
)

// **A match scrutinee is the one temporary whose uses are in successor blocks.** The value
// is produced in one block, the `switch` sends control elsewhere, and the arms read the
// payload out of it — so releasing it "at its production block", which is what
// `flushStmtTemps` does for a temp that does not dominate the statement's end, puts the
// release *before* the branch into the arms. Appending to an already-terminated block lands
// before its terminator, which is exactly what makes the release too early.
//
// It needs the **nested** position to bite: written as a statement the scrutinee's block is
// the statement's own start block, so the flush takes its fast path and moves the release
// past the whole match. Hence the `let`-binding workaround, and hence the bug surviving
// until an ordinary program — the zlib example's first draft — was written with nested
// matches.
func TestExec_NestedMatchDoesNotFreeItsScrutineeEarly(t *testing.T) {
	t.Parallel()
	src := `
module main
data E = Bad
let first = () -> Result<[]u8, E> => Ok([1, 2, 3])
let second = (src: []u8) -> Result<[]u8, E> => { var b: []u8 = [0; 7]; b[0] = 9; Ok(b) }
let main = () -> void => {
  match first() {
    Err(_) => println("err"),
    Ok(p) => match second(p) {
      Err(_) => println("err"),
      Ok(q) => println("${q.len()} ${q[0]} ${p.len()}"),
    },
  }
}`
	out := buildAndRunWithPrelude(t, src, "")
	if got := strings.TrimSpace(out); got != "7 9 3" {
		t.Errorf("nested match = %q; want \"7 9 3\" — a short or wild length is the scrutinee freed early", got)
	}
}

// The memory half, which the value alone does not prove: the early release is a genuine
// `heap-use-after-free`, reported by ASan on the load of the box's length field. Run in a
// loop so a leak in the other direction would show up too.
func TestExec_NestedMatchScrutineeASan(t *testing.T) {
	t.Parallel()
	src := `module main
data E = Bad
let first = () -> Result<[]u8, E> => Ok([1, 2, 3])
let second = (src: []u8) -> Result<[]u8, E> => { var b: []u8 = [0; 7]; Ok(b) }
let main = () -> u8 => {
  var total = 0
  for i in 0..<40 {
    match first() {
      Err(_) => { total = total + 1 },
      Ok(p) => match second(p) { Err(_) => { total = total + 1 }, Ok(q) => { total = total + q.len() } },
    }
  }
  if total == 280 { 3 } else { 1 }
}`
	clang := lookClang(t)
	if got := exitCodeOf(t, exec.Command(preludeBinary(t, src)).Run()); got != 3 {
		t.Errorf("exited %d; want 3", got)
	}
	if got := exitCodeOf(t, exec.Command(compileCached(t, clang, instrumentForASan(emitWithPrelude(t, src)), "-fsanitize=address")).Run()); got != 3 {
		t.Errorf("under ASan: exited %d; want 3", got)
	}
}

// The scrutinee is lowered once by `lowerMatch` now, for every shape of match — so each
// kind is exercised in the nested position that was broken. A second lowering would
// double-evaluate the scrutinee, which a call with an effect makes visible.
func TestExec_NestedMatchAcrossScrutineeKinds(t *testing.T) {
	t.Parallel()
	for _, c := range []struct{ name, src, want string }{
		{"data", `
data Outcome = Fine(i64) | Broken
data Shape = Dot | Box(i64)
let outer = () -> Outcome => Fine(2)
let inner = (n: i64) -> Shape => Box(n * 5)
let main = () -> void => {
  match outer() {
    Broken => println("err"),
    Fine(n) => match inner(n) { Dot => println("dot"), Box(v) => println("${v}") },
  }
}`, "10"},
		{"tuple", `
data Outcome = Fine(i64) | Broken
let outer = () -> Outcome => Fine(2)
let inner = (n: i64) -> (i64, string) => (n * 5, "x" ++ "y")
let main = () -> void => {
  match outer() {
    Broken => println("err"),
    Fine(n) => match inner(n) { (a, b) => println("${a} ${b}") },
  }
}`, "10 xy"},
		{"scalar", `
data Outcome = Fine(i64) | Broken
let outer = () -> Outcome => Fine(2)
let inner = (n: i64) -> string => "n" ++ "${n}"
let main = () -> void => {
  match outer() {
    Broken => println("err"),
    Fine(n) => match inner(n) { "n2" => println("two"), _ => println("other") },
  }
}`, "two"},
		{"dynamic array", `
data Outcome = Fine(i64) | Broken
let outer = () -> Outcome => Fine(2)
let inner = (n: i64) -> []i64 => { var xs: []i64 = [0; 3]; xs[0] = n; xs }
let main = () -> void => {
  match outer() {
    Broken => println("err"),
    Fine(n) => match inner(n) { [a, ...rest] => println("${a} ${rest.len()}"), _ => println("other") },
  }
}`, "2 2"},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			out, _ := buildAndRunCapture(t, c.src)
			if got := strings.TrimSpace(out); got != c.want {
				t.Errorf("got %q; want %q", got, c.want)
			}
		})
	}
}

// A scrutinee with a side effect must be evaluated **once**. `lowerMatch` lowers it and
// hands the value down, so a lowering that also lowered it for itself would print twice —
// which is the mistake the refactor makes possible and this is the guard against.
func TestExec_MatchScrutineeIsEvaluatedOnce(t *testing.T) {
	t.Parallel()
	out, _ := buildAndRunCapture(t, `
data Shape = Dot | Box(i64)
let noisy = () -> Shape => { println("eval"); Box(1) }
let main = () -> void => match noisy() { Dot => println("dot"), Box(v) => println("box ${v}") }
`)
	if got := strings.TrimSpace(out); got != "eval\nbox 1" {
		t.Errorf("got %q; want the scrutinee evaluated exactly once", got)
	}
}
