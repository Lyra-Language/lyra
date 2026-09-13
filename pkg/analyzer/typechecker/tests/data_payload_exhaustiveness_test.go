package typechecker_test

import (
	"strings"
	"testing"
)

// **A `data` match covers a constructor only where it covers that constructor's payloads.**
// Coverage was a set of constructor names until 09/13, so `Some(0)` beside `None` was
// exhaustive to the checker and trapped on `Some(5)` at run time. Each constructor is now
// the pattern matrix specialized by it, nested tuples and structs included.
func TestDataExhaustiveness_PartialPayloads(t *testing.T) {
	const decls = `
data Opt<t> = Some t | None
data List = Cons(i64, shared List) | Nil
struct Pt { x: Opt<i64>, y: i64 }
data Sh = Dot(Pt) | Nothing
`
	for _, c := range []struct{ name, fn, want string }{
		{"a literal payload", `let f = (m: Opt<i64>) -> i64 => match m { Some(0) => 1, None => 2 }`,
			"match on Opt is not exhaustive: not every payload of Some is matched (add `Some _ => …`)"},
		{"a nested constructor", `let f = (m: Opt<Opt<i64>>) -> i64 => match m { Some(Some(n)) => n, None => 2 }`,
			"not every payload of Some is matched"},
		{"inside a tuple payload", `let f = (m: Opt<(i64, Opt<i64>)>) -> i64 => match m { Some((a, Some(b))) => a + b, None => 0 }`,
			"not every payload of Some is matched"},
		{"a recursive type", `let f = (l: shared List) -> i64 => match l { Cons(_, Cons(x, _)) => x, Nil => 0 }`,
			"not every payload of Cons is matched"},
		{"inside a struct payload", `let f = (s: Sh) -> i64 => match s { Dot(Pt { x: Some(v), y }) => v + y, Nothing => 0 }`,
			"not every payload of Dot is matched"},
		{"missing and partial together", `let f = (m: Opt<i64>) -> i64 => match m { Some(0) => 1 }`,
			"missing constructors: None; not every payload of Some is matched"},
	} {
		t.Run(c.name, func(t *testing.T) {
			res := parseCollectAndCheck(t, decls+c.fn, false)
			found := false
			for _, e := range res.errors {
				found = found || strings.Contains(e.Message, c.want)
			}
			if !found {
				t.Errorf("want an error containing %q, got %v", c.want, res.errors)
			}
		})
	}
}

// Coverage spread across arms is still coverage, at any depth.
func TestDataExhaustiveness_CoveredAcrossArms(t *testing.T) {
	const decls = `
data Opt<t> = Some t | None
data List = Cons(i64, shared List) | Nil
struct Pt { x: Opt<i64>, y: i64 }
data Sh = Dot(Pt) | Nothing
`
	for _, fn := range []string{
		`let f = (m: Opt<i64>) -> i64 => match m { Some(0) => 1, Some(n) => n, None => 2 }`,
		`let f = (m: Opt<Opt<i64>>) -> i64 => match m { Some(Some(n)) => n, Some(None) => 0, None => 2 }`,
		`let f = (m: Opt<(i64, Opt<i64>)>) -> i64 => match m { Some((a, Some(b))) => a + b, Some((a, None)) => a, None => 0 }`,
		`let f = (l: shared List) -> i64 => match l { Cons(_, Cons(x, _)) => x, Cons(_, Nil) => 0, Nil => 0 }`,
		`let f = (s: Sh) -> i64 => match s { Dot(Pt { x: Some(v), y }) => v + y, Dot(Pt { x: None, y }) => y, Nothing => 0 }`,
		`let f = (m: Opt<bool>) -> i64 => match m { Some(true) => 1, Some(false) => 2, None => 0 }`,
	} {
		for _, e := range parseCollectAndCheck(t, decls+fn, false).errors {
			if strings.Contains(e.Message, "not exhaustive") {
				t.Errorf("%s: unexpected %q", fn, e.Message)
			}
		}
	}
}
