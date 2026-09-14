package llvm

import (
	"strings"
	"testing"
)

// A `const` range-pattern bound lowers as the literal it folds to (09/13) — top-level,
// derived, negative, local, nested in a payload, in `if let`, in a tuple, and on a float.
// The backend reads only literals, so a bound the typechecker failed to fold is its loud
// "unsupported range start" rather than a wrong branch.
func TestExec_ConstRangePatternBounds(t *testing.T) {
	t.Parallel()
	src := `
const LOW = 10
const HIGH = LOW * 2
const NEG = -5
const HALF = 0.5
let bucket = (n: u8) -> string => match n {
  ..<LOW => "a",
  LOW..<=HIGH => "b",
  21.. => "c",
}
let signed = (n: i64) -> string => match n {
  ..<NEG => "vn",
  NEG..<0 => "n",
  _ => "p",
}
let fl = (x: f64) -> string => match x {
  0.0..<HALF => "lo",
  _ => "hi",
}
let nested = (m: Maybe<i64>) -> string => match m {
  Some(LOW..<=HIGH) => "mid",
  Some(_) => "some",
  None => "none",
}
let tup = (p: (i64, i64)) -> i64 => match p {
  (LOW..<=HIGH, NEG..) => 1,
  _ => 0,
}
let main = () -> void => {
  const LIM = 3
  let local = match 4 { 0..<LIM => "in", _ => "out" }
  var hit = "no"
  if let Some(LOW..<=HIGH) = Some(11) { hit = "yes" }
  println("${bucket(9)} ${bucket(10)} ${bucket(20)} ${bucket(21)} ${signed(-6)} ${signed(-5)} ${signed(0)} ${fl(0.25)} ${fl(0.5)} ${nested(Some(12))} ${nested(Some(1))} ${nested(None)} ${tup((15, -5))}${tup((15, -6))} ${local} ${hit}")
}
`
	want := "a b b c vn n p lo hi mid some none 10 out yes"
	if got := strings.TrimSpace(buildAndRunWithPrelude(t, src, "")); got != want {
		t.Errorf("got %q; want %q", got, want)
	}
}
