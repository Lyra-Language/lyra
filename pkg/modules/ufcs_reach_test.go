package modules_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/Lyra-Language/lyra/pkg/modules"

	"github.com/Lyra-Language/lyra/pkg/driver"
)

// receiverPrelude is testPrelude plus a method — a function whose first parameter is
// `self`, which is all a method is here. The shared prelude has none, and a receiver is
// exactly what these two tests turn on.
const receiverPrelude = testPrelude + `
pub let trim = (self: string) -> string => self
`

func analyzeWithReceiverPrelude(t *testing.T, files map[string]string) *driver.Result {
	t.Helper()
	files["std/prelude.lyra"] = receiverPrelude
	root := buildTree(t, files)
	units, diags := modules.Resolve(filepath.Join(root, "app.lyra"), []string{root},
		modules.Options{Prelude: modules.PreludeModule})
	if len(diags) != 0 {
		t.Fatalf("resolve failed: %v", diags)
	}
	return driver.AnalyzeUnits(units)
}

// **A module's private function is not a method anywhere else** — not even ambiguously.
//
// Method calls used to be answered by "is that module imported here", which admitted
// every function it declared, including the ones it kept to itself. Importing `here`
// from a module that privately declares its own `trim` made `"x".trim()` ambiguous
// between the prelude's and a function this file cannot name, let alone call: the
// collision was the only observable consequence of a name that was supposed to be
// invisible. Found 09/22 while checking whether a shadow really stops at its module.
func TestUFCS_APrivateFunctionOfAnotherModuleIsNotAMethodHere(t *testing.T) {
	res := analyzeWithReceiverPrelude(t, map[string]string{
		"lib.lyra": `module lib
let trim = (self: string) -> string => self
pub let here = () -> i64 => 1`,
		"app.lyra": `import lib.{ here }
let main = () -> u8 => {
  let trimmed = "  x  ".trim()
  u8(here())
}`,
	})
	if errs := res.Errors(); len(errs) != 0 {
		t.Errorf("another module's private trim must not reach this call; got %v", errs)
	}
}

// The mirror, so the test above cannot pass by making every method unreachable: the same
// function *exported* is a candidate from any file importing its module, whether or not
// the import names it — and a second candidate for one receiver is the genuine ambiguity
// the check above must still report. The import list is deliberately unchanged from the
// test above: `pub` is the entire difference between the two.
func TestUFCS_AnExportedFunctionOfAnImportedModuleIsAMethodHere(t *testing.T) {
	res := analyzeWithReceiverPrelude(t, map[string]string{
		"lib.lyra": `module lib
pub let trim = (self: string) -> string => self
pub let here = () -> i64 => 1`,
		"app.lyra": `import lib.{ here }
let main = () -> u8 => {
  let trimmed = "  x  ".trim()
  u8(here())
}`,
	})
	if !hasErrorContaining(res, "ambiguous") {
		t.Errorf("two trims taking a string is an ambiguity, not a silent pick; got %v", res.Diagnostics)
	}
}

// A shadow warning says *why* this is a shadow and not an overload, since receiver
// overloading means a same-named declaration is often perfectly fine. The two reasons
// are the two ways a name cannot be told apart: no receiver at all, or the same one.
func TestPrelude_TheShadowWarningSaysWhyItIsNotAnOverload(t *testing.T) {
	for _, c := range []struct {
		name, decl, want string
	}{
		{
			"no receiver", "let trim = (n: i64) -> i64 => n",
			"takes no `self` receiver",
		},
		{
			"the same receiver", "let trim = (self: string) -> string => self",
			"both take a `string` receiver",
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			res := analyzeWithReceiverPrelude(t, map[string]string{
				"app.lyra": c.decl + "\nlet main = () -> u8 => 1",
			})
			if !warningsContaining(res, c.want) {
				t.Errorf("want the reason %q; got %v", c.want, res.Diagnostics)
			}
		})
	}
}

func hasErrorContaining(res *driver.Result, substring string) bool {
	for _, d := range res.Errors() {
		if strings.Contains(d.Message, substring) {
			return true
		}
	}
	return false
}
