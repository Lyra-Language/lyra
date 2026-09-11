package llvm

import (
	"strings"
	"testing"

	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/llir/llvm/ir"
	"github.com/llir/llvm/ir/constant"
	lltypes "github.com/llir/llvm/ir/types"
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

// **A branching construct must push its joined type back down onto its branches**, or a
// branch whose own type is still untyped keeps its default width and the phi mixes two.
//
// The twins again, and this time they had *diverged*: `checkMatchExpr` called
// `propagateExpectedType` on every arm after computing the join and `checkIfExpr` called
// only `pushSettledInstantiation`, so `if` alone was wrong (09/10). It survived because a
// **bare literal** branch is settled by literal propagation from the binding anyway —
// `if c { a } else { 1.0 }` works. What had nothing else to narrow it was a *computed*
// untyped branch: `0.0 - 1.0` stayed f64 against an f32 sibling, `1 + 2` stayed i64
// against a u8 one, `lyrac check` passed clean, and clang refused the module with
// "'%14' defined with type 'double' but expected 'float'".
//
// The values matter as much as the compile: narrowing to the wrong width would still
// build, so each case takes the branch whose type had to be adjusted and prints it.
func TestExec_IfBranchesJoinTheirUntypedWidths(t *testing.T) {
	t.Parallel()
	const src = `
module main
let take32 = pure (v: f32) -> f32 => v * 2.0
let take8 = pure (v: u8) -> u8 => v
let main = () -> void => {
  // Never true, and not a literal, so no branch is folded away.
  let no = program_args().len() > 99
  let a: f32 = 1.5
  let b: u8 = 7
  // A computed untyped float branch against an f32 sibling.
  let f = if no { a } else { 0.0 - 1.25 }
  // A computed untyped integer branch against a u8 sibling.
  let i = if no { b } else { 1 + 2 }
  // A chain, where the inner if is typed through the outer's else branch.
  let chain = if no { a } else if no { 0.5 } else { 2.0 - 0.5 }
  print("${f64(take32(f))} ${take8(i)} ${f64(take32(chain))}")
}
`
	if got := strings.TrimSpace(buildAndRunWithPrelude(t, src, "")); got != "-2.5 3 3" {
		t.Errorf("got %q; want \"-2.5 3 3\"", got)
	}
}

// The sibling, which was already right and is here so the pair cannot drift apart again.
func TestExec_MatchArmsJoinTheirUntypedWidths(t *testing.T) {
	t.Parallel()
	const src = `
module main
let take32 = pure (v: f32) -> f32 => v * 2.0
let take8 = pure (v: u8) -> u8 => v
let main = () -> void => {
  let n = program_args().len()
  let a: f32 = 1.5
  let b: u8 = 7
  let f = match n { 0 => a, _ => 0.0 - 1.25 }
  let i = match n { 0 => b, _ => 1 + 2 }
  print("${f64(take32(f))} ${take8(i)}")
}
`
	if got := strings.TrimSpace(buildAndRunWithPrelude(t, src, "")); got != "-2.5 3" {
		t.Errorf("got %q; want \"-2.5 3\"", got)
	}
}

// `joinPhi` itself, because the path through it is unreachable from source once the
// typechecker is right — and rule 3's point is that a dead error path is a live one after
// the next front-end change, which is exactly how this one was born. Exercising the helper
// directly is what keeps its message from rotting in the meantime.
func TestJoinPhi_RefusesMismatchedOperands(t *testing.T) {
	t.Parallel()
	fn := ir.NewFunc("f", lltypes.Void)
	a, b := fn.NewBlock("a"), fn.NewBlock("b")
	merge := fn.NewBlock("m")
	incomings := []*ir.Incoming{
		ir.NewIncoming(constant.NewFloat(lltypes.Float, 1), a),
		ir.NewIncoming(constant.NewFloat(lltypes.Double, 2), b),
	}
	_, err := joinPhi(merge, incomings, "if/else branches", ast.Location{StartLine: 7, StartCol: 3})
	if err == nil {
		t.Fatal("joinPhi accepted a float beside a double; it must refuse rather than let clang find it")
	}
	for _, want := range []string{"if/else branches", "7:3", "float", "double"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q — the message exists to locate the expression", err, want)
		}
	}
}

// Matching operands still build a phi, so the guard is a check rather than a refusal.
func TestJoinPhi_AcceptsMatchingOperands(t *testing.T) {
	t.Parallel()
	fn := ir.NewFunc("f", lltypes.Void)
	a, b := fn.NewBlock("a"), fn.NewBlock("b")
	merge := fn.NewBlock("m")
	incomings := []*ir.Incoming{
		ir.NewIncoming(constant.NewFloat(lltypes.Float, 1), a),
		ir.NewIncoming(constant.NewFloat(lltypes.Float, 2), b),
	}
	phi, err := joinPhi(merge, incomings, "if/else branches", ast.Location{})
	if err != nil || phi == nil {
		t.Fatalf("joinPhi refused two floats: %v", err)
	}
}
