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
