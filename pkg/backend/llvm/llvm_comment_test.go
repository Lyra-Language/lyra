package llvm

import "testing"

// A trailing comment on a block's final expression used to make the collector
// append a nil statement after it, so the block's value (its last statement) was
// that nil — the function returned garbage. Regression guard: the block value is
// the real final expression, comment or not.
func TestExec_TrailingCommentOnBlockValue(t *testing.T) {
	t.Parallel()
	withComment := `let f = () -> u8 => {
	  let a: u8 = 40
	  let b: u8 = 2
	  a + b // the block's value
	}
	let main = () -> u8 => f()`
	if got := buildAndRun(t, withComment); got != 42 {
		t.Errorf("block value with a trailing comment: exited %d, want 42", got)
	}

	// A comment as the *only* thing after the value, and a lone trailing comment.
	single := `let main = () -> u8 => {
	  let x: u8 = 7
	  x // done
	}`
	if got := buildAndRun(t, single); got != 7 {
		t.Errorf("single binding + trailing comment: exited %d, want 7", got)
	}
}
