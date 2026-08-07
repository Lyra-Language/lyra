package typechecker_test

import (
	"strings"
	"testing"
)

const nonExhaustiveTuple = "match on tuple type is not exhaustive: add a wildcard `_ => ...` or catch-all arm"

// assertNoTupleExhaustivenessWarning fails when the tuple non-exhaustiveness diagnostic is
// present. Other diagnostics are ignored: these cases are about coverage, and a source that
// also has an unrelated error would otherwise fail for the wrong reason.
func assertNoTupleExhaustivenessWarning(t *testing.T, source string) {
	t.Helper()
	res := parseCollectAndCheck(t, source, false)
	for _, err := range res.errors {
		if strings.Contains(err.Message, nonExhaustiveTuple) {
			t.Fatalf("unexpected non-exhaustiveness warning; all diagnostics: %v", res.errors)
		}
	}
}

func assertTupleExhaustivenessWarning(t *testing.T, source string) {
	t.Helper()
	res := parseCollectAndCheck(t, source, false)
	for _, err := range res.errors {
		if strings.Contains(err.Message, nonExhaustiveTuple) {
			return
		}
	}
	t.Fatalf("expected a non-exhaustiveness warning; all diagnostics: %v", res.errors)
}

// Coverage spread across arms is coverage. This is the shape every multi-clause function
// desugars to, and it warned until 08/06 because the check asked only whether some single
// arm was irrefutable.
func TestMatchTuple_ConstructorsCoveredAcrossArms_NoWarning(t *testing.T) {
	assertNoTupleExhaustivenessWarning(t, `
data Maybe<t> = None | Some t
let apply = (self: Maybe<i64>, predicate: (i64) -> bool) -> bool => match (self, predicate) {
    (Some v, pred) => pred(v),
    (None, _) => false,
}`)
}

// The multi-clause spelling of the same function, which is what the prelude writes.
func TestMatchTuple_MultiClauseFunction_NoWarning(t *testing.T) {
	assertNoTupleExhaustivenessWarning(t, `
data Maybe<t> = None | Some t
let apply = (self: Maybe<i64>, predicate: (i64) -> bool) -> bool {
    (Some v, pred) => pred(v),
    (None, _) => false,
}`)
}

// Both columns enumerated, all four combinations present.
func TestMatchTuple_FullProductCovered_NoWarning(t *testing.T) {
	assertNoTupleExhaustivenessWarning(t, `
data Maybe<t> = None | Some t
let f = (a: Maybe<i64>, b: Maybe<i64>) -> i64 => match (a, b) {
    (Some x, Some y) => x + y,
    (Some x, None) => x,
    (None, Some y) => y,
    (None, None) => 0,
}`)
}

// **The case a per-column check gets wrong.** Every constructor appears in every column,
// but `(Some, Some)` matches no arm. Checking columns independently would call this
// exhaustive and silently drop a diagnostic for a match that traps at runtime — which is
// why this is a matrix.
func TestMatchTuple_PerColumnCoverageIsNotExhaustive_Warns(t *testing.T) {
	assertTupleExhaustivenessWarning(t, `
data Maybe<t> = None | Some t
let f = (a: Maybe<i64>, b: Maybe<i64>) -> i64 => match (a, b) {
    (Some x, None) => x,
    (None, Some y) => y,
}`)
}

// One combination missing from an otherwise complete product.
func TestMatchTuple_MissingOneCombination_Warns(t *testing.T) {
	assertTupleExhaustivenessWarning(t, `
data Maybe<t> = None | Some t
let f = (a: Maybe<i64>, b: Maybe<i64>) -> i64 => match (a, b) {
    (Some x, Some y) => x + y,
    (Some x, None) => x,
    (None, Some y) => y,
}`)
}

// A column of a type whose values cannot be enumerated is covered only by an arm that binds
// it whole. Here every `i64` is left to the binding in each arm, so the match is complete.
func TestMatchTuple_OpaqueColumnBoundWhole_NoWarning(t *testing.T) {
	assertNoTupleExhaustivenessWarning(t, `
data Maybe<t> = None | Some t
let f = (a: Maybe<i64>, n: i64) -> i64 => match (a, n) {
    (Some x, k) => x + k,
    (None, k) => k,
}`)
}

// Testing a value in an unenumerable column leaves the rest of that column uncovered.
func TestMatchTuple_OpaqueColumnTested_Warns(t *testing.T) {
	assertTupleExhaustivenessWarning(t, `
data Maybe<t> = None | Some t
let f = (a: Maybe<i64>, n: i64) -> i64 => match (a, n) {
    (Some x, 0) => x,
    (None, _) => 0,
}`)
}

// bool is enumerable, so both values across arms is exhaustive without a wildcard.
func TestMatchTuple_BoolColumnCovered_NoWarning(t *testing.T) {
	assertNoTupleExhaustivenessWarning(t, `
let f = (a: bool, b: bool) -> i64 => match (a, b) {
    (true, true) => 3,
    (true, false) => 2,
    (false, true) => 1,
    (false, false) => 0,
}`)
}

func TestMatchTuple_BoolColumnPartiallyCovered_Warns(t *testing.T) {
	assertTupleExhaustivenessWarning(t, `
let f = (a: bool, b: bool) -> i64 => match (a, b) {
    (true, true) => 3,
    (false, false) => 0,
}`)
}

// Nested constructor patterns recurse: the payload of `Some` is itself a column.
func TestMatchTuple_NestedConstructorCovered_NoWarning(t *testing.T) {
	assertNoTupleExhaustivenessWarning(t, `
data Maybe<t> = None | Some t
let f = (a: Maybe<Maybe<i64>>, b: bool) -> i64 => match (a, b) {
    (Some (Some x), _) => x,
    (Some None, _) => 1,
    (None, _) => 0,
}`)
}

func TestMatchTuple_NestedConstructorMissing_Warns(t *testing.T) {
	assertTupleExhaustivenessWarning(t, `
data Maybe<t> = None | Some t
let f = (a: Maybe<Maybe<i64>>, b: bool) -> i64 => match (a, b) {
    (Some (Some x), _) => x,
    (None, _) => 0,
}`)
}

// A guarded arm may fail, so it covers nothing on its own — unchanged by the matrix.
func TestMatchTuple_GuardedArmDoesNotCover_Warns(t *testing.T) {
	assertTupleExhaustivenessWarning(t, `
data Maybe<t> = None | Some t
let f = (a: Maybe<i64>, b: bool) -> i64 => match (a, b) {
    (Some x, _) if x > 0 => x,
    (None, _) => 0,
}`)
}

// A single irrefutable arm still covers everything — the case the old per-arm check got
// right, kept so a regression cannot trade one for the other.
func TestMatchTuple_IrrefutableArm_NoWarning(t *testing.T) {
	assertNoTupleExhaustivenessWarning(t, `
data Maybe<t> = None | Some t
let f = (a: Maybe<i64>, b: bool) -> i64 => match (a, b) {
    (Some x, true) => x,
    (p, q) => 0,
}`)
}

// A wildcard arm over the whole tuple, likewise.
func TestMatchTuple_WildcardArm_NoWarning(t *testing.T) {
	assertNoTupleExhaustivenessWarning(t, `
data Maybe<t> = None | Some t
let f = (a: Maybe<i64>, b: bool) -> i64 => match (a, b) {
    (Some x, true) => x,
    _ => 0,
}`)
}

// A three-column product, to check the recursion is not two-deep by accident.
func TestMatchTuple_ThreeColumns_NoWarning(t *testing.T) {
	assertNoTupleExhaustivenessWarning(t, `
let f = (a: bool, b: bool, c: bool) -> i64 => match (a, b, c) {
    (true, _, _) => 1,
    (false, true, _) => 2,
    (false, false, true) => 3,
    (false, false, false) => 4,
}`)
}

func TestMatchTuple_ThreeColumnsMissingOne_Warns(t *testing.T) {
	assertTupleExhaustivenessWarning(t, `
let f = (a: bool, b: bool, c: bool) -> i64 => match (a, b, c) {
    (true, _, _) => 1,
    (false, true, _) => 2,
    (false, false, true) => 3,
}`)
}
