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

// Any number of parameters may be `_` — a function's or a lambda's, a managed one among
// them. The collector refused the second as "already declared" and, past that, the backend
// named both LLVM arguments `p._`, which clang refused (found 10/02 by Vega's checks,
// a pixel function ignoring both coordinates).
func TestExec_SeveralUnderscoreParameters(t *testing.T) {
	t.Parallel()
	got := buildAndRunWithPrelude(t, `let apply = (f: (i64, string, i64) -> i64) -> i64 => f(1, "abc", 2)
let seven = pure (_: i64, _: string) -> i64 => 7
let main = () -> void => {
  let k = 40
  println(apply((_, _, c) => k + c))
  println(apply((_, s, _) => s.len()))
  println(seven(1, "y"))
}
`, "")
	if got != "42\n3\n7\n" {
		t.Errorf("parameters named _ printed %q; want 42, 3 and 7", got)
	}
}
