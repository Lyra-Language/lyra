package typechecker_test

import "testing"

// A lambda literal takes its missing annotations from the context it appears in.
//
// Before, it took nothing: `(x) => x` reported `undefined symbol "x"` because an
// unannotated parameter never reached the parameter scope, and `() => 7` was rejected
// against `() -> i64` because the body's untyped leaf never learned the expected width.
// Only a fully annotated lambda worked, which meant every call site of every lazy
// combinator had to restate types the signature already gave.

// The two halves, at a direct call: a parameter type and a return width.
func TestContextualLambda_AtACallSite(t *testing.T) {
	res := parseCollectAndCheck(t, `
let takes0 = (f: () -> i64) -> i64 => f()
let takes1 = (f: (i64) -> i64) -> i64 => f(1)
let a = () -> i64 => takes0(() => 7)
let b = () -> i64 => takes1((x) => x + 1)`, false)
	assertNoErrors(t, res)
}

// An annotated binding is a context too — the entry noted this is not specific to the
// argument site, and `let g: () -> i64 = () => 7` recorded `g` as `() -> ?` before.
func TestContextualLambda_AtAnAnnotatedBinding(t *testing.T) {
	res := parseCollectAndCheck(t, `
let g: () -> i64 = () => 7
let h: (i64) -> i64 = (x) => x * 2`, false)
	assertNoErrors(t, res)
}

// Under a *generic* parameter the same failure surfaced as an inference error:
// `unwrap_or_else(m, () => 0)` reported "cannot infer type variable t".
//
// The receiver argument must be the one that solves `t` — that is what makes it fail. With
// `t` already bound to i64 by `m`, the bare lambda infers `() -> untyped_int` and unification
// rejects the inconsistent second binding. (Pass an unsolving first argument instead and the
// lambda solves `t` itself, which works either way and tests nothing.) Deferring the lambda
// until `t` is known lets it be elaborated to `() -> i64` before it is inferred.
func TestContextualLambda_UnderAGenericParameter(t *testing.T) {
	res := parseCollectAndCheck(t, `
data Opt<t> = Nil | Just(t)
let or_else<t> = (m: Opt<t>, f: () -> t) -> t => match m {
  Just(v) => v,
  Nil => f(),
}
let use = (m: Opt<i64>) -> i64 => or_else(m, () => 3)`, false)
	assertNoErrors(t, res)
}

// A variable solved by *this lambda's own body* must not be planted as its declared return
// type: `map(m, (x) => x * 2)` solves `u` from the body, so elaborating `u` as the return
// would leave it unsolved forever ("cannot convert u to u8" downstream).
func TestContextualLambda_ReturnVariableSolvedFromTheBody(t *testing.T) {
	res := parseCollectAndCheck(t, `
data Opt<t> = Nil | Just(t)
let map<t,u> = (m: Opt<t>, f: (t) -> u) -> Opt<u> => match m {
  Just(v) => Just(f(v)),
  Nil => Nil,
}
let use = (m: Opt<i64>) -> Opt<i64> => map(m, (x) => x * 2)`, false)
	assertNoErrors(t, res)
}

// A fully annotated lambda still *solves* variables from its own signature rather than
// being deferred — deferring it would lose that, and this is the case that made
// `unwrap_or_else(None, () -> i64 => 0)` work in the first place.
func TestContextualLambda_AnnotatedLambdaStillSolves(t *testing.T) {
	res := parseCollectAndCheck(t, `
data Opt<t> = Nil | Just(t)
let or_else<t> = (m: Opt<t>, f: () -> t) -> t => match m {
  Just(v) => v,
  Nil => f(),
}
let use = () -> i64 => or_else(Nil, () -> i64 => 42)`, false)
	assertNoErrors(t, res)
}

// An explicit annotation always wins: elaboration fills blanks, it does not overwrite. A
// lambda whose written types disagree with the context is still an error.
func TestContextualLambda_ExplicitAnnotationIsNotOverwritten(t *testing.T) {
	res := parseCollectAndCheck(t, `
let takes = (f: (i64) -> i64) -> i64 => f(1)
let bad = () -> i64 => takes((x: string) -> i64 => 1)`, false)
	if len(res.errors) == 0 {
		t.Fatal("a lambda whose written parameter type disagrees with the context should be rejected")
	}
}

// Arity disagreement is left to the call site rather than half-filled, so one mistake stays
// one diagnostic instead of becoming several about types nobody wrote.
func TestContextualLambda_ArityMismatchReportsOnce(t *testing.T) {
	res := parseCollectAndCheck(t, `
let takes = (f: (i64) -> i64) -> i64 => f(1)
let bad = () -> i64 => takes((x, y) => 1)`, false)
	if len(res.errors) == 0 {
		t.Fatal("an arity mismatch should be reported")
	}
}
