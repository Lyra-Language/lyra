package collector_test

import "testing"

func TestCollect_WithStatementAnonymous(t *testing.T) {
	source := `
	with Arena.new(megabytes(4)) {
		let x = alloc(42)
	}`
	runGoldenTest(t, source, "with_statement_anonymous")
}

func TestCollect_WithStatementNamed(t *testing.T) {
	source := `
	with frame = Arena.new(megabytes(4)) {
		let x = alloc(42)
	}`
	runGoldenTest(t, source, "with_statement_named")
}
