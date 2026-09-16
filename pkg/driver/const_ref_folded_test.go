package driver_test

import (
	"strings"
	"testing"

	diag "github.com/Lyra-Language/lyra/pkg/diagnostic"
	"github.com/Lyra-Language/lyra/pkg/driver"
)

// **A name the typechecker folds away is still a use.** These run the whole pipeline on
// purpose: the collector leaves `#[0; N]` holding the identifier, and it is
// `arrayRepeatCount` — during typechecking — that rewrites the count to a literal so the
// backend can fold it without a `const` resolver. Every later pass asking "which names does
// this program use" then sees a program that never mentions `N`.
//
// The unit harnesses in pkg/analyzer/checker stop at the collector, so they cannot see this
// at all; a test there passes whether the bug is present or not.
func TestAnalyze_AConstUsedOnlyAsAFixedArrayCountIsUsed(t *testing.T) {
	res := driver.Analyze([]byte(`let main = () -> void => {
  const N = 3
  let xs: [3]i64 = #[0; N]
  println("${xs.len()}")
}
`))
	for _, d := range res.Diagnostics {
		if d.Code == diag.CodeUnusedVariable {
			t.Errorf("`N` is the array's count, so it is used; got %v", d)
		}
	}
}

// Arithmetic in the count erases the name just the same, so the recovery walks the whole
// expression rather than matching a bare identifier.
func TestAnalyze_AConstInsideACountExpressionIsUsed(t *testing.T) {
	res := driver.Analyze([]byte(`let main = () -> void => {
  const N = 3
  let xs: [6]i64 = #[0; N * 2]
  println("${xs.len()}")
}
`))
	for _, d := range res.Diagnostics {
		if d.Code == diag.CodeUnusedVariable {
			t.Errorf("`N` is used inside the count expression; got %v", d)
		}
	}
}

// A const that really is unused still warns — the recovery must not blanket-exempt consts.
func TestAnalyze_AGenuinelyUnusedConstStillWarns(t *testing.T) {
	res := driver.Analyze([]byte(`let main = () -> void => {
  const UNUSED = 3
  println("hi")
}
`))
	found := false
	for _, d := range res.Diagnostics {
		if d.Code == diag.CodeUnusedVariable && strings.Contains(d.Message, "UNUSED") {
			found = true
		}
	}
	if !found {
		t.Errorf("an unused const should still warn; got %v", res.Diagnostics)
	}
}
