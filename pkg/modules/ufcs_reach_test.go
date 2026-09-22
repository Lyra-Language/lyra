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

// **The import list breaks a tie, but never gates the call.** Two modules exporting a
// `squish` that takes a string leaves a call with two candidates and no way to choose;
// naming one in the import is the plainest statement of which is meant, and it is the fix
// the ambiguity message asks for. Until 09/22 writing it changed nothing — the list
// decided bare names and was not consulted here — so the obvious fix silently failed.
//
// Gating was measured against this repo and rejected: requiring the name would cost 44
// call sites across 13 files, most of them accessors on types that cannot have public
// fields (`d.day()`, `v.field(…)`), and would put names like `day` and `value` into the
// file's bare scope, where they shadow the locals those calls are usually assigned to.
func TestUFCS_TheImportListBreaksATie(t *testing.T) {
	both := map[string]string{
		"a.lyra": `module a
pub let squish = (self: string) -> string => self
pub let alpha = () -> i64 => 1`,
		"b.lyra": `module b
pub let squish = (self: string) -> string => self
pub let beta = () -> i64 => 2`,
	}
	app := func(importA string) map[string]string {
		files := map[string]string{
			"app.lyra": importA + `
import b.{ beta }
let main = () -> u8 => {
  let squished = "x".squish()
  u8(alpha() + beta())
}`,
		}
		for k, v := range both {
			files[k] = v
		}
		return files
	}

	t.Run("named in one import, resolved", func(t *testing.T) {
		res := analyzeWithReceiverPrelude(t, app("import a.{ alpha, squish }"))
		if errs := res.Errors(); len(errs) != 0 {
			t.Errorf("naming one is the answer to the ambiguity; got %v", errs)
		}
	})

	// The mirror: without the name the tie stands, so the test above cannot be passing
	// by having stopped reporting ambiguity altogether.
	t.Run("named in neither, still ambiguous", func(t *testing.T) {
		res := analyzeWithReceiverPrelude(t, app("import a.{ alpha }"))
		if !hasErrorContaining(res, "is ambiguous") {
			t.Errorf("two candidates and no choice is still ambiguous; got %v", res.Diagnostics)
		}
		// The advice has to be the one that works from here.
		if !hasErrorContaining(res, "import the one you mean by name (`import a.{ squish }`)") {
			t.Errorf("the message should offer the import fix; got %v", res.Errors())
		}
	})

	// Naming both is the file asking for both: the list has been used and did not settle
	// it, so the advice changes rather than repeating what the reader already did.
	t.Run("named in both, the advice changes", func(t *testing.T) {
		files := app("import a.{ alpha, squish }")
		files["app.lyra"] = strings.Replace(files["app.lyra"],
			"import b.{ beta }", "import b.{ beta, squish }", 1)
		res := analyzeWithReceiverPrelude(t, files)
		if !hasErrorContaining(res, "drop the one you do not mean from its import") {
			t.Errorf("want the drop-one advice; got %v", res.Errors())
		}
	})
}

// A method the import list does not name is still callable — the tiebreak above must not
// have quietly become a gate. This is the rule the whole repo relies on: importing
// `parse_args` from a module is enough to call `args.value(…)`.
func TestUFCS_AMethodNeedNotBeNamedInTheImport(t *testing.T) {
	res := analyzeWithReceiverPrelude(t, map[string]string{
		"lib.lyra": `module lib
pub let squish = (self: string) -> string => self
pub let alpha = () -> i64 => 1`,
		"app.lyra": `import lib.{ alpha }
let main = () -> u8 => {
  let squished = "x".squish()
  u8(alpha())
}`,
	})
	if errs := res.Errors(); len(errs) != 0 {
		t.Errorf("the import list breaks ties, it does not gate calls; got %v", errs)
	}
}
