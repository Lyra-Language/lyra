package typechecker_test

import "testing"

// Pattern literals are value-checked against the type they are compared to
// (lyra-E048, 08/13) — the truncation family the audit's second sweep found. Until
// then a pattern literal was checked for *kind* and never for *value*, and the
// backend lowered the constant at the scrutinee's width, so these were not merely
// dead arms: `match x { 300 => … }` on a u8 **matched 44**, `{ -1 => … }` matched
// 255, and `Some(300)` on a `Maybe<u8>` matched `Some(44)` — a silent wrong branch.
// Every value here is a compile-time constant by grammar, so the ladder's provable
// rung is the whole ladder.

func TestPatternRange_BareLiteralTooWide(t *testing.T) {
	res := parseCollectAndCheck(t, `
let f = (x: u8) -> u8 => match x {
  300 => 1,
  _ => 3,
}`, false)
	assertErrorsAre(t, res, "pattern 300 does not fit the scrutinee type u8, so this arm can never match")
}

// The negative-on-unsigned case — the negative-indexing bug's spirit in pattern
// position, found hours after that was removed from indexing.
func TestPatternRange_NegativeOnUnsigned(t *testing.T) {
	res := parseCollectAndCheck(t, `
let f = (x: u8) -> u8 => match x {
  -1 => 1,
  _ => 3,
}`, false)
	assertErrorsAre(t, res, "pattern -1 does not fit the scrutinee type u8, so this arm can never match")
}

// Each range bound is checked; a range wholly outside reports both.
func TestPatternRange_RangeBoundsTooWide(t *testing.T) {
	res := parseCollectAndCheck(t, `
let f = (x: u8) -> u8 => match x {
  1000..<=2000 => 2,
  _ => 3,
}`, false)
	assertErrorsAre(t, res,
		"pattern 1000 does not fit the scrutinee type u8, so this arm can never match",
		"pattern 2000 does not fit the scrutinee type u8, so this arm can never match")
}

// An exclusive end names a position, not a value: `0..<256` on a u8 is exactly
// `0..<=255` — the full range, which the exhaustiveness analysis already reads it
// as — so the last *included* value is what must fit. The grace is exactly one.
func TestPatternRange_ExclusiveEndOnePastMaxOk(t *testing.T) {
	assertNoErrors(t, parseCollectAndCheck(t, `
let f = (x: u8) -> string => match x {
  0..<256 => "ok",
}`, false))
}

func TestPatternRange_ExclusiveEndTwoPastMaxRefused(t *testing.T) {
	res := parseCollectAndCheck(t, `
let f = (x: u8) -> u8 => match x {
  0..<257 => 1,
  _ => 3,
}`, false)
	assertErrorsAre(t, res, "pattern 257 does not fit the scrutinee type u8, so this arm can never match")
}

// A payload sub-pattern is checked against the constructor's *substituted* field
// type — `Opt<u8>` narrows the payload to u8, not to the parameter t.
func TestPatternRange_DataPayload(t *testing.T) {
	res := parseCollectAndCheck(t, `data Opt<t> = ONone | OSome(t)
let f = (m: Opt<u8>) -> u8 => match m {
  OSome(300) => 1,
  _ => 3,
}`, false)
	assertErrorsAre(t, res, "pattern 300 does not fit the scrutinee type u8, so this arm can never match")
}

// A tuple scrutinee checks element-wise.
func TestPatternRange_TupleElement(t *testing.T) {
	res := parseCollectAndCheck(t, `
let f = (p: (u8, u8)) -> u8 => match p {
  (300, 1) => 1,
  _ => 3,
}`, false)
	assertErrorsAre(t, res, "pattern 300 does not fit the scrutinee type u8, so this arm can never match")
}

// The constraint half of "the check follows the type": a value the newtype's range
// excludes is a dead arm too, reported against the constraint rather than the width.
func TestPatternRange_NewtypeConstraint(t *testing.T) {
	res := parseCollectAndCheck(t, `newtype Percent = u8 where range(0..<=100)
let f = (p: Percent) -> u8 => match p {
  200 => 9,
  _ => 2,
}`, false)
	assertErrorsAre(t, res, "pattern 200 is outside the range 0..<=100 of Percent, so this arm can never match")
}

// A newtype scrutinee now reaches the numeric kind policing at all — until 08/13 it
// matched none of checkMatchExpr's kind branches, so its arms were never checked and
// its match never exhaustiveness-tested. The wrong-kind literal is the pin.
func TestPatternRange_NewtypeScrutineeKindPoliced(t *testing.T) {
	res := parseCollectAndCheck(t, `newtype Percent = u8 where range(0..<=100)
let f = (p: Percent) -> u8 => match p {
  "fifty" => 1,
  _ => 2,
}`, false)
	assertErrorsAre(t, res, `literal pattern '"fifty"' is not an integer type`)
}

// The boundaries themselves are values, and valid ones.
func TestPatternRange_BoundariesOk(t *testing.T) {
	assertNoErrors(t, parseCollectAndCheck(t, `
let f = (x: u8) -> u8 => match x {
  0 => 1,
  255 => 2,
  _ => 3,
}
let g = (x: i8) -> u8 => match x {
  -128 => 1,
  127 => 2,
  _ => 3,
}`, false))
}

// ── the return-position sibling ──────────────────────────────────────────────
//
// `() -> u8 => 300` returned 44 — tracked open since 08/08, fixed with this family
// as its expression-position member: the propagation deliberately leaves an
// unfitting literal untyped for a downstream site to report, and a return has no
// downstream, so checkReturnValue now makes the same call the decl sites make.

func TestReturnLiteral_TooWideRefused(t *testing.T) {
	res := parseCollectAndCheck(t, `let f = () -> u8 => 300`, false)
	assertErrorsAre(t, res, "f: literal value 300 overflows u8")
}

func TestReturnLiteral_NestedReturnRefused(t *testing.T) {
	res := parseCollectAndCheck(t, `
let f = (b: bool) -> u8 => {
  if b { return 300 }
  1
}`, false)
	assertErrorsAre(t, res, "f: literal value 300 overflows u8")
}

func TestReturnLiteral_NegativePastMinRefused(t *testing.T) {
	res := parseCollectAndCheck(t, `let f = () -> i8 => -129`, false)
	assertErrorsAre(t, res, "f: literal value -129 overflows i8")
}

func TestReturnLiteral_BoundaryOk(t *testing.T) {
	assertNoErrors(t, parseCollectAndCheck(t, `
let f = () -> u8 => 255
let g = () -> i8 => -128`, false))
}
