package modules_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/Lyra-Language/lyra/pkg/driver"
	"github.com/Lyra-Language/lyra/pkg/modules"
)

const testPrelude = `module std.prelude

pub data Maybe<t> = None | Some(t)
pub data Result<t, e> = Ok(t) | Err(e)

pub let unwrapOr = (m: Maybe<i64>, fallback: i64) -> i64 => match m {
  Some(v) => v,
  None => fallback,
}
`

// analyzeWithPrelude resolves with the prelude enabled, as lyrac does.
func analyzeWithPrelude(t *testing.T, root string) *driver.Result {
	t.Helper()
	units, diags := modules.Resolve(filepath.Join(root, "app.lyra"), []string{root},
		modules.Options{Prelude: modules.PreludeModule})
	if len(diags) != 0 {
		t.Fatalf("resolve failed: %v", diags)
	}
	return driver.AnalyzeUnits(units)
}

func warningsContaining(res *driver.Result, want string) bool {
	for _, d := range res.Diagnostics {
		if strings.Contains(d.Message, want) {
			return true
		}
	}
	return false
}

// The prelude is reachable without an import — that is what "implicit" means. It is
// still an ordinary module with `pub` exports, so this also confirms `pub` is what makes
// its names visible rather than the prelude bypassing visibility.
func TestPrelude_AvailableWithoutImport(t *testing.T) {
	root := buildTree(t, map[string]string{
		"std/prelude.lyra": testPrelude,
		"app.lyra": `let main = () -> u8 => {
  let m: Maybe<i64> = Some(40)
  u8(unwrapOr(m, 0) + 2)
}`,
	})
	res := analyzeWithPrelude(t, root)
	if errs := res.Errors(); len(errs) != 0 {
		t.Errorf("expected prelude names to resolve unqualified; got %v", errs)
	}
}

// A declaration may take a prelude name: it warns, and the local one wins.
//
// This has to be a warning. The prelude is implicitly in scope everywhere, so treating
// the clash the way two user modules' clash is treated would make every name the prelude
// exports permanently unusable — and adding a name to the prelude later would break
// programs that never mentioned it.
func TestPrelude_UserDeclarationShadowsWithWarning(t *testing.T) {
	for _, c := range []struct{ name, app string }{
		{"function", "let unwrapOr = (a: i64, b: i64) -> i64 => 99\nlet main = () -> u8 => u8(unwrapOr(1, 2))"},
		{"type", "data Maybe<t> = None | Some(t)\nlet main = () -> u8 => 1"},
	} {
		t.Run(c.name, func(t *testing.T) {
			root := buildTree(t, map[string]string{
				"std/prelude.lyra": testPrelude,
				"app.lyra":         c.app,
			})
			res := analyzeWithPrelude(t, root)
			if errs := res.Errors(); len(errs) != 0 {
				t.Fatalf("shadowing the prelude must not be an error; got %v", errs)
			}
			if !warningsContaining(res, "shadows the prelude's") {
				t.Errorf("expected a shadowing warning; got %v", res.Diagnostics)
			}
		})
	}
}

// The prelude does not import itself — otherwise it could not be compiled or tested
// like any other module.
func TestPrelude_CompilesStandalone(t *testing.T) {
	root := buildTree(t, map[string]string{"app.lyra": testPrelude})
	units, diags := modules.Resolve(filepath.Join(root, "app.lyra"), []string{root},
		modules.Options{Prelude: modules.PreludeModule})
	if len(diags) != 0 {
		t.Fatalf("resolve failed: %v", diags)
	}
	if len(units) != 1 {
		t.Errorf("the prelude should not pull in a second copy of itself; got %d units", len(units))
	}
}

// A missing prelude is not an error. The standard library is found by searching the
// roots, and a program compiled where there is none — most of the test suite — must
// still build. Requiring it would turn "no std/ here" into a compile failure for every
// program.
func TestPrelude_AbsentIsNotAnError(t *testing.T) {
	root := buildTree(t, map[string]string{
		"app.lyra": "let main = () -> u8 => 7",
	})
	units, diags := modules.Resolve(filepath.Join(root, "app.lyra"), []string{root},
		modules.Options{Prelude: modules.PreludeModule})
	if len(diags) != 0 {
		t.Errorf("a missing prelude must not be an error; got %v", diags)
	}
	if len(units) != 1 {
		t.Errorf("expected just the entry unit; got %d", len(units))
	}
}

// Disabling the prelude actually disables it — the opt-out is what lets the prelude
// itself be built by a compiler that is not yet handing it to everything.
func TestPrelude_OptOut(t *testing.T) {
	root := buildTree(t, map[string]string{
		"std/prelude.lyra": testPrelude,
		"app.lyra":         "let main = () -> u8 => u8(unwrapOr(Some(1), 0))",
	})
	units, _ := modules.Resolve(filepath.Join(root, "app.lyra"), []string{root}, modules.Options{})
	res := driver.AnalyzeUnits(units)
	if len(res.Errors()) == 0 {
		t.Error("expected prelude names to be unavailable when the prelude is disabled")
	}
}
