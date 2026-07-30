package modules_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/Lyra-Language/lyra/pkg/ast"
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

// A shadow reaches exactly as far as the module that declared it.
//
// This is the limitation the single namespace used to impose: taking a prelude name
// withdrew the prelude's declaration *program-wide*, so a second module — one that
// never mentioned the name and cannot even see the shadowing declaration — got whatever
// the first module had bound, or, when the shadowing declaration was private, nothing at
// all. Both halves are asserted per case: the shadowing module reaches its own
// declaration, and the bystander module still reaches the prelude's.
//
// The entry file is the case worth reading twice. It has no module path, and used to
// share the global scope on the grounds that a program root has nothing to be private
// from — which also put its declarations in the scope every other module falls through
// to, so `let unwrapOr` in app.lyra silently rebound the prelude's for the whole
// program.
func TestPrelude_ShadowIsConfinedToItsModule(t *testing.T) {
	// Each case shadows the prelude's `unwrapOr` from a different place, and the shadow
	// takes **two plain integers** where the prelude's takes a `Maybe`. That difference
	// is the assertion: the shadowing module calls it with two integers and the
	// bystander calls it with a `Maybe`, so *either* resolution going the wrong way is a
	// type error rather than a silently different answer.
	for _, c := range []struct{ name, app, shadower string }{
		{
			name: "entry file",
			app: `import bystander
let unwrapOr = (a: i64, b: i64) -> i64 => a + b
let main = () -> u8 => u8(unwrapOr(1, 2) + fromBystander())`,
		},
		{
			name: "another module, privately",
			app: `import bystander
import shadower
let main = () -> u8 => u8(fromShadower() + fromBystander())`,
			shadower: `module shadower
let unwrapOr = (a: i64, b: i64) -> i64 => a + b
pub let fromShadower = () -> i64 => unwrapOr(1, 2)`,
		},
		{
			name: "another module, exported",
			app: `import bystander
import shadower
let main = () -> u8 => u8(fromShadower() + fromBystander())`,
			shadower: `module shadower
pub let unwrapOr = (a: i64, b: i64) -> i64 => a + b
pub let fromShadower = () -> i64 => unwrapOr(1, 2)`,
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			files := map[string]string{
				"std/prelude.lyra": testPrelude,
				"app.lyra":         c.app,
				"bystander.lyra": `module bystander
pub let fromBystander = () -> i64 => unwrapOr(Some(3), 0)`,
			}
			if c.shadower != "" {
				files["shadower.lyra"] = c.shadower
			}
			root := buildTree(t, files)
			res := analyzeWithPrelude(t, root)
			if errs := res.Errors(); len(errs) != 0 {
				t.Fatalf("a shadow in one module must not disturb another; got %v", errs)
			}
			if !warningsContaining(res, "shadows the prelude's") {
				t.Errorf("expected a shadowing warning; got %v", res.Diagnostics)
			}
			// Both resolution paths, since they are separate mechanisms that must
			// agree: the scope chain (what the typechecker walks) and the function key
			// (what the backend and the ownership pass ask for).
			assertResolvesTo(t, res, "bystander", "unwrapOr", modules.PreludeModule)
		})
	}
}

// A module that exported a prelude name is still reachable by namespace, and reaching it
// that way gets *its* declaration rather than the prelude's.
//
// Membership used to be established from ModuleOf, which is last-writer-wins and so
// forgets who declared a name once two modules do; the lookup behind it was by bare
// name, which is the prelude's key. Both had to become module-aware together, or
// `shadower.unwrapOr` would resolve to the prelude's signature.
func TestPrelude_ShadowedNameIsStillReachableByNamespace(t *testing.T) {
	root := buildTree(t, map[string]string{
		"std/prelude.lyra": testPrelude,
		"shadower.lyra": `module shadower
pub let unwrapOr = (a: i64, b: i64) -> i64 => a + b`,
		"app.lyra": `import shadower
let main = () -> u8 => u8(shadower.unwrapOr(1, 2) + unwrapOr(Some(3), 0))`,
	})
	res := analyzeWithPrelude(t, root)
	if errs := res.Errors(); len(errs) != 0 {
		t.Fatalf("both the namespaced and the ambient name should resolve; got %v", errs)
	}
}

// assertResolvesTo checks that name, looked up from module, resolves to a declaration
// in wantModule — through the scope chain and through SymbolTable.Functions alike.
func assertResolvesTo(t *testing.T, res *driver.Result, module, name, wantModule string) {
	t.Helper()
	st := res.SymbolTable
	moduleOf := func(loc ast.Location) string { return st.ModuleOfFile[loc.File] }

	sym, ok := st.ModuleScopeFor(module).Lookup(name)
	if !ok {
		t.Fatalf("module %q cannot resolve %q at all", module, name)
	}
	if got := moduleOf(sym.GetLocation()); got != wantModule {
		t.Errorf("scope lookup of %q from %q resolved to module %q, want %q", name, module, got, wantModule)
	}

	var file string
	for f, m := range st.ModuleOfFile {
		if m == module {
			file = f
		}
	}
	fn, ok := st.LookupFunctionFrom(name, ast.Location{File: file})
	if !ok {
		t.Fatalf("no function %q registered for module %q", name, module)
	}
	if got := moduleOf(fn.GetLocation()); got != wantModule {
		t.Errorf("function key for %q from %q resolved to module %q, want %q", name, module, got, wantModule)
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
