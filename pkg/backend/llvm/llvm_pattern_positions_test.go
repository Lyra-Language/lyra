package llvm

import (
	"strings"
	"testing"
)

// **A tuple rest covers the positions between the elements around it**, and binds them as
// a tuple. It parsed from the start and worked nowhere until 09/13: the name was never
// bound, an element after the rest paired with the wrong position, the backend refused
// the pattern and a match arm reported an arity error for it. Every form here goes through
// ast.MatchPositions — a `let`, a match arm (testing a position after the rest), an
// `if let`, a parameter and a constructor payload.
func TestExec_TupleRestPositions(t *testing.T) {
	t.Parallel()
	out := buildAndRunWithPrelude(t, `
module main
data Shape = Tri(i64, string, i64) | Dot
let pick = ((a, ...rest): (i64, string, bool)) -> string => if rest.1 { rest.0 } else { "no" }
let main = () -> void => {
  let (a, ...mid, z) = (1, "two", 3.5, true)
  println("${a} ${mid.0} ${mid.1} ${z}")
  let (...none, x, y) = (4, 5)
  println("${x + y} ${none == ()}")
  match (1, "s", false, 9) {
    (0, ...r) => println("zero"),
    (n, ...r, 9) => println("nine ${n} ${r.0}"),
    _ => println("other"),
  }
  match Tri(7, "t", 8) { Tri(i, ...more) => println("${i} ${more.0} ${more.1}"), Dot => println("dot") }
  if let Tri(...all) = Tri(1, "u", 2) { println("${all.2}") }
  println(pick((1, "hi", true)))
}
`, "")
	want := "1 two 3.5 true\n9 true\nnine 1 s\n7 t 8\n2\nhi"
	if got := strings.TrimSpace(out); got != want {
		t.Errorf("got %q; want %q", got, want)
	}
}

// **A regex is a pattern at any depth**, not only at an arm's top. The collector dropped
// one inside a tuple or array pattern — `(x, r"^a$")` collected as `(x)` — and the backend
// had no test for one nested in a tuple, a struct field, an array element or a payload, so
// `Some(r"^a")` was refused and the others never arrived (09/13). A regex matches the whole
// string, as it does at the top of an arm.
func TestExec_NestedRegexPatterns(t *testing.T) {
	t.Parallel()
	out := buildAndRunWithPrelude(t, `
module main
struct Tag { name: string, n: i64 }
let main = () -> void => {
  match (1, "abc") { (x, r"a.*") => println("tuple ${x}"), _ => println("tuple miss") }
  match Tag { name: "zz", n: 2 } { Tag { name: r"a.*", n } => println("struct ${n}"), _ => println("struct miss") }
  let xs: []string = ["ab", "q"]
  match xs { [r"a.", y] => println("array ${y}"), _ => println("array miss") }
  match Some("b") { Some(r"a") => println("some a"), Some(r"[b-z]") => println("some b-z"), _ => println("none") }
}
`, "")
	want := "tuple 1\nstruct miss\narray q\nsome b-z"
	if got := strings.TrimSpace(out); got != want {
		t.Errorf("got %q; want %q", got, want)
	}
}

// A rest's tuple is a copy of managed elements the pattern's value still owns; binding
// one from a `let` must take its own references, and a match arm's must not leak.
func TestLSan_TupleRestOverManagedElements(t *testing.T) {
	t.Parallel()
	const src = `module main
let main = () -> u8 => {
  let s = "hello" ++ " world"
  let (n, ...r) = (1, s, [s, s])
  let m = match (s, [s], 2) { (_, ...tail) => tail.0.len() + tail.1, }
  u8(r.0.len() + r.1.len() + n + m)
}`
	if got := buildAndRunLSanWithPrelude(t, src); got != 17 {
		t.Errorf("exited %d; want 17 (1 is LeakSanitizer reporting a leak)", got)
	}
}
