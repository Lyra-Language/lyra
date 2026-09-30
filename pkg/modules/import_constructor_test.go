package modules_test

import (
	"testing"

	diag "github.com/Lyra-Language/lyra/pkg/diagnostic"
)

// **An imported constructor is used wherever it is written** — matched on in a pattern,
// or built — and `lyra-W004` said otherwise until 09/29: patterns are not expressions, so
// the reference walk never entered them, and a constructor call collects to a
// DataConstructorExpr or a named TupleLiteralExpr rather than an identifier. Vega's
// sources carried nine such warnings, every one advice that would break the build.
func TestModules_AnImportedConstructorIsUsed(t *testing.T) {
	for name, app := range map[string]string{
		"matched": `import util.shapes.{ Shape, Circle, Square, unit }
let area = (s: Shape) -> i64 => match s {
  Circle => 3,
  Square(n) => n * n,
}
let main = () -> u8 => u8(area(unit()))`,
		"built": `import util.shapes.{ Shape, Circle, Square }
let main = () -> u8 => {
  let shapes: []Shape = [Circle, Square(2)]
  u8(shapes.len())
}`,
		"a newtype built": `import util.shapes.{ Label }
let main = () -> u8 => {
  let l = Label("x")
  0
}`,
		"compared": `import util.shapes.{ Circle, unit }
let main = () -> u8 => if unit() == Circle { 1 } else { 0 }`,
	} {
		t.Run(name, func(t *testing.T) {
			root := buildTree(t, map[string]string{
				"app.lyra": app,
				"util/shapes.lyra": "module util.shapes\npub data Shape = Circle | Square(i64)\n" +
					"pub newtype Label = string\npub let unit = pure () -> Shape => Square(1)",
			})
			res := analyze(t, root)
			if errs := res.Errors(); len(errs) != 0 {
				t.Fatalf("expected a clean program; got %v", errs)
			}
			for _, d := range res.Diagnostics {
				if d.Code == diag.CodeUnusedImport {
					t.Errorf("every imported name is written, so none is unused; got %v", d)
				}
			}
		})
	}
}

// A constructor imported and never written still warns, beside one that is.
func TestModules_AnUnwrittenConstructorStillWarns(t *testing.T) {
	root := buildTree(t, map[string]string{
		"app.lyra": `import util.shapes.{ Circle, Square, unit }
let main = () -> u8 => match unit() {
  Circle => 1,
  _ => 0,
}`,
		"util/shapes.lyra": "module util.shapes\npub data Shape = Circle | Square(i64)\n" +
			"pub let unit = pure () -> Shape => Square(1)",
	})
	res := analyze(t, root)
	if !warnsWith(res, diag.CodeUnusedImport, "Square") {
		t.Errorf("Square is never written; expected it to warn, got %v", res.Diagnostics)
	}
	if warnsWith(res, diag.CodeUnusedImport, "Circle") {
		t.Errorf("Circle is matched on; expected no warning for it, got %v", res.Diagnostics)
	}
}
