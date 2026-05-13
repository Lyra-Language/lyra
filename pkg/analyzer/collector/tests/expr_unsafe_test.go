package collector_test

import "testing"

func TestCollect_UnsafeBlockAsStatement(t *testing.T) {
	source := `unsafe { ptr^ = 42 }`
	runGoldenTest(t, source, "expr_unsafe_block_as_statement")
}

func TestCollect_UnsafeBlockAsExpr(t *testing.T) {
	source := `
	let result = unsafe {
		let val = ptr^
		val + 1
	}`
	runGoldenTest(t, source, "expr_unsafe_block_as_expr")
}
