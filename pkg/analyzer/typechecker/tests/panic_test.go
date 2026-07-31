package typechecker_test

import "testing"

// `panic(msg)` has type `never`, the bottom type: assignable to everything, so it
// satisfies whatever the surrounding context needs a value of without inventing one.
// The match's type comes from the *other* arm.
func TestPanic_NeverSatisfiesAnyArmType(t *testing.T) {
	// These tests run without a prelude, so the data type is declared locally.
	res := parseCollectAndCheck(t, `
data Opt<t> = Nil | Just(t)
let expect = (m: Opt<i64>, msg: string) -> i64 => match m {
  Just(v) => v,
  Nil => panic(msg),
}`, false)
	assertNoErrors(t, res)
}

// The same in the other order, and at a non-numeric type: `never` joins with whatever
// the sibling arm produces, from either side of the fold (branchCommonType tries both).
func TestPanic_NeverJoinsFromEitherSide(t *testing.T) {
	res := parseCollectAndCheck(t, `
data Opt<t> = Nil | Just(t)
let first = (m: Opt<string>) -> string => match m {
  Nil => panic("empty"),
  Just(v) => v,
}
let second = (b: bool) -> string => if b { panic("no") } else { "ok" }`, false)
	assertNoErrors(t, res)
}

// A whole function may be `never` — every path panics. The declared return type is
// satisfied because `never` is assignable to it.
func TestPanic_EveryPathPanics(t *testing.T) {
	res := parseCollectAndCheck(t, `let boom = (why: string) -> i64 => panic(why)`, false)
	assertNoErrors(t, res)
}

// The message is a runtime string, so an interpolated one is fine — that is the case
// that makes a panic message worth writing.
func TestPanic_InterpolatedMessage(t *testing.T) {
	res := parseCollectAndCheck(t, `
let at = (i: i64) -> i64 => if i < 0 { panic("negative index ${i}") } else { i }`, false)
	assertNoErrors(t, res)
}

func TestPanic_MessageMustBeAString(t *testing.T) {
	res := parseCollectAndCheck(t, `let f = () -> i64 => panic(42)`, false)
	assertErrorsAre(t, res, "panic: message must be a string, got i64")
}

func TestPanic_Arity(t *testing.T) {
	res := parseCollectAndCheck(t, `let f = () -> i64 => panic("a", "b")`, false)
	assertErrorsAre(t, res, "panic: expected 1 argument(s), got 2")
}

// Resolved after scope resolution misses, exactly like print/println — so a user
// binding of the same name wins, and adding this builtin cannot break a program that
// already had its own `panic`.
func TestPanic_UserBindingShadowsTheBuiltin(t *testing.T) {
	res := parseCollectAndCheck(t, `
let panic = (n: i64) -> i64 => n * 2
let f = () -> i64 => panic(21)`, false)
	assertNoErrors(t, res)
}

// Nothing is assignable *to* `never` — it is the bottom of the lattice, not the top.
// There is no syntax for the type, so the reachable form of this is a binding whose
// value is a panic being used afterwards; the binding itself is legal (and dead).
func TestPanic_BindingToANeverValueIsLegalButDead(t *testing.T) {
	res := parseCollectAndCheck(t, `let f = () -> i64 => { let x = panic("boom") 0 }`, false)
	assertNoErrors(t, res)
}
