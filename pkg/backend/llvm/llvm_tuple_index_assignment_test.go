package llvm

import (
	"strings"
	"testing"
)

// **A tuple position is written in place** (09/13), in every path a field write takes: a
// local, through a struct field and an array element, a named tuple, a compound operator,
// a `mut` parameter, a tuple-assignment swap, a `shared` tuple seen through an alias, a
// pointer path, and `&mut` of the position itself.
func TestExec_TupleIndexAssignment(t *testing.T) {
	t.Parallel()
	out := buildAndRunWithPrelude(t, `module main
tuple Pair(i64, string)
struct Holder { t: (i64, i64) }
let bump = (p: mut (i64, i64)) -> void => { p.0 += 1 }
let main = () -> void => {
  var p = (1, 2)
  p.0 = 10
  p.1 += 5
  bump(p)
  var q = Pair(1, "a")
  q.1 = "b"
  var h = Holder { t: (3, 4) }
  h.t.0 = 30
  var xs: [](i64, i64) = [(1, 2)]
  xs[0].1 = 20
  (p.0, p.1) = (p.1, p.0)
  let mut s: shared (i64, string) = (1, "a")
  let alias = s
  s.1 = "seen"
  var hh = Holder { t: (5, 6) }
  let hp = unsafe { &mut hh }
  unsafe { hp^.t.1 = 60 }
  var r = (0, 0)
  let rp = unsafe { &mut r.1 }
  unsafe { rp^ = 70 }
  println("${p.0} ${p.1} ${q.1} ${h.t.0} ${xs[0].1} ${alias.1} ${hh.t.1} ${r.1}")
}
`, "")
	want := "7 11 b 30 20 seen 60 70"
	if got := strings.TrimSpace(out); got != want {
		t.Errorf("got %q; want %q", got, want)
	}
}

// Replacing a managed element releases what it held and keeps what it now holds, with no
// leak and no double release.
func TestLSan_TupleIndexAssignmentOfAManagedElement(t *testing.T) {
	t.Parallel()
	const src = `module main
let main = () -> u8 => {
  var p = (1, "a" ++ "b")
  p.1 = "cd" ++ "e"
  var xs: [](string, i64) = [("x" ++ "y", 1)]
  xs[0].0 = "zz" ++ "z"
  u8(p.1.len() + xs[0].0.len())
}`
	if got := buildAndRunLSanWithPrelude(t, src); got != 6 {
		t.Errorf("exited %d; want 6 (1 is LeakSanitizer reporting a leak)", got)
	}
}
