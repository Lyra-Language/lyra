package checker_test

import (
	"strings"
	"testing"
)

// `noalloc` and closures. A nested lambda that *captures* heap-boxes its environment
// at every construction (closures.go's buildEnv), so it is charged; a capture-free
// one is the shared pinned static (emptyEnv) and stays free — under the dev lowering
// and under Lambda Set Specialization alike, so the exemption is not a bet on the
// release tier.
//
// The refusal closes the 08/12 audit finding: a `noalloc` function containing a
// capturing lambda checked clean while its emitted body called `lyra_rc_alloc` on
// every invocation — the `slice` hole's shape again, a bound that silently stops
// binding. The old position deferred the charge until LSS ("noalloc is defined
// against the release lowering"); the capture split makes the deferral unnecessary,
// and if LSS later makes a non-escaping capturing closure free, relaxing this is a
// compatible loosening.

func TestClosureAlloc_CapturingClosureRefused(t *testing.T) {
	src := `
let f = noalloc (n: i64) -> i64 => {
    let add = (x: i64) -> i64 => x + n
    add(1)
}`
	errs := checkPurity(t, src)
	assertBoundError(t, errs, "lyra-E016")
	if len(errs) > 0 && !strings.Contains(errs[0].Message, "a closure captures its environment into a heap box") {
		t.Errorf("message should name the closure, got %q", errs[0].Message)
	}
}

// A capture-free nested lambda is genuinely free — the environment is a pinned
// static — so charging it would be a false positive against the shipped lowering,
// not conservatism.
func TestClosureAlloc_CaptureFreeAllowed(t *testing.T) {
	src := `
let f = noalloc (x: i64) -> i64 => {
    let inc = (n: i64) -> i64 => n + 1
    inc(x)
}`
	assertPurityCount(t, checkPurity(t, src), 0)
}

// Receiving and calling a callback allocates nothing — the prelude's combinators are
// `pure noalloc` and live on exactly this shape, so charging it would break the
// standard library.
func TestClosureAlloc_CallbackParameterAllowed(t *testing.T) {
	src := `
let apply = noalloc (f: (i64) -> i64, x: i64) -> i64 => f(x)`
	assertPurityCount(t, checkPurity(t, src), 0)
}

// An *escaping* capturing closure — returned out — is the case that allocates under
// both tiers, so this charge survives LSS unchanged.
func TestClosureAlloc_EscapingClosureRefused(t *testing.T) {
	src := `
let make = noalloc (n: i64) -> (i64) -> i64 => {
    (x: i64) -> i64 => x + n
}`
	assertBoundError(t, checkPurity(t, src), "lyra-E016")
}

// The charge travels through inference: an unannotated closure-maker infers
// EffectAlloc, so a `noalloc` caller of it is refused at the call.
func TestClosureAlloc_TransitiveThroughCallee(t *testing.T) {
	src := `
let make_adder = (n: i64) -> (i64) -> i64 => {
    (x: i64) -> i64 => x + n
}
let outer = noalloc (a: i64) -> i64 => {
    let f = make_adder(a)
    f(1)
}`
	assertBoundError(t, checkPurity(t, src), "lyra-E016")
}

// The trait-method ladder is a second copy of the same walk (hazard 8's standing
// concern), so the charge is pinned there separately.
func TestClosureAlloc_TraitMethodCopyCharges(t *testing.T) {
	src := `
struct Adder { base: i64 }
trait Apply { go: (Self, i64) -> i64 }
impl Apply for Adder {
  go = noalloc (self, x) => {
    let f = (n: i64) -> i64 => n + self.base
    f(x)
  }
}`
	assertBoundError(t, checkPurity(t, src), "lyra-E016")
}
