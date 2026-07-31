package checker_test

import (
	"strings"
	"testing"
)

// Trait-impl methods are effect-polymorphic over their callback parameters, exactly as free
// functions are. Until this, `methodEffects` charged a call through a method parameter the
// full AllEffects taint, so a method taking a callback was as poisoned as every function was
// before effect polymorphism landed — and the taint spread to its callers.

const applyTrait = `
trait Apply { apply: (Self, () -> i64) -> i64 }
data Box = Empty | Full(i64)
impl Apply for Box {
  apply = (self, f) => f()
}`

// A pure caller supplying a pure callback. The method's body calls `f()`, which used to make
// every call to it impure regardless of what was passed.
func TestTraitMethodEffects_PureCallerPureCallback(t *testing.T) {
	src := applyTrait + `
let good = pure (b: Box) -> i64 => b.apply(() -> i64 => 7)`
	assertPurityCount(t, checkPurity(t, src), 0)
}

// And the impure callback is still caught — with a message that distinguishes "the method is
// impure" from "what you handed it is", since the fix differs.
func TestTraitMethodEffects_PureCallerImpureCallback(t *testing.T) {
	src := applyTrait + `
var log = 0
let bump = () -> i64 => { log = 1  0 }
let bad = pure (b: Box) -> i64 => b.apply(bump)`
	errs := checkPurity(t, src)
	assertPurityCount(t, errs, 1)
	if len(errs) == 1 && !strings.Contains(errs[0].Message, "impure callback argument") {
		t.Errorf("expected the callback to be blamed, not the method: %q", errs[0].Message)
	}
}

// A method impure in its own body is still impure at every call site, callbacks or not —
// polymorphism is about the callback, not an amnesty.
func TestTraitMethodEffects_BodyEffectsAreStillCharged(t *testing.T) {
	src := `
trait Log { run: (Self, () -> i64) -> i64 }
data Box = Empty | Full(i64)
var log = 0
impl Log for Box {
  run = (self, f) => { log = 1  f() }
}
let bad = pure (b: Box) -> i64 => b.run(() -> i64 => 7)`
	errs := checkPurity(t, src)
	assertPurityCount(t, errs, 1)
	if len(errs) == 1 && !strings.Contains(errs[0].Message, "non-pure trait method") {
		t.Errorf("a method impure in its own body should be named: %q", errs[0].Message)
	}
}

// The receiver is signature parameter 0 but sits *outside* `call.Arguments`, so signature
// index i is `Arguments[i-1]`. Getting that wrong checks each callback against the argument
// one place over — silently, since two function-typed arguments type-check interchangeably.
// Two callbacks in different positions is what makes the offset observable.
func TestTraitMethodEffects_ReceiverOffsetIsCorrect(t *testing.T) {
	src := `
trait Two { both: (Self, () -> i64, () -> i64) -> i64 }
data Box = Empty | Full(i64)
impl Two for Box {
  both = (self, f, g) => f() + g()
}
var log = 0
let bump = () -> i64 => { log = 1  0 }
let firstImpure  = pure (b: Box) -> i64 => b.both(bump, () -> i64 => 1)
let secondImpure = pure (b: Box) -> i64 => b.both(() -> i64 => 1, bump)
let neitherImpure = pure (b: Box) -> i64 => b.both(() -> i64 => 1, () -> i64 => 2)`
	// One diagnostic each for the two calls carrying an impure callback, none for the third.
	assertPurityCount(t, checkPurity(t, src), 2)
}

// A trait *signature* may declare a bound on a method's callback parameter. That makes the
// method's cost known from the signature, so an impl is pure for every caller — and the
// bound binds callers, including impure ones, since it belongs to the signature.
func TestTraitMethodEffects_DeclaredBoundInSignature(t *testing.T) {
	src := `
trait Apply { apply: (Self, pure () -> i64) -> i64 }
data Box = Empty | Full(i64)
impl Apply for Box {
  apply = (self, f) => f()
}
var log = 0
let bump = () -> i64 => { log = 1  0 }
let fine = pure (b: Box) -> i64 => b.apply(() -> i64 => 7)
let bad  = (b: Box) -> i64 => b.apply(bump)`
	errs := checkPurity(t, src)
	assertPurityCount(t, errs, 1)
	if len(errs) == 1 && !strings.Contains(errs[0].Message, "must be `pure`") {
		t.Errorf("expected the declared bound to be named: %q", errs[0].Message)
	}
}

// A method with no callback parameters is unaffected — the common case must not have gained
// a cost or a diagnostic.
func TestTraitMethodEffects_FirstOrderMethodUnaffected(t *testing.T) {
	src := `
trait Show { show: (Self) -> i64 }
data Box = Empty | Full(i64)
impl Show for Box {
  show = (self) => 1
}
let good = pure (b: Box) -> i64 => b.show()`
	assertPurityCount(t, checkPurity(t, src), 0)
}
