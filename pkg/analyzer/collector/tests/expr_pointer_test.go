package collector_test

import "testing"

func TestCollect_AddressOf(t *testing.T) {
	source := `let p = &x`
	runGoldenTest(t, source, "expr_address_of")
}

func TestCollect_AddressOfMut(t *testing.T) {
	source := `let p = &mut x`
	runGoldenTest(t, source, "expr_address_of_mut")
}

func TestCollect_DerefExpr(t *testing.T) {
	source := `let v = p^`
	runGoldenTest(t, source, "expr_deref")
}

func TestCollect_DerefAssignment(t *testing.T) {
	source := `ptr^ = 42`
	runGoldenTest(t, source, "stmt_deref_assignment")
}
