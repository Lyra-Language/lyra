package llvm

import (
	"strings"
	"testing"
)

// A literal is a postfix head as of 08/06, so a method may be called on one
// directly. `_primary_expr` had admitted an identifier, a `parenthesized_expr` and
// a struct literal and no literal at all, so `"abc".len()` was a *syntax* error
// while `("abc").len()` and a bound `s.len()` both worked.
//
// It matters more than it reads: UFCS made method syntax the normal way to call, so
// every combinator the prelude gains was unreachable from the literal a reader would
// naturally try it on.
func TestExec_MethodOnALiteralReceiver(t *testing.T) {
	t.Parallel()
	const src = `
module main
let main = () -> void => {
  println("${"abc".len()}");
  println("[${"  padded  ".trim()}]");
  println("${"héllo".slice(1, 3)}");
  println("${[1, 2, 3].len()}");
  println("${1.wrapping_add(2)}");
  println("${1.5.floor()}");
}
`
	want := "3\n[padded]\nél\n3\n3\n1"
	if got := strings.TrimSpace(buildAndRunWithPrelude(t, src, "")); got != want {
		t.Errorf("methods on literal receivers gave:\n%s\nwant:\n%s", got, want)
	}
}

// A numeric literal receiver has to be pinned to its default width, and this is the
// case that proves it: `builtinMethodSignature` promotes internally to decide
// *whether* the method exists, but that promotion is local to the lookup — the
// receiver node keeps what the literal inferred as, and the backend reads the node.
// So `1.5.floor()` type-checked and failed to lower with "floor() on non-float
// receiver float literal". No receiver could be a bare literal before this change,
// which is why it had never come up.
func TestExec_NumericLiteralReceiverTakesItsDefaultWidth(t *testing.T) {
	t.Parallel()
	const src = `
module main
let main = () -> void => {
  println("${2.5.ceil()} ${2.4.round()} ${7.wrapping_mul(6)}");
}
`
	if got := strings.TrimSpace(buildAndRunWithPrelude(t, src, "")); got != "3 2 42" {
		t.Errorf("got %q; want \"3 2 42\"", got)
	}
}

// The readings a literal head contests, all of which the corpus also pins. They run
// here because a parse that is merely *different* rather than broken would still
// compile — `0 - 200` reading as `0` followed by a dangling negation is the failure
// this grammar region is on record for, and it produces a program, not an error.
func TestExec_LiteralHeadsDoNotDisturbNeighbouringReadings(t *testing.T) {
	t.Parallel()
	const src = `
module main
let apply = (f: (i64, i64) -> i64, a: i64, b: i64) -> i64 => f(a, b)
let main = () -> void => {
  let pair = (20, 22);
  let neg = (-1, 2);
  println("${0 - 200} ${pair.0 + pair.1} ${neg.0} ${apply((a, b) => a + b, 3, 4)} ${"a" ++ "b"}");
}
`
	if got := strings.TrimSpace(buildAndRunWithPrelude(t, src, "")); got != "-200 42 -1 7 ab" {
		t.Errorf("got %q; want \"-200 42 -1 7 ab\"", got)
	}
}
