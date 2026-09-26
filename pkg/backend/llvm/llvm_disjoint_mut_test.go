package llvm

import (
	"strings"
	"testing"
)

// **Two disjoint fields may be passed `mut` to one call** (09/25), and each write lands in
// its own field. The typechecker admits the call now that exclusivity compares places
// rather than roots; this is the other half — that lowering hands each parameter a pointer
// to *its* field, so neither write is lost or crosses over. Nested fields, a tuple's
// positions, and a `ref` read beside a `mut` write of a sibling are all asserted.
func TestExec_DisjointFieldsPassedMut(t *testing.T) {
	t.Parallel()
	out := buildAndRunWithPrelude(t, `
module main
struct Point { x: i64, y: i64 }
struct Pair { left: Point, right: Point }
struct Nest { inner: Pair, other: Point }
let two = (a: mut Point, b: mut Point) -> void => {
  a.x = a.x + 10
  b.y = b.y + 20
}
let peek = (a: ref Point, b: mut Point) -> i64 => {
  b.x = 99
  a.x
}
let main = () -> void => {
  var s = Pair { left: Point { x: 1, y: 2 }, right: Point { x: 3, y: 4 } }
  two(s.left, s.right)
  print("${s.left.x} ${s.left.y} ${s.right.x} ${s.right.y} ")
  var n = Nest { inner: s, other: Point { x: 5, y: 6 } }
  two(n.inner.right, n.other)
  print("${n.inner.right.x} ${n.other.y} ${n.inner.left.x} ")
  var t = (Point { x: 7, y: 8 }, Point { x: 9, y: 10 })
  two(t.0, t.1)
  print("${t.0.x} ${t.1.y} ")
  print("${peek(s.left, s.right)} ${s.right.x}")
}
`, "")
	want := "11 2 3 24 13 26 11 17 30 11 99"
	if got := strings.TrimSpace(out); got != want {
		t.Errorf("disjoint mut fields = %q; want %q", got, want)
	}
}
