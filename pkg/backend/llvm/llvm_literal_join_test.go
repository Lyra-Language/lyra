package llvm

import "testing"

// Untyped literals of both signs joined inside tuples and arrays (09/29, the typechecker's
// structuralJoin) lower to the values written: a direction table, nested arrays, tuple
// branches, and a table an annotation narrows.
func TestExec_LiteralJoinInsideTuplesAndArrays(t *testing.T) {
	t.Parallel()
	out := buildAndRunWithPrelude(t, `
let pick = (c: bool) -> (i64, i64) => if c { (-1, 0) } else { (1, 0) }

let main = () -> void => {
  var sum = 0
  for (dx, dy) in [(-1, 0), (1, 0), (0, -3), (0, 1)] { sum += dx * 10 + dy }
  let rows = [[-1], [1, 2]]
  let small: [](u8, i8) = [(1, -1), (2, 1)]
  println("${sum} ${rows[0][0] + rows[1][1]} ${pick(true).0} ${pick(false).0} ${small[0].1}")
}`, "")
	if want := "-2 1 -1 1 -1\n"; out != want {
		t.Errorf("printed %q; want %q", out, want)
	}
}
