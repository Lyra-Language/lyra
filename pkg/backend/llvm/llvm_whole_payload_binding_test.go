package llvm

import (
	"strings"
	"testing"
)

// One name for a multi-field payload binds it as a tuple (09/13): `Rect pair` is
// `Rect(...pair)`, which the typechecker rewrites it to — so it lowers exactly as the rest
// does, in every position a pattern is written.
func TestExec_WholePayloadBinding(t *testing.T) {
	t.Parallel()
	src := `
data Shape = Rect(i64, i64) | Dot
data Named = Named(string, string)
data Wrap = Wr(i64, i64)
data Pr<t> = Two(t, t) | Zero
data Deep = Dp((i64, i64), i64)
let area = (s: Shape) -> i64 => match s {
  Rect pair => pair.0 * pair.1,
  Dot => 0,
}
let names = (n: Named) -> string => match n { Named both => both.0 ++ " " ++ both.1 }
let joined = (p: Pr<string>) -> string => match p { Two ts => ts.0 ++ ts.1, Zero => "" }
let deep = (d: Deep) -> i64 => match d { Dp all => all.0.0 + all.0.1 + all.1 }
let main = () -> void => {
  let first = "a" ++ "b"
  var hit = 0
  if let Rect p = Rect(5, 6) { hit = p.1 }
  let Wr w = Wr(7, 8)
  let Rect r = Rect(9, 10) else { return }
  let nested = match Some(Rect(1, 2)) { Some(Rect q) => q.0 + q.1, _ => 0 }
  println("${area(Rect(3, 4))} ${names(Named(first, "c" ++ "d"))} ${hit} ${w.1} ${r.0} ${nested} ${joined(Two("x" ++ "y", "z"))} ${deep(Dp((1, 2), 3))}")
}
`
	want := "12 ab cd 6 8 9 3 xyz 6"
	if got := strings.TrimSpace(buildAndRunWithPrelude(t, src, "")); got != want {
		t.Errorf("got %q; want %q", got, want)
	}
}

// The bound tuple borrows managed fields as the rest does — read, copied out, and the
// payload still released once. Runtime-built strings, since a literal is immortal and would
// hide a miscount.
func TestLSan_WholePayloadBindingOfManagedFields(t *testing.T) {
	t.Parallel()
	const src = `module main
data Named = Named(string, string)
let main = () -> u8 => {
  let n = Named("a" ++ "b", "c" ++ "de")
  let kept = match n { Named both => both }
  let len = match n { Named both => both.0.len() + both.1.len() }
  u8(len + kept.1.len())
}`
	// ASan first: the LSan run skips off Linux, and a skip ends the test.
	if got := buildAndRunASanWithPrelude(t, src); got != 8 {
		t.Errorf("under ASan: exited %d; want 8", got)
	}
	if got := buildAndRunLSanWithPrelude(t, src); got != 8 {
		t.Errorf("exited %d; want 8 (1 is LeakSanitizer reporting a leak)", got)
	}
}
