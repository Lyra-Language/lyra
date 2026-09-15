package llvm

import (
	"os/exec"
	"testing"
)

// An array literal mixing a **solved** and an **unsolved** generic element lowers at the
// context's element width, not at the width its own elements joined to.
//
// A front-end test cannot establish this. The elements narrowing is only half the fix —
// assignability and the backend both read the type recorded for the literal **node**, so a
// version that narrowed the leaves and left the node alone would type-check and then build
// `Maybe<i64>` payloads into a `Maybe<u8>` array. Running it is what says which happened.
func TestExec_ArrayLiteralTakesItsContextsElementType(t *testing.T) {
	t.Parallel()
	out := buildAndRunWithPrelude(t, `module main
let main = () -> void => {
  let xs: []Maybe<u8> = [Some(200), None]
  let ys: [2]Maybe<u8> = #[Some(201), None]
  print("${xs[0].unwrap_or(0)} ${xs[1].unwrap_or(7)} ${ys[0].unwrap_or(0)}")
}`, "")
	if out != "200 7 201" {
		t.Fatalf("want %q, got %q", "200 7 201", out)
	}
}

// A struct field seeds a generic call's type arguments, and the result lowers. The field's
// declaration is the only thing that says what `bag_new()` builds — no argument mentions
// the variable at all.
func TestExec_StructFieldSeedsAGenericConstructor(t *testing.T) {
	t.Parallel()
	out := buildAndRunWithPrelude(t, `module main
struct Bag<t> { items: []t }
let bag_new<t> = pure () -> Bag<t> => Bag { items: [] }
struct Holder { b: Bag<string> }
let main = () -> void => {
  var h = Holder { b: bag_new() }
  h.b.items.push("x")
  print("${h.b.items.len()} ${h.b.items[0]}")
}`, "")
	if out != "1 x" {
		t.Fatalf("want %q, got %q", "1 x", out)
	}
}

// The generic caller's lambda, lowered. `sort` delegates to `sort_by` with an unannotated
// comparator, which is the prelude's own spelling since 09/11 — so this exercises the
// shipped code path rather than a reconstruction of it.
func TestExec_UnannotatedComparatorInAGenericCaller(t *testing.T) {
	t.Parallel()
	out := buildAndRunWithPrelude(t, `module main
let mysort<t> where t: Ord = (self: mut []t) -> void => self.sort_by((a, b) => a.compare(b))
let main = () -> void => {
  var xs: []i64 = [5, 3, 9, 1]
  xs.mysort()
  let ys = [30, 10, 20].sorted()
  print("${xs[0]}${xs[3]} ${ys[0]}${ys[2]}")
}`, "")
	if out != "19 1030" {
		t.Fatalf("want %q, got %q", "19 1030", out)
	}
}

// A literal payload's flavor comes from its context, and the managed `[]i64` a `Maybe`
// then holds is retained and released correctly — run twice, the second time under ASan.
// Every position that was refused until 09/13 is exercised: an annotation, an element
// join with different lengths, a struct field, a reassignment, an argument, a return, and
// a literal passed to a generic `t` another argument already bound (`unwrap_or([])`).
func TestExec_ArrayPayloadTakesItsContextsFlavor(t *testing.T) {
	t.Parallel()
	src := `
struct Holder { m: Maybe<[]i64> }
let give = () -> Maybe<[]i64> => Some([4, 5, 6])
let total = (m: Maybe<[]i64>) -> i64 => m.unwrap_or([]).len()
let main = () -> u8 => {
  let a: Maybe<[]i64> = Some([1])
  let rows: []Maybe<[]i64> = [Some([1]), Some([2, 3]), None]
  let grid: [][]i64 = [[1], [2, 3], []]
  let h = Holder { m: Some([1, 2]) }
  var r: Maybe<[]i64> = None
  r = Some([7, 8])
  var sum = total(a) + total(give()) + total(h.m) + total(r) + total(Some([9, 9]))
  for row in rows { sum += row.unwrap_or([0, 0, 0, 0]).len() }
  sum += grid[1][1] + grid[2].len()
  u8(sum)   // 1 + 3 + 2 + 2 + 2, then 1 + 2 + 4, then 3 + 0
}`
	if got := exitCode(t, exec.Command(preludeBinary(t, src)).Run()); got != 20 {
		t.Errorf("exited %d; want 20", got)
	}
	if got := buildAndRunASanWithPrelude(t, src); got != 20 {
		t.Errorf("under ASan: exited %d; want 20", got)
	}
}

// Branches of array literals lower as the dynamic array their context asks for: the branch
// values and the merge the backend builds from the branching node's record must agree, or
// two dynamic arrays meet a fixed-array phi (rule 5's joinPhi check, or silent garbage).
// Different lengths, an empty branch, a match and a newtype, with managed elements and a
// push onto each result, so ASan sees any record that disagrees with what was built.
func TestExec_ArrayLiteralBranchesTakeTheContext(t *testing.T) {
	t.Parallel()
	src := `newtype Bag = []string
let pick = (c: bool) -> []string => if c { ["a".slice(0, 1) ++ "x"] } else { ["b", "c" ++ "d"] }
let nums = (n: i64) -> []i64 => match n { 0 => [], 1 => [5], _ => { let k = n * 2; [k, k, k] } }
let bag = (c: bool) -> Bag => if c { [] } else { ["p", "q" ++ "r"] }
let main = () -> u8 => {
  var a = pick(true)
  a.push("y")
  var b = pick(false)
  b.push("z")
  var n = nums(4)
  n.push(1)
  let e = nums(0)
  let g = base(bag(false))
  u8(a.len() + a[0].len() + b.len() + b[1].len() + n.len() + n[0] + e.len() + nums(1)[0] + g.len() + g[1].len() + base(bag(true)).len())
}`
	// 2 + 2 + 3 + 2 + 4 + 8 + 0 + 5 + 2 + 2 + 0
	const want = 30
	if got := buildAndRun(t, src); got != want {
		t.Errorf("exited %d; want %d", got, want)
	}
	if got := buildAndRunASan(t, lookClang(t), src); got != want {
		t.Errorf("under ASan: exited %d; want %d", got, want)
	}
}

// Same-length branches join to a fixed array on their own, so the branching node keeps that
// record unless the context's push re-records it (recordBranchingValueNode). An annotated
// binding and an argument slot are where the backend reads it: without the re-record it
// stored a `[2]string` merge into a `[]string` slot, and llir panicked in NewStore.
func TestExec_SameLengthArrayBranchesRecordTheContext(t *testing.T) {
	t.Parallel()
	src := `let take = (xs: []string) -> i64 => xs.len() + xs[1].len()
let main = () -> u8 => {
  let c = true
  var xs: []string = if c { ["a" ++ "b", "c"] } else { ["d", "e"] }
  xs.push("f")
  let n = take(if c { ["g", "h" ++ "i"] } else { ["j", "k"] })
  let m = match n { 4 => { let ys: []string = if c { ["l", "m"] } else { ["n", "o"] }; ys.len() }, _ => 0 }
  u8(xs.len() + xs[0].len() + n + m)
}`
	// 3 + 2 + 4 + 2
	const want = 11
	if got := buildAndRun(t, src); got != want {
		t.Errorf("exited %d; want %d", got, want)
	}
	if got := buildAndRunASan(t, lookClang(t), src); got != want {
		t.Errorf("under ASan: exited %d; want %d", got, want)
	}
}

// Both spellings lower as what they say, end to end: `[…]` and `[v; n]` as heap boxes that
// grow, `#[…]` and `#[v; n]` as inline storage, nested either way, as arguments, payloads,
// receivers and loop sources, with managed elements so ASan sees any box built as the
// wrong flavor.
func TestExec_ArrayLiteralSpellingIsTheFlavor(t *testing.T) {
	t.Parallel()
	src := `struct Grid { cells: [2][3]u8 }
data Slot = Full([2]string) | Empty
let sumFixed = (xs: [3]i64) -> i64 => xs[0] + xs[1] + xs[2]
let sumDyn = (self: []i64) -> i64 => { var t = 0; for x in self { t = t + x }; t }
let main = () -> u8 => {
  var d = [1, 2, 3]
  d.push(4)
  let f = #[10, 20, 30]
  var r = [0; 2]
  r.push(7)
  let rf = #[5; 3]
  let g = Grid { cells: #[#[1, 2, 3], #[4, 5, 6]] }
  var nested = [["a" ++ "b"], []]
  nested[1].push("c")
  let slot = Full(#["x" ++ "y", "z"])
  let named = match slot { Full(ss) => ss[0].len() + ss[1].len(), Empty => 0 }
  var loops = 0
  for k in #[1, 2, 3] { loops = loops + k }
  let summed = [4, 5].sumDyn()
  u8(sumDyn(d) + sumFixed(f) + r.len() + r[2] + rf[2] + i64(g.cells[1][2]) + nested[0][0].len() + nested[1].len() + named + loops + summed)
}`
	// 10 + 60 + 3 + 7 + 5 + 6 + 2 + 1 + 3 + 6 + 9
	const want = 112
	if got := buildAndRun(t, src); got != want {
		t.Errorf("exited %d; want %d", got, want)
	}
	if got := buildAndRunASan(t, lookClang(t), src); got != want {
		t.Errorf("under ASan: exited %d; want %d", got, want)
	}
}
