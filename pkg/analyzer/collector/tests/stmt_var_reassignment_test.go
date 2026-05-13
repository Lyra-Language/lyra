package collector_test

import "testing"

func TestCollect_VarReassignment(t *testing.T) {
	source := `
	var x = 1
	x = 2`
	runGoldenTest(t, source, "stmt_var_reassignment")
}
