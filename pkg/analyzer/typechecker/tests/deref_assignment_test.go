package typechecker_test

import "testing"

// The grammar represents an assignment whose target is a const-cased identifier
// written in the caret-deref form (`NAME^ = v`) as a DerefAssignmentStmt whose
// operand carries IsConst. The typechecker rejects it as a write to an
// immutable binding. (A SCREAMING_CASE name is what the grammar classifies as a
// const identifier, so `PTR` here is const-flavored.)
func TestDerefAssignment_ConstTargetRejected(t *testing.T) {
	res := parseCollectAndCheck(t, `PTR^ = 42`, false)
	assertErrorsAre(t, res, "PTR: 'const' binding is immutable and cannot be reassigned")
}

// A lowercase (non-const) identifier in the same caret-deref form is not a const
// binding, so the const check does not fire — and what is left is a genuine
// pointer write, which is lyra-E051 (raw pointers unimplemented) since 08/13.
//
// **This test asserted no errors at all until then, and that was the phantom in
// miniature**: nothing else looked at a pointer write either, so the one raw-
// pointer form that never reaches inferExprType's DerefExpr arm (WalkStmt
// descends into the target's operand, not the deref) type-checked clean and died
// in the backend. It drew E011's "requires an `unsafe` block" from the standalone
// checker, advice that could not be followed because `unsafe { … }` was itself an
// unknown expression.
func TestDerefAssignment_NonConstTargetRefused(t *testing.T) {
	res := parseCollectAndCheck(t, `ptr^ = 42`, false)
	assertErrorsAre(t, res,
		"writing through a raw pointer is not implemented: Lyra has no raw-pointer operations yet")
}
