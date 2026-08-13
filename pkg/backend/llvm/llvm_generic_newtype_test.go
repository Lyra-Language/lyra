package llvm

import (
	"strings"
	"testing"
)

// A generic `newtype` — `newtype Boxed<t> = t`.
//
// `newtype` was the one type declaration whose grammar had no slot for generic
// parameters: struct, data, tuple and trait all took them. The `<t>` landed in an ERROR
// node while the declaration collected anyway, so the parameters were silently dropped —
// and the golden file for that case recorded the drop as if it were the intended output.
//
// A newtype is nominal to the typechecker and *transparent* to codegen, so a
// `Boxed<i64>` is an i64 at run time with no wrapper — which is what these check.
func TestExec_GenericNewtypeIsItsSubstitutedBase(t *testing.T) {
	t.Parallel()
	const src = `
module main
newtype Boxed<t> = t
newtype Plain = i64
let main = () -> void => {
  let b: Boxed<i64> = 5;
  let raw = i64(b);
  let p: Plain = 7;
  let praw = i64(p);
  println("${raw + praw}");
}
`
	if got := strings.TrimSpace(buildAndRunWithPrelude(t, src, "")); got != "12" {
		t.Errorf("got %q; want \"12\"", got)
	}
}

// Two instantiations of one generic newtype, each transparent to its own base.
func TestExec_GenericNewtypeAtTwoInstantiations(t *testing.T) {
	t.Parallel()
	const src = `
module main
newtype Tagged<t> = t
let main = () -> void => {
  let n: Tagged<i64> = 41;
  let s: Tagged<string> = "hi";
  let rn = i64(n);
  let rs = string(s);
  println("${rn + 1} ${rs}");
}
`
	if got := strings.TrimSpace(buildAndRunWithPrelude(t, src, "")); got != "42 hi" {
		t.Errorf("got %q; want \"42 hi\"", got)
	}
}

// The nominal half still holds: a generic newtype is not interchangeable with its base,
// so assigning one to the other without an annotation is still an error. Transparent to
// codegen, distinct to the typechecker — that is the whole point of `newtype`.
func TestCheck_GenericNewtypeIsStillNominal(t *testing.T) {
	t.Parallel()
	const src = `
module main
newtype Boxed<t> = t
let takes = (n: i64) -> i64 => n
let main = () -> void => {
  let b: Boxed<string> = "x";
  println("${takes(b)}");
}
`
	if diags := checkWithPrelude(t, src); len(diags) == 0 {
		t.Error("a Boxed<string> is not an i64 — the nominal distinction must survive")
	}
}

// A generic newtype constructs by call (08/12), and lowers to its operand exactly as
// a concrete one does — the parameters are solved from the operand (or bound by the
// `::<>` turbofish), the bound set resolves through the same expansion the annotation
// form uses, and the backend sees the substituted ConstrainedType it already knows.
// Two instantiations in one body pin that the solving is per-call, not per-name.
func TestExec_GenericNewtypeConstructorLowers(t *testing.T) {
	t.Parallel()
	const src = `
module main
newtype Boxed<t> = t
let main = () -> void => {
  let a = Boxed(41)
  let s = Boxed("hi")
  let c = Boxed::<u8>(200)
  println("${i64(a) + 1} ${string(s)} ${u8(c)}")
}
`
	if got := strings.TrimSpace(buildAndRunWithPrelude(t, src, "")); got != "42 hi 200" {
		t.Errorf("generic newtype construction = %q; want \"42 hi 200\"", got)
	}
}
