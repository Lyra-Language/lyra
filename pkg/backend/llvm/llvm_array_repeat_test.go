package llvm

import (
	"strings"
	"testing"
)

// The array-repeat literal, `[v; n]` (08/08). It parsed and collected from the start and
// nothing downstream read it — the typechecker reported `unknown expression type
// "[0; 5]"` — which is loud rather than silent, so it was an unimplemented feature rather
// than a phantom.

func TestExec_ArrayRepeatFixedSize(t *testing.T) {
	t.Parallel()
	const src = `
module main
let main = () -> void => {
  let g = [0; 5];
  let a: [4]u8 = [7; 4];
  println("${g.len()} ${g[0]} ${g[4]} ${a.len()} ${a[3]}");
}
`
	if got := strings.TrimSpace(buildAndRunWithPrelude(t, src, "")); got != "5 0 0 4 7" {
		t.Errorf("repeat literal = %q; want \"5 0 0 4 7\"", got)
	}
}

// The slots are independent storage, not n views of one cell — the thing that would be
// wrong if the aggregate were built by aliasing rather than by n inserts.
func TestExec_ArrayRepeatSlotsAreIndependent(t *testing.T) {
	t.Parallel()
	const src = `
module main
let main = () -> void => {
  var m = [1; 3];
  m[1] = 9;
  println("${m[0]} ${m[1]} ${m[2]}");
}
`
	if got := strings.TrimSpace(buildAndRunWithPrelude(t, src, "")); got != "1 9 1" {
		t.Errorf("mutated repeat = %q; want \"1 9 1\"", got)
	}
}

// A `const` count. The typechecker folds it and **rewrites the AST** to the literal, so
// the backend needs no const lookup of its own — this is the test that the rewrite
// actually happens, since without it codegen has no way to know the length.
func TestExec_ArrayRepeatConstCount(t *testing.T) {
	t.Parallel()
	const src = `
module main
const N = 3
const M = N * 2
let main = () -> void => {
  let a = [1; N];
  let b = [2; M];
  println("${a.len()} ${b.len()} ${b[5]}");
}
`
	if got := strings.TrimSpace(buildAndRunWithPrelude(t, src, "")); got != "3 6 2" {
		t.Errorf("const count = %q; want \"3 6 2\"", got)
	}
}

// A **dynamic** context builds a heap `[]T` rather than a fixed-size array, exactly as
// `[1, 2, 3]` does under the same annotation. It was invalid IR at first — a
// `[3 x i64]` bitcast to a pointer — because the type recorded said `[]i64` and the
// lowering had only the fixed-size path.
func TestExec_ArrayRepeatDynamicContext(t *testing.T) {
	t.Parallel()
	const src = `
module main
let main = () -> void => {
  let d: []i64 = [4; 3];
  println("${d.len()} ${d[0]} ${d[2]}");
}
`
	if got := strings.TrimSpace(buildAndRunWithPrelude(t, src, "")); got != "3 4 4" {
		t.Errorf("dynamic repeat = %q; want \"3 4 4\"", got)
	}
}

// A **managed** element is where the reference counting has to be right: the value is
// evaluated once and every slot is an owner, so the lowering emits n-1 extra retains.
// Without them the box's drop glue releases n times what was retained once — a
// use-after-free the ASan harness turns into a fault rather than wrong text.
func TestExec_ArrayRepeatManagedElement(t *testing.T) {
	t.Parallel()
	const src = `
module main
let main = () -> void => {
  let s = "ab" ++ "cd";
  let d: []string = [s; 3];
  println("${d[0]} ${d[2]} ${d.len()}");
}
`
	if got := strings.TrimSpace(buildAndRunWithPrelude(t, src, "")); got != "abcd abcd 3" {
		t.Errorf("managed repeat = %q; want \"abcd abcd 3\"", got)
	}
}

// The value is evaluated **once**, not n times. Asserted through a side effect, since
// that is the only way the difference is observable — and it is the promise the
// compile-time count buys: `[next(); 3]` would otherwise be three calls with nothing in
// the syntax saying so.
func TestExec_ArrayRepeatEvaluatesTheValueOnce(t *testing.T) {
	t.Parallel()
	const src = `
module main
let noisy = () -> i64 => {
  println("called");
  7
}
let main = () -> void => {
  let a = [noisy(); 3];
  println("${a[0]} ${a[2]}");
}
`
	if got := strings.TrimSpace(buildAndRunWithPrelude(t, src, "")); got != "called\n7 7" {
		t.Errorf("evaluation count = %q; want one \"called\" then \"7 7\"", got)
	}
}

// A struct element, and a zero-length repeat.
func TestExec_ArrayRepeatStructAndEmpty(t *testing.T) {
	t.Parallel()
	const src = `
module main
struct Pt { x: i64, y: i64 }
let main = () -> void => {
  let ps = [Pt { x: 1, y: 2 }; 3];
  let e = [0; 0];
  println("${ps[2].x} ${ps[0].y} ${e.len()}");
}
`
	if got := strings.TrimSpace(buildAndRunWithPrelude(t, src, "")); got != "1 2 0" {
		t.Errorf("struct/empty repeat = %q; want \"1 2 0\"", got)
	}
}

// The other half of "the slots are independent storage": the *values* in them are not,
// where the value is a reference. These are the measurements lyra-W019 is built on, and
// they belong in the backend because the predicate the warning uses is a claim about
// what the lowering does — a change here that made a `[]T` element copy per slot would
// silently turn the warning into noise.
func TestExec_ArrayRepeatSharesAReferenceElement(t *testing.T) {
	t.Parallel()
	const src = `
module main
let main = () -> void => {
  var row: []i64 = [0, 0];
  var grid: [][]i64 = [row; 3];
  grid[0][0] = 7;
  println("${grid[1][0]}");
}
`
	if got := strings.TrimSpace(buildAndRunWithPrelude(t, src, "")); got != "7" {
		t.Errorf("write through slot 0 = %q; want \"7\" (every slot holds the same array)", got)
	}
}

// A struct is copied per slot, so its scalar fields are independent — while the array
// *behind* one of its fields is not, the copy being shallow. Both readings in one test,
// because the pair is the rule: lyra-W019 fires on the second and not the first.
func TestExec_ArrayRepeatStructIsCopiedButItsArrayIsNot(t *testing.T) {
	t.Parallel()
	const src = `
module main
struct Cell { n: i64 }
struct Row { cells: []i64 }
let main = () -> void => {
  var cs: []Cell = [Cell { n: 0 }; 3];
  cs[0].n = 7;
  var rs: []Row = [Row { cells: [0, 0] }; 3];
  rs[0].cells[0] = 7;
  println("${cs[1].n} ${rs[1].cells[0]}");
}
`
	if got := strings.TrimSpace(buildAndRunWithPrelude(t, src, "")); got != "0 7" {
		t.Errorf("struct-vs-field aliasing = %q; want \"0 7\"", got)
	}
}

// A string element is managed and shared, and the sharing is unobservable: there is no
// way to mutate a string in place, so slot 0 can only be *replaced*. This is why
// lyra-W019's predicate is narrower than ownership.IsManaged — `["hi"; 3]` is correct
// code and by far the commoner spelling.
func TestExec_ArrayRepeatStringElementIsUnobservable(t *testing.T) {
	t.Parallel()
	const src = `
module main
let main = () -> void => {
  var words: []string = ["hi"; 3];
  words[0] = "bye";
  println("${words[0]} ${words[1]}");
}
`
	if got := strings.TrimSpace(buildAndRunWithPrelude(t, src, "")); got != "bye hi" {
		t.Errorf("string repeat = %q; want \"bye hi\"", got)
	}
}
