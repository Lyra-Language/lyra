package typechecker_test

import "testing"

// Return-type-driven inference: a type variable the arguments cannot reach is solved from
// the **context the call sits in** — an annotation, a declared return type, or the
// parameter slot the call is being passed into.
//
// Solving reads argument types only, so a variable mentioned only in the callee's return
// type had nothing to bind it. That is exactly a constructor's shape — takes a capacity,
// returns the thing the variables live in — so a generic collection had no constructor.
//
// Two restrictions keep this from changing what any existing program means, and both are
// tested below. It seeds **only variables no parameter mentions**, so it never competes
// with argument solving for a variable the arguments do determine; and only on a
// declaration that **declares** its type parameters, so an implicit return-only variable
// (likelier a typo than an intent) is still refused at the call.

func TestTypeCheck_ReturnTypeInference_FromAnAnnotation(t *testing.T) {
	assertNoErrors(t, parseCollectAndCheck(t, `
let empty<t> = pure () -> []t => []
let xs: []i64 = empty()
`, false))
}

func TestTypeCheck_ReturnTypeInference_FromADeclaredReturn(t *testing.T) {
	assertNoErrors(t, parseCollectAndCheck(t, `
let empty<t> = pure () -> []t => []
let make = pure () -> []i64 => empty()
`, false))
}

func TestTypeCheck_ReturnTypeInference_FromAParameterSlot(t *testing.T) {
	assertNoErrors(t, parseCollectAndCheck(t, `
let empty<t> = pure () -> []t => []
let take = pure (xs: []i64) -> i64 => xs.len()
let n = take(empty())
`, false))
}

// The shape the feature exists for: several parameters, none reachable from the one
// argument, all solved from the annotation.
func TestTypeCheck_ReturnTypeInference_SolvesAGenericConstructor(t *testing.T) {
	assertNoErrors(t, parseCollectAndCheck(t, `
data Slot<k, v> = Empty | Full(k, v)
struct HashMap<k, v> { slots: []Slot<k, v>, count: i64 }
let with_capacity<k,v> = pure (cap: i64) -> HashMap<k,v> =>
  HashMap { slots: [Empty; cap], count: 0 }
let m: HashMap<string, i64> = with_capacity(16)
`, false))
}

// With no context there is nothing to seed from, so the call is still refused — the
// feature adds a source of bindings, it does not invent one.
func TestTypeCheck_ReturnTypeInference_NoContextStillFails(t *testing.T) {
	res := parseCollectAndCheck(t, `
let empty<t> = pure () -> []t => []
let xs = empty()
`, false)
	assertErrorsAre(t, res,
		"empty: cannot infer type variable t from these arguments; name them explicitly with ::<t>")
}

// An **implicit** return-only variable — no `<t>` on the declaration — is not seeded. A
// lowercase name is a type variable either way, but without a declared parameter list it
// is far likelier a typo, and binding it from context would let the same broken
// declaration compile at call sites whose context happens to fit and fail at the rest.
func TestTypeCheck_ReturnTypeInference_SkipsAnUndeclaredVariable(t *testing.T) {
	res := parseCollectAndCheck(t, `
let make = (n: i64) -> t => n
let main = () -> u8 => u8(make(1))
`, false)
	assertErrorContainsGeneric(t, res, "cannot infer type variable t")
}

// A variable the arguments *do* reach is still solved from them. The context is not
// consulted for it, so there is no second source of truth and no precedence question —
// this call means exactly what it meant before the feature existed.
func TestTypeCheck_ReturnTypeInference_ArgumentsStillWinWhereTheyReach(t *testing.T) {
	assertNoErrors(t, parseCollectAndCheck(t, `
let id<t> = pure (x: t) -> t => x
let a: i64 = id(7)
`, false))
}

// …and a call whose arguments contradict each other still fails for that reason, rather
// than being rescued by a context that would have made one of them fit.
func TestTypeCheck_ReturnTypeInference_DoesNotRescueAnInconsistentCall(t *testing.T) {
	res := parseCollectAndCheck(t, `
let same<t> = pure (a: t, b: t) -> t => a
let x: bool = same(7, true)
`, false)
	assertErrorContainsGeneric(t, res, "cannot infer type variable t")
}

// An explicit turbofish that disagrees with the annotation is reported as the assignment
// mismatch it is: the turbofish bound the variable, and the result then did not fit.
func TestTypeCheck_ReturnTypeInference_TurbofishBeatsTheAnnotation(t *testing.T) {
	res := parseCollectAndCheck(t, `
let empty<t> = pure () -> []t => []
let xs: []string = empty::<i64>()
`, false)
	assertErrorsAre(t, res, "xs: cannot assign DynamicArray<i64> to DynamicArray<string>")
}
