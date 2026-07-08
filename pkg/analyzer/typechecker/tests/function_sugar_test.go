package typechecker_test

import "testing"

// The ML-style function-definition sugar (`let name(params) => body`, no `=`)
// desugars to a plain `let name = (params) => body` binding, so a function
// defined with it must register in the symbol table and typecheck — including
// being callable — exactly like the `=` form.

func TestSugar_DefinedFunctionIsCallable(t *testing.T) {
	res := parseCollectAndCheck(t, `
	let add(a: i64, b: i64) -> i64 => a + b
	let result = add(1, 2)`, false)
	assertNoErrors(t, res)
}

func TestSugar_ArgumentTypeMismatchStillChecked(t *testing.T) {
	// The sugar-defined function is fully typed, so a bad call is still caught.
	res := parseCollectAndCheck(t, `
	let add(a: i64, b: i64) -> i64 => a + b
	let result = add(1, "two")`, false)
	if len(res.errors) == 0 {
		t.Fatalf("expected a type error for the mismatched argument, got none")
	}
}
