package checker_test

import (
	"strings"
	"testing"
)

// The receiver-offset hazard, guarded.
//
// A UFCS call — `m.or_else(f)` for `or_else(self, f)` — is **rewritten by the typechecker**
// so the receiver becomes the first argument. That is what keeps this pass correct without
// knowing UFCS exists: it indexes arguments positionally against the declaration's
// parameters (`callableParams`), so a receiver left outside `Arguments` would shift every
// index by one and each callback would be checked against the bound of the parameter to its
// right.
//
// The reason it needs a test rather than an argument: the failure is *silent*. Both the
// receiver and the callback are ordinary values, and a function-typed argument satisfies
// the wrong function-typed parameter just as well as the right one — so a shifted check
// reports nothing and the bound simply stops being enforced. These fail loudly if the
// desugar is ever traded for a receiver-aware index.

const ufcsBoundSource = `
data Opt<t> = Nil | Just(t)

let or_else = pure (self: Opt<i64>, f: pure () -> i64) -> i64 => match self {
  Just(v) => v,
  Nil => f(),
}
`

// The declared bound is enforced through the method spelling: an impure callback in the
// `f` slot is rejected exactly as it is when the call is written out.
func TestUFCS_DeclaredBoundEnforcedThroughMethodCall(t *testing.T) {
	src := ufcsBoundSource + `
var log = 0
let bump = () -> i64 => { log = 1  0 }
let caller = (m: Opt<i64>) -> i64 => m.or_else(bump)`
	errs := checkPurity(t, src)
	assertPurityCount(t, errs, 1)
	if len(errs) == 1 && !strings.Contains(errs[0].Message, "mutates state outside itself") {
		t.Errorf("the diagnostic should say what the argument does, got %q", errs[0].Message)
	}
}

// …and the call form reports the same thing, which is the point: the two spellings are one
// call. A shifted index would most likely diverge here first.
func TestUFCS_MethodAndCallFormAgree(t *testing.T) {
	method := checkPurity(t, ufcsBoundSource+`
var log = 0
let bump = () -> i64 => { log = 1  0 }
let caller = (m: Opt<i64>) -> i64 => m.or_else(bump)`)
	direct := checkPurity(t, ufcsBoundSource+`
var log = 0
let bump = () -> i64 => { log = 1  0 }
let caller = (m: Opt<i64>) -> i64 => or_else(m, bump)`)
	if len(method) != len(direct) {
		t.Fatalf("the two spellings must agree: method form gave %d error(s), call form %d", len(method), len(direct))
	}
}

// A pure callback through the method form is accepted — the control for the above. Without
// it, a pass that rejected *everything* would look like it was enforcing the bound.
func TestUFCS_PureCallbackThroughMethodCallIsAccepted(t *testing.T) {
	src := ufcsBoundSource + `
let caller = pure (m: Opt<i64>) -> i64 => m.or_else(() -> i64 => 41)`
	assertPurityCount(t, checkPurity(t, src), 0)
}

// The receiver itself is never treated as a callback slot. With the offset wrong in the
// other direction, the *receiver* would be checked against `f`'s bound and a perfectly
// ordinary value would be reported as an impure callback.
func TestUFCS_ReceiverIsNotCheckedAsACallback(t *testing.T) {
	src := ufcsBoundSource + `
let caller = pure (m: Opt<i64>) -> i64 => m.or_else(() -> i64 => 0)`
	if errs := checkPurity(t, src); len(errs) != 0 {
		t.Errorf("the receiver is a value, not a callback; got %v", errs)
	}
}
