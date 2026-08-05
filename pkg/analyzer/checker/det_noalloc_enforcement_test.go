package checker_test

import (
	"strings"
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

// Drawing from the ambient global RNG is non-deterministic, so it breaks `det`.
// The message must name the *random* source specifically — proving the Rand bit
// was detected, not the conservative "unknown external call" Input fallback that
// would also produce E016 but describe it as reading input.
func TestDet_AmbientRandom_Violates(t *testing.T) {
	src := `let roll = det (n: i64) -> i64 => { Random.global() }`
	errs := checkPurity(t, src)
	assertBoundError(t, errs, "lyra-E016")
	if !strings.Contains(errs[0].Message, "random source") {
		t.Errorf("expected message to name the random source, got: %q", errs[0].Message)
	}
}

// Reading the ambient wall clock is non-deterministic, so it breaks `det`. As
// with the RNG, the message must name the *clock* specifically (the Time bit),
// not the Input fallback.
func TestDet_AmbientClock_Violates(t *testing.T) {
	src := `let stamp = det (n: i64) -> i64 => { wallClock() }`
	errs := checkPurity(t, src)
	assertBoundError(t, errs, "lyra-E016")
	if !strings.Contains(errs[0].Message, "system clock") {
		t.Errorf("expected message to name the system clock, got: %q", errs[0].Message)
	}
}

// The ambient-vs-threaded split: a seed threaded in as a parameter is ordinary
// data, not an effect, so a `det` function may derive pseudo-randomness from it
// (an LCG step here) and stay deterministic — reproducible from its input.
func TestDet_ThreadedSeed_Ok(t *testing.T) {
	src := `let next = det (seed: u64) -> u64 => { seed * 6364136223846793005 + 1442695040888963407 }`
	assertPurityCount(t, checkPurity(t, src), 0)
}

// Ambient randomness breaks `pure` too (Rand ∈ PurityEffects), reported by the
// fine-grained per-op walk as E007 rather than the E016 bound check.
func TestPure_AmbientRandom_Violates(t *testing.T) {
	src := `let roll = pure (n: i64) -> i64 => { Random.global() }`
	assertBoundError(t, checkPurity(t, src), "lyra-E007")
}

// --- noalloc: heap allocation is forbidden; output / mutation are allowed ---

// Constructing a value used as `shared` heap-allocates, so it breaks `noalloc`.
// Allocation is a use-site flavor now: the binding is annotated `shared`, and the
// typechecker records that flavor on the construction (AllocationOf sees it).
func TestNoAlloc_SharedConstruction_Violates(t *testing.T) {
	src := `
struct Node { v: i64 }
let make = noalloc (x: i64) -> i64 => {
    let n: shared Node = Node { v: x }
    n.v
}`
	assertBoundError(t, checkPurity(t, src), "lyra-E016")
}

// A construction NOT used as `shared` (an unannotated / stack binding) does not
// heap-allocate, so it is clean under `noalloc`.
func TestNoAlloc_StackConstruction_Ok(t *testing.T) {
	src := `
struct Node { v: i64 }
let make = noalloc (x: i64) -> i64 => {
    let n = Node { v: x }
    n.v
}`
	assertPurityCount(t, checkPurity(t, src), 0)
}

// An unknown external call could allocate, and we can't inspect its body to know
// otherwise, so a `noalloc` function that calls one is conservatively flagged —
// the alloc-axis sibling of the same taint that makes an unknown call impure for
// `pure`/`det`. (Before this taint included EffectAlloc, `noalloc` only caught
// known `shared` constructions and silently passed such calls.)
func TestNoAlloc_UnknownExternalCall_Violates(t *testing.T) {
	src := `let make = noalloc (n: i64) -> i64 => { helper(n) }`
	assertBoundError(t, checkPurity(t, src), "lyra-E016")
}

// The conservative alloc taint propagates transitively: a `noalloc` function
// calling a local helper that itself calls an unknown external function is
// flagged too (the helper's inferred effect set carries EffectAlloc).
func TestNoAlloc_TransitiveUnknownCall_Violates(t *testing.T) {
	src := `
let helper = (n: i64) -> i64 => { external(n) }
let make = noalloc (n: i64) -> i64 => { helper(n) }`
	assertBoundError(t, checkPurity(t, src), "lyra-E016")
}

// A `shared` construction lexically inside a `with`-arena block is discharged,
// so a `noalloc` function may build into an arena.
func TestNoAlloc_ArenaDischarged_Ok(t *testing.T) {
	src := `
struct Node { v: i64 }
let make = noalloc (x: i64) -> i64 => {
    with Arena.new(bytes(64)) {
        let n: shared Node = Node { v: x }
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

// --- noalloc: a dynamic array is a heap box, whatever flavor it carries ---

// A `[]T` literal allocates. Until 08/04 the alloc effect asked only whether a value's
// *flavor* was `shared` — the right question for a construction, where allocation is a
// use-site property, and the wrong one for a dynamic array, which lowers to a ref-counted
// box before any flavor is consulted. So this was accepted, and once `map`/`filter` for
// arrays went into the prelude as comprehensions the annotation was being claimed by
// functions that allocate per element.
func TestNoAlloc_DynamicArrayLiteral_Violates(t *testing.T) {
	src := `let build = pure noalloc (n: i64) -> []i64 => [1, 2, 3]`
	assertBoundError(t, checkPurity(t, src), "lyra-E016")
}

// A comprehension always builds a box — its length is a runtime question, so it has no
// fixed-size form to be the cheap case.
func TestNoAlloc_ArrayComprehension_Violates(t *testing.T) {
	src := `let double = pure noalloc (xs: []i64) -> []i64 => [x in xs | x * 2]`
	assertBoundError(t, checkPurity(t, src), "lyra-E016")
}

// The **same literal** as a fixed-size `[N]T` is stack storage and allocates nothing. This
// is the assertion that fails if the rule ever keys off the syntax instead of the recorded
// type — the two are told apart by what the literal was used as, not by how it was written.
func TestNoAlloc_FixedSizeArrayLiteral_IsFine(t *testing.T) {
	src := `let build = pure noalloc (n: i64) -> [3]i64 => [1, 2, 3]`
	if errs := checkPurity(t, src); len(errs) != 0 {
		t.Errorf("a fixed-size array is stack storage and must not count as an allocation: %v", errs)
	}
}

// Reading through a `[]T` allocates nothing: the rule is about value-*producing* forms, not
// about every expression whose type happens to be heap-represented. An identifier is the
// case that would break if `heapRepresented` were asked of everything.
func TestNoAlloc_ReadingADynamicArray_IsFine(t *testing.T) {
	src := `let first = pure noalloc (xs: []i64) -> i64 => xs[0]`
	if errs := checkPurity(t, src); len(errs) != 0 {
		t.Errorf("indexing an array allocates nothing: %v", errs)
	}
}

// The message must not say "a `shared`-typed value" any more — a reader told that would go
// looking for a construction that is not there.
func TestNoAlloc_MessageNamesBothWaysToAllocate(t *testing.T) {
	errs := checkPurity(t, `let build = pure noalloc (n: i64) -> []i64 => [1, 2, 3]`)
	if len(errs) != 1 {
		t.Fatalf("expected 1 error, got %v", errs)
	}
	if !strings.Contains(errs[0].Message, "dynamic array") {
		t.Errorf("expected the message to name the dynamic array, got %q", errs[0].Message)
	}
}
