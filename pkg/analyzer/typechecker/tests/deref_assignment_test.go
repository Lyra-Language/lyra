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
// binding, so the const check does not fire.
func TestDerefAssignment_NonConstTargetOK(t *testing.T) {
	res := parseCollectAndCheck(t, `ptr^ = 42`, false)
	assertNoErrors(t, res)
}
