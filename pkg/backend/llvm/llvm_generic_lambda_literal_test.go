package llvm

import (
	"strings"
	"testing"
)

// A generic's type variable reached only by an untyped literal and by an unannotated
// lambda's *parameters* is settled from the literal before the lambda is elaborated
// (10/19). The lambda waited for the literal, which spoke last, so `apply(5, (x) => x)`
// reported "undefined symbol x" and "cannot infer type variable t" — and so did the most
// ordinary fold there is, `fold(xs, 0, (acc, x) => acc + x)`. The todo entry called it a
// second-call failure; one call was enough, the entry's first call having had a typed lambda.
func TestExec_ALiteralSettlesALambdasParameter(t *testing.T) {
	t.Parallel()
	const src = `
module main
let apply<t> = (by: t, f: (t) -> i64) -> i64 => f(by)
let fold<t, a> = (xs: []t, init: a, f: (a, t) -> a) -> a => {
  var acc = init
  for x in xs { acc = f(acc, x) }
  acc
}
let check<t> = (by: t, f: (t) -> bool) -> bool => f(by)
let main = () -> void => {
  let a = apply(5, (x) => x)
  let b = apply(6, (x) => x * 2)
  let s = fold([1, 2, 3], 0, (acc, x) => acc + x)
  print("${a} ${b} ${s} ${check(2.5, (x) => x > 1.0)} ${check(-5, (x) => x < 0)}")
}
`
	if got := strings.TrimSpace(buildAndRunWithPrelude(t, src, "")); got != "5 12 6 true true" {
		t.Errorf("got %q; want \"5 12 6 true true\"", got)
	}
}

// What the literal settles is only its default, so everything with a width of its own still
// speaks first: a context (`u8`, which then traps at 250 + 10 rather than holding 260), and a
// lambda's *return* where only the return reaches the variable — the literal adopts `u8`
// there instead of fixing `t` at `i64`.
func TestExec_ALiteralStillSpeaksAfterAWidth(t *testing.T) {
	t.Parallel()
	const src = `
module main
let app<t> = (by: t, f: (t) -> t) -> t => f(by)
let late<t> = (by: t, f: () -> t) -> t => {
  let _ = by
  f()
}
let take8 = (v: u8) -> u8 => v
let main = () -> void => {
  let a: u8 = app(250, (x) => x + 5)
  let b: u8 = 7
  print("${a} ${take8(late(5, () => b))}")
}
`
	if got := strings.TrimSpace(buildAndRunWithPrelude(t, src, "")); got != "255 7" {
		t.Errorf("got %q; want \"255 7\"", got)
	}
}
