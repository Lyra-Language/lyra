package driver_test

import (
	"testing"

	diag "github.com/Lyra-Language/lyra/pkg/diagnostic"
)

const aliasedShapes = `module shapes
pub trait Shape {
  area: (Self) -> i64
}
pub let total<s> where s: Shape = (x: s) -> i64 => x.area()
`

// An impl written through an aliased trait import satisfies a bound naming the trait, and
// the import it is written through is used (lyra-W004): erased to the trait's own name, the
// impl head no longer says `Figure`, so the use is handed to the check (eraseTraitAliases).
func TestTraitAlias_AnImplThroughItSatisfiesTheBound(t *testing.T) {
	res := analyzeModules(t, map[string]string{
		"shapes.lyra": aliasedShapes,
		"main.lyra": `module main
import shapes
import shapes.{ Shape as Figure }
struct Square { side: i64 }
impl Figure for Square {
  area = (self) => self.side * self.side
}
let main = () -> void => println("${shapes.total(Square { side: 3 })}")
`,
	})
	for _, d := range res.Errors() {
		t.Errorf("unexpected error: %s", d.Message)
	}
	for _, d := range res.Diagnostics {
		if d.Code == diag.CodeUnusedImport {
			t.Errorf("unexpected %s: %s", d.Code, d.Message)
		}
	}
}

// An alias written nowhere is still unused: the erasure counts only what it rewrote.
func TestTraitAlias_UnwrittenIsStillUnused(t *testing.T) {
	res := analyzeModules(t, map[string]string{
		"shapes.lyra": aliasedShapes,
		"main.lyra": `module main
import shapes.{ Shape as Figure }
let main = () -> void => println("hi")
`,
	})
	unused := 0
	for _, d := range res.Diagnostics {
		if d.Code == diag.CodeUnusedImport {
			unused++
		}
	}
	if unused != 1 {
		t.Errorf("got %d lyra-W004, want 1 for the unwritten `Figure`", unused)
	}
}
