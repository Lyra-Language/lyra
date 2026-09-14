package typechecker_test

import "testing"

// A range pattern may be bounded by a `const` (09/13). The typechecker folds the bound to
// its literal in place (range_pattern_consts.go), so everything that reads a bound — the
// value check, exhaustiveness, overlap — sees the number it stands for.

// Exhaustiveness reads a folded bound: `..<LOW`, `LOW..<=HIGH`, `21..` covers a u8, and a
// const defined in terms of another folds too.
func TestPatternConstBound_CoversTheType(t *testing.T) {
	assertNoErrors(t, parseCollectAndCheck(t, `
const LOW = 10
const HIGH = LOW * 2
let f = (x: u8) -> string => match x {
  ..<LOW => "a",
  LOW..<=HIGH => "b",
  21.. => "c",
}`, false))
}

// The value check reads it too: a const that does not fit the scrutinee is lyra-E048,
// exactly as the literal would be.
func TestPatternConstBound_TooWideForTheScrutinee(t *testing.T) {
	res := parseCollectAndCheck(t, `
const BIG = 300
let f = (x: u8) -> u8 => match x {
  0..<=BIG => 1,
  _ => 3,
}`, false)
	assertErrorsAre(t, res, "pattern 300 does not fit the scrutinee type u8, so this arm can never match")
}

// Overlap is judged on the folded values.
func TestPatternConstBound_Overlap(t *testing.T) {
	res := parseCollectAndCheck(t, `
const LOW = 10
let f = (x: i64) -> i64 => match x {
  0..<LOW => 1,
  5..<LOW => 2,
  _ => 3,
}`, false)
	assertErrorsAre(t, res, "overlapping match arm: this range overlaps with a previous arm")
}

// A negative const folds to a negated literal, the shape a written `-5` has.
func TestPatternConstBound_Negative(t *testing.T) {
	assertNoErrors(t, parseCollectAndCheck(t, `
const NEG = -5
let f = (x: i8) -> i64 => match x {
  ..<NEG => 1,
  NEG..<=127 => 2,
}`, false))
}

// Reached through the destructuring walk as well as the match: a bound nested in a
// payload, and one in `if let`.
func TestPatternConstBound_NestedAndIfLet(t *testing.T) {
	assertNoErrors(t, parseCollectAndCheck(t, `
data Maybe = None | Some(i64)
const LOW = 10
const HIGH = 20
let f = (m: Maybe) -> i64 => {
  if let Some(LOW..<=HIGH) = m { return 1 }
  match m {
    Some(LOW..<=HIGH) => 2,
    Some(_) => 3,
    None => 0,
  }
}`, false))
}

// The grammar admits any SCREAMING_CASE name; one that is not a `const` is refused.
func TestPatternConstBound_NotAConst(t *testing.T) {
	res := parseCollectAndCheck(t, `
let f = (x: i64) -> i64 => match x {
  UNDEFINED.. => 1,
  _ => 0,
}`, false)
	assertErrorsAre(t, res, "a range-pattern bound must be a number or a `const`; UNDEFINED is not a `const`")
}

// A const that is not a number cannot be a bound.
func TestPatternConstBound_NotANumber(t *testing.T) {
	res := parseCollectAndCheck(t, `
const NAME = "x"
let f = (x: i64) -> i64 => match x {
  0..<NAME => 1,
  _ => 0,
}`, false)
	assertErrorsAre(t, res, "range-pattern bound NAME must be a compile-time number; its `const` initializer does not fold to one")
}

// A float bound on an integer scrutinee has no integer to compare against. It reached the
// backend as an unsupported bound before, written as a literal; a float const is the
// easier way to write it.
func TestPatternConstBound_FloatOnAnInteger(t *testing.T) {
	res := parseCollectAndCheck(t, `
const HALF = 0.5
let f = (x: i64) -> i64 => match x {
  HALF.. => 1,
  0.5.. => 2,
  _ => 0,
}`, false)
	assertErrorsAre(t, res,
		"range pattern bound is not an integer, but the scrutinee is i64",
		"range pattern bound is not an integer, but the scrutinee is i64")
}

// A float const bounds a float range.
func TestPatternConstBound_FloatOnAFloat(t *testing.T) {
	assertNoErrors(t, parseCollectAndCheck(t, `
const HALF = 0.5
let f = (x: f64) -> i64 => match x {
  0.0..<HALF => 1,
  _ => 0,
}`, false))
}
