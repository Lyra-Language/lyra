package llvm

import (
	"strings"
	"testing"
)

// Array comprehensions — `[ x in xs | guard | result ]`.
//
// The grammar has had these since before the collector did; this is the lowering. A
// comprehension allocates one box at the product of its sources' lengths and fills it,
// recording the *count* that survived the guards as the box's length — so the assertions
// below check both the length and the elements, since an over-allocated box with the wrong
// length reads as a correct one until you look at what is in it.

func TestExec_ArrayCompMapsEveryElement(t *testing.T) {
	t.Parallel()
	got := buildAndRun(t, `
let main = () -> u8 => {
  let xs: []i64 = [1, 2, 3, 4]
  let doubled = [x in xs | x * 2]
  u8(doubled.len() + doubled[0] + doubled[3])
}`)
	if got != 14 {
		t.Errorf("expected 14 (len 4 + 2 + 8), got %d", got)
	}
}

// A guard filters, so the box's length is the survivor count and not the capacity it was
// allocated at. This is the assertion that fails if the final length is ever written as the
// capacity.
func TestExec_ArrayCompGuardSetsTheLength(t *testing.T) {
	t.Parallel()
	got := buildAndRun(t, `
let main = () -> u8 => {
  let xs: []i64 = [1, 2, 3, 4, 5, 6]
  let evens = [x in xs | x % 2 == 0 | x]
  u8(evens.len() * 10 + evens[0] + evens[2])
}`)
	if got != 38 {
		t.Errorf("expected 38 (len 3 → 30, plus 2 and 6), got %d", got)
	}
}

// Every element failing the guard yields a real, empty box rather than anything special —
// the same uniformity an empty array literal has.
func TestExec_ArrayCompCanBeEmpty(t *testing.T) {
	t.Parallel()
	got := buildAndRun(t, `
let main = () -> u8 => {
  let xs: []i64 = [1, 3, 5]
  let evens = [x in xs | x % 2 == 0 | x]
  u8(evens.len() + 7)
}`)
	if got != 7 {
		t.Errorf("expected 7 (an empty result), got %d", got)
	}
}

// Several generators nest, so the result is the cross product — 3 × 4 = 12 elements, and
// the last is the product of the two final elements.
func TestExec_ArrayCompNestsGenerators(t *testing.T) {
	t.Parallel()
	got := buildAndRun(t, `
let main = () -> u8 => {
  let as: []i64 = [1, 2, 3]
  let bs: []i64 = [1, 2, 3, 4]
  let products = [a in as, b in bs | a * b]
  u8(products.len() + products[11])
}`)
	if got != 24 {
		t.Errorf("expected 24 (12 elements + 3*4), got %d", got)
	}
}

// Guards are conjunctive and short-circuit, so a second guard only sees what the first let
// through.
func TestExec_ArrayCompMultipleGuards(t *testing.T) {
	t.Parallel()
	got := buildAndRun(t, `
let main = () -> u8 => {
  let xs: []i64 = [1, 2, 3, 4, 5, 6, 7, 8]
  let picked = [x in xs | x % 2 == 0, x > 4 | x]
  u8(picked.len() * 10 + picked[0])
}`)
	if got != 26 {
		t.Errorf("expected 26 (len 2 → 20, first is 6), got %d", got)
	}
}

// A **managed** element type: the box owns its elements and frees them once. Run under
// `./asan.sh` this is also the leak/double-free case.
func TestExec_ArrayCompManagedElements(t *testing.T) {
	t.Parallel()
	got := buildAndRun(t, `
let label = (n: i64) -> string => "a" ++ "b"
let size = (s: string) -> i64 => 2
let main = () -> u8 => {
  let xs: []i64 = [1, 2, 3]
  let names = [x in xs | label(x)]
  u8(names.len() + size(names[0]))
}`)
	if got != 5 {
		t.Errorf("expected 5, got %d", got)
	}
}

// A fixed-size source works as well as a dynamic one, and the result is dynamic either
// way — a guard decides the length at run time, so a comprehension is never `[N]T`.
func TestExec_ArrayCompOverFixedSizeSource(t *testing.T) {
	t.Parallel()
	got := buildAndRun(t, `
let main = () -> u8 => {
  let xs: [3]i64 = [5, 6, 7]
  let ys = [x in xs | x + 1]
  u8(ys.len() + ys[2])
}`)
	if got != 11 {
		t.Errorf("expected 11 (len 3 + 8), got %d", got)
	}
}

// The generator binding is scoped to the comprehension: an outer name of the same spelling
// is untouched after it.
func TestExec_ArrayCompBindingIsScoped(t *testing.T) {
	t.Parallel()
	got := buildAndRun(t, `
let main = () -> u8 => {
  let x = 9
  let xs: []i64 = [1, 2]
  let ys = [x in xs | x]
  u8(x + ys.len())
}`)
	if got != 11 {
		t.Errorf("expected 11 (the outer x is still 9), got %d", got)
	}
}

// Deferred loudly rather than mis-lowered: a source that depends on an earlier generator
// would be materialized before that binding has a value, since sources are hoisted to make
// the capacity computable.
func TestEmit_ArrayCompDependentGeneratorIsRefused(t *testing.T) {
	t.Parallel()
	_, err := emitSource(t, `
let main = () -> u8 => {
  let grid: [][]i64 = [[1, 2], [3, 4]]
  let flat = [row in grid, cell in row | cell]
  u8(flat.len())
}`)
	if err == nil {
		t.Fatal("expected a dependent generator to be refused")
	}
	if !strings.Contains(err.Error(), "depends on an earlier generator") {
		t.Errorf("expected the message to name the dependency, got %v", err)
	}
}
