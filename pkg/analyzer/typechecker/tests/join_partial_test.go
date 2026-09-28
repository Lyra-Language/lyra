package typechecker_test

import (
	"strings"
	"testing"
)

// Branches that each solve part of a generic type join to the whole: `Pick(5)` solves `a`,
// `Other("x")` solves `b`, and together they are `Either<i64, string>`. Until 09/28 the
// join stayed the bare `Either` and nothing settled it (lyra-E073).

const joinTypes = `
  data Either<a, b> = Pick(a) | Other(b)
`

func TestJoinPartial_IfAndMatchSettleWithNoContext(t *testing.T) {
	res := parseCollectAndCheck(t, joinTypes+`
  let f = (c: bool, n: i64) -> i64 => {
    let r = if c { Pick(5) } else { Other("x") }
    let s = match n { 0 => Pick(1), 1 => Other("one"), _ => Pick(3) }
    let nested = if c { Pick(1) } else { if n > 0 { Other(true) } else { Pick(2) } }
    0
  }
	`, false)
	assertNoErrors(t, res)
}

// The join's instantiation rests on `5`'s default, so a context arriving afterwards still
// narrows it — and still checks it.
func TestJoinPartial_ContextStillNarrows(t *testing.T) {
	res := parseCollectAndCheck(t, joinTypes+`
  let ok = (c: bool) -> Either<u8, string> => if c { Pick(200) } else { Other("x") }
  let bad = (c: bool) -> Either<u8, string> => if c { Pick(300) } else { Other("x") }
	`, false)
	assertErrorsAre(t, res, "Pick: literal value 300 overflows u8")
}

// What no branch solves stays unsolved: a branch that never falls through contributes
// nothing, and a conflict merges nothing.
func TestJoinPartial_UnsolvedStaysRefused(t *testing.T) {
	res := parseCollectAndCheck(t, joinTypes+`
  let f = (c: bool) -> i64 => {
    let r = if c { Pick(1) } else { return 0 }
    0
  }
	`, false)
	if len(res.errors) != 1 || !strings.Contains(res.errors[0].Message, "nothing here solves Either's `b`") {
		t.Errorf("want one E073 naming b; got %v", res.errors)
	}
}

// **One level down and deeper**: `Wrap(Pick(1))` beside `Wrap(Other("x"))` joins to
// `Box<Either<i64, string>>` through the payloads, in an `if`, a `match` with a nullary
// arm, a branch that is itself an `if`, and two levels deep.
func TestJoinPartial_Nested(t *testing.T) {
	res := parseCollectAndCheck(t, joinTypes+`
  data Box<t> = Wrap(t) | Empty
  let f = (c: bool, n: i64) -> i64 => {
    let a = if c { Wrap(Pick(1)) } else { Wrap(Other("x")) }
    let b = match n { 0 => Wrap(Pick(1)), 1 => Empty, _ => Wrap(Other("y")) }
    let d = if c { Wrap(Pick(6)) } else { if n > 0 { Empty } else { Wrap(Other(true)) } }
    let e = if c { Wrap(Wrap(Pick(4))) } else { Wrap(Wrap(Other("z"))) }
    0
  }
	`, false)
	assertNoErrors(t, res)
}

// Nested, a context still narrows and checks the inner payload, and what no branch solves
// stays refused.
func TestJoinPartial_NestedContextAndUnsolved(t *testing.T) {
	res := parseCollectAndCheck(t, joinTypes+`
  data Box<t> = Wrap(t) | Empty
  let ok = (c: bool) -> Box<Either<u8, string>> => if c { Wrap(Pick(200)) } else { Wrap(Other("x")) }
  let bad = (c: bool) -> Box<Either<u8, string>> => if c { Wrap(Pick(300)) } else { Wrap(Other("x")) }
	`, false)
	assertErrorsAre(t, res, "Pick: literal value 300 overflows u8")

	res = parseCollectAndCheck(t, joinTypes+`
  data Box<t> = Wrap(t) | Empty
  let f = (c: bool) -> i64 => {
    let m = if c { Wrap(Pick(1)) } else { Wrap(Pick(2)) }
    0
  }
	`, false)
	for _, e := range res.errors {
		if !strings.Contains(e.Message, "nothing here solves Either's `b`") {
			t.Errorf("unexpected error: %s", e.Message)
		}
	}
	if len(res.errors) == 0 {
		t.Error("want E073 for the unsolved b")
	}
}
