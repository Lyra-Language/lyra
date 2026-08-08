package llvm

import (
	"strings"
	"testing"
)

// Formatting a value whose type is a type parameter (08/08).
//
// `print` and `"${…}"` pick a formatter per *concrete* type, so a `t` could not be
// rendered at all. With a `where t: Show` bound the operand is rewritten to `v.show()`
// before anything downstream sees it — ordinary bound dispatch, so the backend learns
// nothing new and these tests are really asking whether the rewrite reaches lowering.

func TestExec_ShowInterpolatesABoundTypeParameter(t *testing.T) {
	t.Parallel()
	const src = `
module main
let describe<t> where t: Show = (v: t) -> string => "value ${v} (twice: ${v}${v})"
let main = () -> void => {
  println(describe(7));
  println(describe("s"));
  println(describe(3.5));
  println(describe(true));
}
`
	want := "value 7 (twice: 77)\nvalue s (twice: ss)\nvalue 3.5 (twice: 3.53.5)\nvalue true (twice: truetrue)"
	if got := strings.TrimSpace(buildAndRunWithPrelude(t, src, "")); got != want {
		t.Errorf("interpolated type parameter =\n%q\nwant\n%q", got, want)
	}
}

// `print`/`println` take the same rewrite, so a bounded generic can print its payload
// directly rather than only interpolate it.
func TestExec_ShowPrintsABoundTypeParameter(t *testing.T) {
	t.Parallel()
	const src = `
module main
let emit<t> where t: Show = (v: t) -> void => println(v)
let main = () -> void => {
  emit(42);
  emit('x');
  emit("done");
}
`
	if got := strings.TrimSpace(buildAndRunWithPrelude(t, src, "")); got != "42\nx\ndone" {
		t.Errorf("printed type parameter = %q; want \"42\\nx\\ndone\"", got)
	}
}

// A **user** impl reaches the same path — the prelude's impls are not privileged, they are
// just the ones that ship. This is also the case the feature exists for: a combinator
// describing a payload whose type it does not know.
func TestExec_ShowUserImplThroughAGeneric(t *testing.T) {
	t.Parallel()
	const src = `
module main
struct Pt { x: i64, y: i64 }
impl Show for Pt { show = (self) => "(${self.x}, ${self.y})" }
let describe<t> where t: Show = (v: t) -> string => "value ${v}"
let main = () -> void => {
  println(describe(Pt { x: 1, y: 2 }));
  println(describe(9));
}
`
	if got := strings.TrimSpace(buildAndRunWithPrelude(t, src, "")); got != "value (1, 2)\nvalue 9" {
		t.Errorf("user impl = %q; want \"value (1, 2)\\nvalue 9\"", got)
	}
}

// A bounded generic calling another bounded generic: the bound has to travel, and each
// instantiation picks its own impl.
func TestExec_ShowThroughNestedGenerics(t *testing.T) {
	t.Parallel()
	const src = `
module main
let inner<t> where t: Show = (v: t) -> string => "[${v}]"
let outer<u> where u: Show = (v: u) -> string => "<" ++ inner(v) ++ ">"
let main = () -> void => {
  println(outer(5));
  println(outer("x"));
}
`
	if got := strings.TrimSpace(buildAndRunWithPrelude(t, src, "")); got != "<[5]>\n<[x]>" {
		t.Errorf("nested generics = %q; want \"<[5]>\\n<[x]>\"", got)
	}
}

// The prelude's impls cover every printable scalar, and each renders as `print` does —
// the impls are `"${self}"`, so this is really asserting that the set is complete rather
// than that the formatting is new.
func TestExec_ShowCoversEveryPrintableScalar(t *testing.T) {
	t.Parallel()
	const src = `
module main
let s<t> where t: Show = (v: t) -> string => v.show()
let main = () -> void => {
  println("${s(i8(-1))} ${s(i16(-2))} ${s(i32(-3))} ${s(i64(-4))}");
  println("${s(u8(1))} ${s(u16(2))} ${s(u32(3))} ${s(u64(4))}");
  println("${s(f32(1.5))} ${s(f64(2.5))} ${s(true)} ${s('z')} ${s("str")}");
}
`
	want := "-1 -2 -3 -4\n1 2 3 4\n1.5 2.5 true z str"
	if got := strings.TrimSpace(buildAndRunWithPrelude(t, src, "")); got != want {
		t.Errorf("scalar impls =\n%q\nwant\n%q", got, want)
	}
}
