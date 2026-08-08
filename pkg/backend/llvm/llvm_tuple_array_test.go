package llvm

import (
	"strings"
	"testing"
)

// An array whose element is an **anonymous tuple** (08/08). The grammar's element-type
// rule was `type` minus the modifier forms and minus `void`, and the anonymous tuple, the
// raw pointer and the anonymous struct had simply never been added — so `[](i64, string)`
// was a syntax error in every position that rule feeds, while `[]Pair` was fine. The
// workaround was to name the tuple, which is the one thing an anonymous tuple exists not
// to require.

func TestExec_ArrayOfAnonymousTuples(t *testing.T) {
	t.Parallel()
	const src = `
module main
let main = () -> void => {
  let xs: [](i64, string) = [(1, "a"), (2, "b")];
  let ys: [2](i64, i64) = [(1, 2), (3, 4)];
  println("${xs.len()} ${xs[0].0} ${xs[1].1} ${ys[1].0}");
}
`
	if got := strings.TrimSpace(buildAndRunWithPrelude(t, src, "")); got != "2 1 b 3" {
		t.Errorf("array of tuples = %q; want \"2 1 b 3\"", got)
	}
}

// Iterating one, which is what the type is for — a list of pairs read back element-wise.
func TestExec_IterateAnArrayOfTuples(t *testing.T) {
	t.Parallel()
	const src = `
module main
let main = () -> void => {
  let xs: [](i64, string) = [(1, "a"), (2, "b")];
  for p in xs { println("${p.0}=${p.1}") };
}
`
	if got := strings.TrimSpace(buildAndRunWithPrelude(t, src, "")); got != "1=a\n2=b" {
		t.Errorf("iteration = %q; want \"1=a\\n2=b\"", got)
	}
}

// A **managed** element, since a tuple carrying a string is the case where the array's
// drop glue has to walk into the element rather than treat it as a scalar.
func TestExec_ArrayOfTuplesWithManagedPayload(t *testing.T) {
	t.Parallel()
	const src = `
module main
let main = () -> void => {
  let s = "ab" ++ "cd";
  let xs: [](i64, string) = [(1, s), (2, s)];
  println("${xs[0].1} ${xs[1].1}");
}
`
	if got := strings.TrimSpace(buildAndRunWithPrelude(t, src, "")); got != "abcd abcd" {
		t.Errorf("managed tuple elements = %q; want \"abcd abcd\"", got)
	}
}
