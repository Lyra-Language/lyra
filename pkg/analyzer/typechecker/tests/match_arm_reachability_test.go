package typechecker_test

import "testing"

// lyra-W021: an arm an earlier unguarded arm already covers unconditionally.
//
// The rule turns on the *earlier* arm only: it must be unguarded, and its pattern must bind
// without testing. Both halves are load-bearing, and the negative cases below are what pin
// them — a guard may fail and a test may fail, either of which leaves the later arm live.

// A second arm for a constructor an earlier bind-only arm already takes.
//
// This is the half with a miscompile behind it. The backend emitted both as cases of one
// LLVM `switch`, which clang refuses ("duplicate case value in switch"); it now drops the
// later arm, matching first-match-wins. The warning is what keeps that drop from being
// silent — a second `Wrap` written where `Nil` was meant would otherwise compile and run
// with one branch quietly missing.
func TestTypeCheck_UnreachableArm_DuplicateConstructor_Warning(t *testing.T) {
	res := parseCollectAndCheck(t, `
data D = Wrap(i64) | Nil
let d: D = Wrap(1)
match d {
  Wrap(a) => a,
  Wrap(b) => b + 100,
  Nil => 0,
}
`, false)
	assertErrorsAre(t, res,
		"unreachable match arm: constructor Wrap is already matched unconditionally by the arm at 5:3, so this arm can never run")
}

// A nullary constructor repeated. Its sub-pattern is nil, which is a binding leaf and so
// irrefutable — the same rule, with nothing to bind.
func TestTypeCheck_UnreachableArm_DuplicateNullaryConstructor_Warning(t *testing.T) {
	res := parseCollectAndCheck(t, `
data D = Wrap(i64) | Nil
let d: D = Nil
match d {
  Nil => 0,
  Nil => 1,
  Wrap(a) => a,
}
`, false)
	assertErrorsAre(t, res,
		"unreachable match arm: constructor Nil is already matched unconditionally by the arm at 5:3, so this arm can never run")
}

// An irrefutable arm covers *everything* after it, not just its own shape — every later arm
// is reported, each naming the arm that killed it.
func TestTypeCheck_UnreachableArm_AfterCatchAll_Warning(t *testing.T) {
	res := parseCollectAndCheck(t, `
let n = 5
match n {
  x => x,
  7 => 70,
  _ => 99,
}
`, false)
	assertErrorsAre(t, res,
		"unreachable match arm: the arm at 4:3 matches every value, so this arm can never run",
		"unreachable match arm: the arm at 4:3 matches every value, so this arm can never run")
}

// A destructure that only binds is irrefutable too — there is no tag to discriminate on a
// tuple, so `(x, y)` matches every value and the conventional trailing `_` is dead.
func TestTypeCheck_UnreachableArm_AfterIrrefutableDestructure_Warning(t *testing.T) {
	res := parseCollectAndCheck(t, `
let p = (1, 2)
match p {
  (x, y) => x + y,
  _ => 0,
}
`, false)
	assertErrorsAre(t, res,
		"unreachable match arm: the arm at 4:3 matches every value, so this arm can never run")
}

// ── the negatives, which are where the rule earns its narrowness ──────────────

// A *test* in the earlier arm's payload leaves the later arm reachable. `Wrap(0)` matches
// only some `Wrap`s, so `Wrap(b)` still runs — condemning it would be a false positive on
// correct code, and dropping it in the backend would be a miscompile.
func TestTypeCheck_UnreachableArm_RefutablePayload_NoWarning(t *testing.T) {
	res := parseCollectAndCheck(t, `
data D = Wrap(i64) | Nil
let d: D = Wrap(1)
match d {
  Wrap(0) => 100,
  Wrap(b) => b,
  Nil => 0,
}
`, false)
	assertNoErrors(t, res)
}

// A guard on the earlier arm may fail, so it covers nothing.
func TestTypeCheck_UnreachableArm_GuardedEarlierArm_NoWarning(t *testing.T) {
	res := parseCollectAndCheck(t, `
data D = Wrap(i64) | Nil
let d: D = Wrap(1)
match d {
  Wrap(a) if a > 100 => 1,
  Wrap(b) => b,
  Nil => 0,
}
`, false)
	assertNoErrors(t, res)
}

// A guard on the *later* arm is irrelevant — an arm that never runs never runs its guard —
// so this one is still reported. The asymmetry is the rule, not an oversight.
func TestTypeCheck_UnreachableArm_GuardedLaterArm_StillWarns(t *testing.T) {
	res := parseCollectAndCheck(t, `
data D = Wrap(i64) | Nil
let d: D = Wrap(1)
match d {
  Wrap(a) => a,
  Wrap(b) if b > 100 => 1,
  Nil => 0,
}
`, false)
	assertErrorsAre(t, res,
		"unreachable match arm: constructor Wrap is already matched unconditionally by the arm at 5:3, so this arm can never run")
}

// Distinct constructors cover nothing of each other.
func TestTypeCheck_UnreachableArm_DistinctConstructors_NoWarning(t *testing.T) {
	res := parseCollectAndCheck(t, `
data D = Wrap(i64) | Nil
let d: D = Wrap(1)
match d {
  Wrap(a) => a,
  Nil => 0,
}
`, false)
	assertNoErrors(t, res)
}

// **The check sits out when the match has an error in it.** `(x, y, z)` against a 2-tuple is
// an arity error, and it binds three names — so the shape-only rule reads it as irrefutable
// and would condemn the `_` that follows, which is the arm keeping the match exhaustive
// while the real mistake is fixed. Only the arity error is reported.
func TestTypeCheck_UnreachableArm_NotReportedOnAnIllTypedMatch(t *testing.T) {
	res := parseCollectAndCheck(t, `
let p = (1, 2)
match p {
  (x, y, z) => 0,
  _ => 1,
}
`, false)
	assertErrorsAre(t, res, "tuple pattern has 3 element(s) but scrutinee has 2")
}

// A duplicate *literal* belongs to the older checkDuplicateMatchArms and must not be
// reported twice — a literal is neither irrefutable nor a constructor, so it is invisible
// to this pass by construction.
func TestTypeCheck_UnreachableArm_DuplicateLiteralIsReportedOnlyOnce(t *testing.T) {
	res := parseCollectAndCheck(t, `
let n = 5
match n {
  1 => 1,
  1 => 2,
  _ => 3,
}
`, false)
	assertErrorsAre(t, res,
		"duplicate match arm: pattern 1 is already covered by an earlier arm")
}
