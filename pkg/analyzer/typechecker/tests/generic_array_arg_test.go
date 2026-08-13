package typechecker_test

import "testing"

// Solving a generic `[]t` parameter from an array-literal argument (08/13).
//
// An array literal is the one expression whose representation its context chooses —
// `[1, 2, 3]` is a fixed `[3]T` or a heap-allocated `[]T` by what it is used as —
// and the usual mechanism for that (propagating the target type onto the literal)
// cannot run here: the target is `[]t` and `t` is exactly what is being solved. So
// the literal inferred `[3]i64`, the unifier's DynamicArrayType arm accepted only a
// DynamicArrayType, and the call reported "cannot infer type variable t from these
// arguments" — naming the wrong problem, since `t` is plainly `i64`.

func TestTypeCheck_GenericDynamicArrayParam_FromArrayLiteral(t *testing.T) {
	assertNoErrors(t, parseCollectAndCheck(t, `
let first_of<t> = (xs: []t) -> t => xs[0]
let a = first_of([1, 2, 3])
`, false))
}

func TestTypeCheck_GenericDynamicArrayParam_FromRepeatLiteral(t *testing.T) {
	assertNoErrors(t, parseCollectAndCheck(t, `
let first_of<t> = (xs: []t) -> t => xs[0]
let a = first_of([0; 4])
`, false))
}

func TestTypeCheck_GenericDynamicArrayParam_FromStringLiteralArray(t *testing.T) {
	assertNoErrors(t, parseCollectAndCheck(t, `
let first_of<t> = (xs: []t) -> t => xs[0]
let a = first_of(["x", "y"])
`, false))
}

// The two forms that already worked, kept beside it: a `[]T` binding, and a
// fixed-size generic parameter taking the same literal.
func TestTypeCheck_GenericArrayParam_ExistingFormsStillWork(t *testing.T) {
	assertNoErrors(t, parseCollectAndCheck(t, `
let first_of<t> = (xs: []t) -> t => xs[0]
let first_fixed<t> = (xs: [3]t) -> t => xs[0]
let xs: []i64 = [1, 2, 3]
let a = first_of(xs)
let b = first_fixed([4, 5, 6])
`, false))
}

// **A fixed-array *binding* is still refused, deliberately.** Only a literal is
// adapted, because the adaptation is a claim about how the value will be *built*,
// and a `[3]i64` binding is already stack storage while `[]T` is a ref-counted box.
// Accepting it here would import a live memory fault: the non-generic path does
// accept it (`isAssignable`'s static→dynamic rule tests the type although its own
// comment says "literal"), and the resulting program **segfaults** — see todo.md.
// Pinned so that fixing that bug is a deliberate act rather than something this
// test's silence invites.
func TestTypeCheck_GenericDynamicArrayParam_FixedBindingStillRefused(t *testing.T) {
	res := parseCollectAndCheck(t, `
let first_of<t> = (xs: []t) -> t => xs[0]
let ys: [3]i64 = [1, 2, 3]
let a = first_of(ys)
`, false)
	assertErrorsAre(t, res, "first_of: cannot infer type variable t from these arguments")
}
