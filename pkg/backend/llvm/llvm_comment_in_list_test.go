package llvm

import (
	"strings"
	"testing"
)

// A comment inside a comma-separated list, in every position that has one.
//
// Comments are grammar `extras`, so they may sit between any two tokens — including between
// a comma and the element after it. All three comment kinds are **named** nodes, and every
// list collector told an element from a comma by asking `child.IsNamed()`, which is true for
// a comment. So each list collected one element too many, and the extra one was a nil
// `ast.Expression`: rule 3's typed nil, which passes an `expr == nil` test and surfaces
// somewhere else entirely.
//
// The two symptoms are why this is worth a test at the source level rather than a collector
// assertion. `f(1, /*comment*/ 2)` written across two lines reported **"expected 2
// argument(s), got 3"** — correct code accused of a different mistake, with the comment
// nowhere in the message. `[1, /*comment*/ 2]` type-checked clean and died in the backend as
// "expression lowering not implemented for <nil>". Neither points at a comment, and a
// comment inside a multi-line argument list is ordinary formatting rather than an exotic
// shape.
//
// `for … in` was the worst of the seven sites: its collector assigned the iterable from any
// named child, so a comment after `in` **overwrote** the iterable with nil rather than
// appending beside it.
func TestExec_CommentInsideEveryCommaSeparatedList(t *testing.T) {
	t.Parallel()
	out, code := buildAndRunCapture(t, `
struct Pt { x: i64, y: i64 }
let g = pure (a: i64, b: i64) -> i64 => a + b
let id<t> = pure (v: t) -> t => v
let main = () -> void => {
  println(g(1, // a call argument
    2))
  var xs: []i64 = [1, // an array element
    2]
  println(xs[1])
  let p = Pt { x: 3, // a struct field
    y: 4 }
  println(p.y)
  let t = (5, // a tuple element
    6)
  println(t.1)
  println(id::<i64>( // a generic argument
    7))
  for v in [8, // a for-in iterable
    9] {
    println(v)
  }
}
`)
	const want = "3\n2\n4\n6\n7\n8\n9"
	if code != 0 || strings.TrimSpace(out) != want {
		t.Errorf("exit %d, output %q; want exit 0 and %q — a comment in a list must not "+
			"become an element", code, strings.TrimSpace(out), want)
	}
}
