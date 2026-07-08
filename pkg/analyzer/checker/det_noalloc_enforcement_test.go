package checker_test

import (
	"testing"

	"github.com/Lyra-Language/lyra/pkg/analyzer/checker"
)

// assertBoundError asserts exactly one diagnostic with the given code.
func assertBoundError(t *testing.T, errs []checker.PurityError, wantCode string) {
	t.Helper()
	if len(errs) != 1 {
		t.Fatalf("expected 1 error, got %d: %v", len(errs), errs)
	}
	if errs[0].Code != wantCode {
		t.Errorf("wrong code: got %q, want %q (%v)", errs[0].Code, wantCode, errs[0])
	}
}

// --- det: input is forbidden, but output / mutation / allocation are allowed ---

// Reading external input is non-deterministic, so it breaks `det`.
func TestDet_ReadsInput_Violates(t *testing.T) {
	src := `let load = det (path: string) -> string => { read(path) }`
	assertBoundError(t, checkPurity(t, src), "lyra-E016")
}

// `await` resumes with an external result — an input effect — so it breaks `det`.
func TestDet_Await_Violates(t *testing.T) {
	src := `let fetch = det (x: i64) -> i64 => { await x }`
	assertBoundError(t, checkPurity(t, src), "lyra-E016")
}

// Output (a void-returning print) is determinism-safe: a `det` function may log.
// This is the whole point of splitting IO into input vs output.
func TestDet_Prints_Ok(t *testing.T) {
	src := `
let step = det (n: i64) -> i64 => {
    println(n)
    n
}`
	assertPurityCount(t, checkPurity(t, src), 0)
}

// `det` permits mutation of owned locals — it is coarser than `pure`.
func TestDet_LocalMutation_Ok(t *testing.T) {
	src := `
let tick = det (n: i64) -> i64 => {
    var acc = 0
    acc += n
    acc
}`
	assertPurityCount(t, checkPurity(t, src), 0)
}

// A `det` function that transitively calls an input-reading helper is flagged
// too — enforcement runs against the full inferred (transitive) effect set.
func TestDet_TransitiveInput_Violates(t *testing.T) {
	src := `
let helper = (p: string) -> string => { read(p) }
let load = det (p: string) -> string => { helper(p) }`
	// Only the `det` function is flagged; the unannotated helper is fine.
	assertBoundError(t, checkPurity(t, src), "lyra-E016")
}

// --- noalloc: heap allocation is forbidden; output / mutation are allowed ---

// Constructing a `shared`-declared value heap-allocates, so it breaks `noalloc`.
func TestNoAlloc_SharedConstruction_Violates(t *testing.T) {
	src := `
shared struct Node { v: i64 }
let make = noalloc (x: i64) -> i64 => {
    let n = Node { v: x }
    n.v
}`
	assertBoundError(t, checkPurity(t, src), "lyra-E016")
}

// A `shared` construction lexically inside a `with`-arena block is discharged,
// so a `noalloc` function may build into an arena.
func TestNoAlloc_ArenaDischarged_Ok(t *testing.T) {
	src := `
shared struct Node { v: i64 }
let make = noalloc (x: i64) -> i64 => {
    with Arena.new(bytes(64)) {
        let n = Node { v: x }
        x
    }
}`
	assertPurityCount(t, checkPurity(t, src), 0)
}

// `noalloc` is a resource axis: output and mutation don't allocate, so a
// `det noalloc` function that logs and mutates locals is clean.
func TestDetNoAlloc_OutputAndMutation_Ok(t *testing.T) {
	src := `
let update = det noalloc (n: i64) -> i64 => {
    println(n)
    var acc = n
    acc += 1
    acc
}`
	assertPurityCount(t, checkPurity(t, src), 0)
}

// --- the split must not loosen `pure`: output still violates it ---

// `pure` forbids ALL io, including output — a memoized/reordered pure call must
// not drop or reorder a print.
func TestPure_Prints_StillViolates(t *testing.T) {
	src := `
let f = pure (n: i64) -> i64 => {
    println(n)
    n
}`
	assertBoundError(t, checkPurity(t, src), "lyra-E007")
}

// --- trait methods carry the same bounds ---

func TestDet_TraitMethod_ReadsInput_Violates(t *testing.T) {
	src := `
impl Loader for World {
    load = det (self) => { read("x") }
}`
	assertBoundError(t, checkPurity(t, src), "lyra-E016")
}
