package typechecker_test

import "testing"

// lyra-E073 — a construction whose generic type parameters nothing solved.
//
// **This is not the missing-propagation bug it looks like.** Where a context exists and
// was not pushed onto the constructor, that is a compiler fault and was fixed as one
// (COMPLETED.md 09/09, one omission in four places). Here there *is* no context: a nullary
// constructor solves none of its parameters, the surrounding expression supplies none, and
// the binding has no annotation. The program does not say what it means.
//
// It used to type-check clean and fail in the backend with `unknown named type "Opt"` —
// the wrong end of the compiler, and a message about an internal table rather than about
// the program.
//
// The tests declare their own generic type because this harness has no prelude, which is
// also why they cannot use `Maybe`.

const optDecl = "data Opt<t> = Nothing | Just(t)\n"

func TestUninferable_NothingSolvesTheParameter(t *testing.T) {
	res := parseCollectAndCheck(t, optDecl+`
let main = () -> void => {
  let x = Nothing
  println("${x}")
}`, false)
	assertHasErrorContaining(t, res, "cannot tell what `Nothing` holds")
}

// The reported shape: inside a tuple, where the tuple supplies no context either.
func TestUninferable_InsideATuple(t *testing.T) {
	res := parseCollectAndCheck(t, optDecl+`
let main = () -> void => {
  let t = (Nothing, 1)
  println("${t.1}")
}`, false)
	assertHasErrorContaining(t, res, "cannot tell what `Nothing` holds")
}

// **An unused binding is reported too**, and that is the point of checking in the front
// end. It used to depend on *use*: an unused value is never lowered, so the backend never
// asked and the same ill-typed program compiled. Whether a program is well-typed cannot
// depend on whether anyone reads the value.
func TestUninferable_EvenWhenTheBindingIsUnused(t *testing.T) {
	res := parseCollectAndCheck(t, optDecl+`
let main = () -> void => {
  let x = Nothing
  println("done")
}`, false)
	assertHasErrorContaining(t, res, "cannot tell what `Nothing` holds")
}

// **A non-generic data type is never flagged.** `North` records as the bare `Dir` and
// always will — there is nothing to solve, so having no parameters is the whole test.
func TestUninferable_ANonGenericConstructorIsFine(t *testing.T) {
	assertNoErrors(t, parseCollectAndCheck(t, `
data Dir = North | South
let main = () -> void => {
  let d = North
  match d { North => println("n"), South => println("s") }
}`, false))
}

// Every position that *does* supply a context stays clean — the same set the lowering
// tests enumerate, so a change to either is checked against both.
func TestUninferable_AContextSolvesIt(t *testing.T) {
	for _, src := range []string{
		"let main = () -> void => { let x: Opt<i64> = Nothing\n  println(\"${x == Nothing}\") }",
		"let take = pure (v: Opt<i64>) -> bool => v == Nothing\nlet main = () -> void => println(\"${take(Nothing)}\")",
		"let give = pure () -> Opt<i64> => Nothing\nlet main = () -> void => println(\"${give() == Nothing}\")",
		"let main = () -> void => { let t: (Opt<i64>, i64) = (Nothing, 1)\n  println(\"${t.1}\") }",
		"let main = () -> void => { var m: Opt<i64> = Just(1)\n  m = Nothing\n  println(\"${m == Nothing}\") }",
		"let main = () -> void => { let xs: []Opt<i64> = [Nothing]\n  println(\"${xs.len()}\") }",
	} {
		assertNoErrors(t, parseCollectAndCheck(t, optDecl+src+"\n", false))
	}
}

// A generic *body* mentions the enclosing function's type variable, which is a solved
// instantiation as far as this check is concerned — the specialization substitutes it
// later. Flagging it would refuse every generic function that names a generic type.
func TestUninferable_AGenericBodyIsFine(t *testing.T) {
	assertNoErrors(t, parseCollectAndCheck(t, optDecl+`
let is_empty<t> = pure (v: Opt<t>) -> bool => v == Nothing
let main = () -> void => println("${is_empty(Just(1))}")`, false))
}
