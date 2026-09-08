package typechecker_test

import "testing"

// `gen`, `yield` and `Seq<t>`: the typechecker rung (09/08). A `gen` body is checked as
// a void body under the element its `-> Seq<t>` names; a `Seq` is a fourth source for
// `for-in` and the comprehension; `yield from` takes anything a loop walks.

func TestSeq_YieldIsCheckedAgainstTheDeclaredElement(t *testing.T) {
	res := parseCollectAndCheck(t, `
let g = pure gen () -> Seq<u8> => { yield 200 }
let bad = pure gen () -> Seq<u8> => { yield "no" }
let main = () -> void => { for x in g() { let y: u8 = x } }
`, false)
	assertErrorsAre(t, res, `bad: cannot yield string from a Seq<u8>`)
}

func TestSeq_GenFunctionMustDeclareItsSequence(t *testing.T) {
	res := parseCollectAndCheck(t, `
let g = pure gen () -> i64 => { yield 1 }
let main = () -> void => { }
`, false)
	assertErrorsAre(t, res, "g: a `gen` function declares what it yields as `-> Seq<t>`")
}

func TestSeq_IsASourceForLoopsAndComprehensions(t *testing.T) {
	res := parseCollectAndCheck(t, `
let g = pure gen () -> Seq<string> => { yield "a"; yield "b" }
let main = () -> void => {
  for s in g() { let n: i64 = s.len() }
  let all: []string = [s in g() | s]
  let lens: []i64 = [s in g() | s.len()]
}
`, false)
	assertNoErrors(t, res)
}

// The range is parenthesized because `yield from 0..<3` parses as `(yield from 0) ..< 3`
// — the `yield` forms bind tighter than a range operator (todo.md, 09/08).
func TestSeq_YieldFromTakesAnythingALoopWalks(t *testing.T) {
	res := parseCollectAndCheck(t, `
let g = pure gen () -> Seq<i64> => { yield 1 }
let h = pure gen (xs: []i64) -> Seq<i64> => {
  yield from g()
  yield from xs
  yield from (0..<3)
}
let wrong = pure gen () -> Seq<i64> => { yield from "abc" }
let main = () -> void => { }
`, false)
	assertErrorsAre(t, res, "wrong: cannot yield rune elements from a Seq<i64>")
}

func TestSeq_YieldHasNoValue(t *testing.T) {
	res := parseCollectAndCheck(t, `
let g = pure gen () -> Seq<i64> => { let v: i64 = yield 1 }
let main = () -> void => { }
`, false)
	assertErrorsAre(t, res, "v: cannot assign void to i64")
}

// A `gen` function's signature is a function returning a `Seq`, so a generic combinator
// over `Seq<t>` solves `t` from a call's result, and a later-declared function passed as
// a value is typed from its declaration (the order fix of the same day).
func TestSeq_CombinatorSolvesFromAGenCallAndALaterFunction(t *testing.T) {
	res := parseCollectAndCheck(t, `
let main = () -> void => {
  let kept = [x in keep_odd(g()) | x]
}
let keep_odd = pure gen (s: Seq<i64>) -> Seq<i64> => { for x in s { if x % 2 == 1 { yield x } } }
let g = pure gen () -> Seq<i64> => { yield 1; yield 2; yield 3 }
`, false)
	assertNoErrors(t, res)
}
