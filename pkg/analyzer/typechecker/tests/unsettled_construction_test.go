package typechecker_test

import (
	"strings"
	"testing"
)

// An applied constructor whose payload leaves a parameter open — `Pick(5)` says nothing of
// `b` — with no context anywhere is lyra-E073, as a bare `None` already was. Until 09/28 it
// type-checked and failed in the backend ("type variable t has no concrete type here").

const unsettledTypes = `
  data Either<a, b> = Pick(a) | Other(b)
  data Box<t> = Wrap(t) | Empty
`

func assertOnlyUninferable(t *testing.T, res checkResult, wantOpen string) {
	t.Helper()
	if len(res.errors) != 1 {
		t.Fatalf("want one lyra-E073; got %v", res.errors)
	}
	msg := res.errors[0].Message
	if !strings.Contains(msg, "cannot tell what `Pick(…)` builds") ||
		!strings.Contains(msg, "solves Either's "+wantOpen) {
		t.Errorf("want E073 naming %s; got %q", wantOpen, msg)
	}
}

func TestUnsettledConstruction_BareBinding(t *testing.T) {
	res := parseCollectAndCheck(t, unsettledTypes+`
  let r = Pick(5)
	`, false)
	assertOnlyUninferable(t, res, "`b`")
}

// A later use does not settle the binding — the construction is where the type goes.
func TestUnsettledConstruction_LaterUseDoesNotSettleIt(t *testing.T) {
	res := parseCollectAndCheck(t, unsettledTypes+`
  let take = (x: Either<i64, string>) -> i64 => 0
  let r = Pick(5)
  let n = take(r)
	`, false)
	if len(res.errors) == 0 || !strings.Contains(res.errors[0].Message, "cannot tell what `Pick(…)` builds") {
		t.Errorf("want E073 at the construction first; got %v", res.errors)
	}
}

// Nested with no context: the inner construction is the one left bare.
func TestUnsettledConstruction_Nested(t *testing.T) {
	res := parseCollectAndCheck(t, unsettledTypes+`
  let x = Wrap(Pick(5))
	`, false)
	assertOnlyUninferable(t, res, "`b`")
}

// Every settled spelling stays clean: annotation, argument, return, generic body, and a
// turbofish.
func TestUnsettledConstruction_SettledSpellingsAreClean(t *testing.T) {
	res := parseCollectAndCheck(t, unsettledTypes+`
  let a: Either<i64, string> = Pick(5)
  let take = (x: Either<i64, string>) -> i64 => 0
  let b = take(Pick(5))
  let c = () -> Either<i64, bool> => Pick(1)
  let lift<t> = (x: t) -> Box<Either<t, string>> => Wrap(Pick(x))
  let d = Pick::<u8, string>(200)
	`, false)
	assertNoErrors(t, res)
}

// A turbofish binds the parameters outright: its payload is checked against them, and it
// must name every one.
func TestConstructorTurbofish_IsChecked(t *testing.T) {
	res := parseCollectAndCheck(t, unsettledTypes+`
  let a = Pick::<u8, string>(300)
  let b = Pick::<i64, string>("x")
  let c = Pick::<i64>(1)
	`, false)
	assertErrorsAre(t, res,
		"Pick: literal value 300 overflows u8",
		"Pick: cannot assign string to i64",
		"Pick::<…> takes 2 type argument(s), one per parameter of Either, but was given 1")
}
