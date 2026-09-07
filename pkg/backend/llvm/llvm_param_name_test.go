package llvm

import "testing"

// A parameter may carry any source name, including one the backend uses for a label.
//
// LLVM keeps a function's values and labels in one symbol table, and every lowered
// function opens with a block named `entry`, so a parameter named `entry` used to reach
// the IR bare and make clang refuse the module — on a program the front end had checked
// clean. `lowerParameter` now prefixes every source name, so the collision cannot recur
// for `entry` or for any block the backend names later; this test pins the one that
// happened. It runs the program rather than inspecting the IR because the failure was
// clang's, not the emitter's.
func TestExec_ParameterNamedLikeABlockLabel(t *testing.T) {
	t.Parallel()
	got := buildAndRun(t, `let bump = pure noalloc (entry: i64) -> i64 => entry + 1
let main = () -> u8 => u8(bump(2))
`)
	if got != 3 {
		t.Errorf("a function whose parameter is named entry exited %d; want 3", got)
	}
}
