package checker_test

import "testing"

// `noalloc` and the array-repeat literal. The dynamic form allocates a box and the
// fixed-size form is stack storage, told apart by the *type* rather than the syntax —
// the same split `[1, 2, 3]` has.
//
// The gap this pins was live for exactly one build (08/08): the allocation walk names
// the allocating *forms*, and `ArrayRepeatExpr` was not among them, so
// `noalloc … => { let d: []i64 = [0; 3]; … }` type-checked clean while the identical
// `[1, 2, 3]` was refused. Hazard 8 in an arm that lists syntax rather than in a switch
// over types — which is why it survived adding the form everywhere else.
func TestArrayRepeatAlloc_DynamicFormRefusedByNoalloc(t *testing.T) {
	src := `
let f = noalloc () -> i64 => {
    let d: []i64 = [0; 3]
    d[0]
}`
	assertBoundError(t, checkPurity(t, src), "lyra-E016")
}

func TestArrayRepeatAlloc_FixedFormAllowed(t *testing.T) {
	src := `
let f = noalloc () -> i64 => {
    let a = [0; 3]
    a[0]
}`
	assertPurityCount(t, checkPurity(t, src), 0)
}
