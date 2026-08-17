package llvm

import (
	"strings"
	"testing"
)

// A branching construct whose branches call `void` functions must not build a phi.
//
// Both of these emitted `phi void` until 08/17 and clang refused the module outright
// ("void type only allowed for function results"), so `if b { f() } else { g() }` over
// two void functions — six ordinary lines — did not compile at all.
//
// The cause is that "produced no value" has **two spellings**, and both merges tested
// only one: a builtin hands back a nil, while a call to a user-defined `void` function
// hands back the `ir.Call` itself, non-nil with a void type. Both guards' comments
// already anticipated void branches; neither anticipated that one of them is not nil.
// They are twins (control_flow.go and match.go) and were fixed in one change, so they
// are tested together.

func TestExec_VoidIfBranchesDoNotBuildAPhi(t *testing.T) {
	t.Parallel()
	const src = `
module main
let f = () -> void => println("f")
let g = () -> void => println("g")
let main = () -> void => {
  var b = true;
  if b { f() } else { g() }
  b = false;
  if b { f() } else { g() }
  println("done");
}
`
	got := strings.TrimSpace(buildAndRunWithPrelude(t, src, ""))
	if got != "f\ng\ndone" {
		t.Errorf("void if branches gave %q, want \"f\\ng\\ndone\"", got)
	}
}

func TestExec_VoidMatchArmsDoNotBuildAPhi(t *testing.T) {
	t.Parallel()
	const src = `
module main
let f = () -> void => println("f")
let g = () -> void => println("g")
let main = () -> void => {
  var x = 0;
  match x { 0 => f(), _ => g() }
  x = 1;
  match x { 0 => f(), _ => g() }
  println("done");
}
`
	got := strings.TrimSpace(buildAndRunWithPrelude(t, src, ""))
	if got != "f\ng\ndone" {
		t.Errorf("void match arms gave %q, want \"f\\ng\\ndone\"", got)
	}
}
