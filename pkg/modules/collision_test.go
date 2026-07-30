package modules_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Lyra-Language/lyra/pkg/driver"
	"github.com/Lyra-Language/lyra/pkg/modules"
)

func buildTree(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for name, body := range files {
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func analyze(t *testing.T, root string) *driver.Result {
	t.Helper()
	units, diags := modules.Resolve(filepath.Join(root, "app.lyra"), []string{root})
	if len(diags) != 0 {
		t.Fatalf("resolve failed: %v", diags)
	}
	return driver.AnalyzeUnits(units)
}

func errorsContaining(res *driver.Result, want string) bool {
	for _, d := range res.Errors() {
		if strings.Contains(d.Message, want) {
			return true
		}
	}
	return false
}

// Two modules defining the same function is an error, not a coin flip.
//
// RegisterFunction used to overwrite silently — harmless in a single file, where a
// redeclaration is caught earlier by the scope's own Define, but with modules it meant
// the program built and called whichever was registered last. The collector also
// dropped the error on the floor, so both halves had to be fixed.
func TestModules_DuplicateFunctionAcrossModulesIsAnError(t *testing.T) {
	root := buildTree(t, map[string]string{
		"app.lyra": "import one\nimport two\nlet main = () -> u8 => u8(helper())",
		"one.lyra": "module one\npub let helper = () -> i64 => 1",
		"two.lyra": "module two\npub let helper = () -> i64 => 2",
	})
	res := analyze(t, root)
	if !errorsContaining(res, `function "helper" is already defined`) {
		t.Errorf("expected a duplicate-function error; got %v", res.Errors())
	}
}

// The same for types, which were already rejected — pinned so the message keeps
// naming the *other* file, without which a bare line:col reads as a position in the
// file being reported on.
func TestModules_DuplicateTypeNamesTheOtherFile(t *testing.T) {
	root := buildTree(t, map[string]string{
		"app.lyra": "import one\nimport two\nlet main = () -> u8 => 0",
		"one.lyra": "module one\npub struct Point { x: i64 }",
		"two.lyra": "module two\npub struct Point { x: i64 }",
	})
	res := analyze(t, root)
	if !errorsContaining(res, "one.lyra") {
		t.Errorf("expected the duplicate-type error to name the other file; got %v", res.Errors())
	}
}

// Distinct names across modules compose: this is the case that must keep working.
func TestModules_DistinctNamesCompose(t *testing.T) {
	root := buildTree(t, map[string]string{
		"app.lyra":       "import util.math\nlet main = () -> u8 => u8(double(21))",
		"util/math.lyra": "module util.math\npub let double = (n: i64) -> i64 => n * 2",
	})
	res := analyze(t, root)
	if errs := res.Errors(); len(errs) != 0 {
		t.Errorf("expected a clean multi-module program; got %v", errs)
	}
}

// A diagnostic raised inside an imported module names that module's file, not the
// entry file — the payoff of stamping ast.Location.File at collection.
func TestModules_DiagnosticNamesTheDefiningFile(t *testing.T) {
	root := buildTree(t, map[string]string{
		"app.lyra":         "import util.broken\nlet main = () -> u8 => 0",
		"util/broken.lyra": "module util.broken\npub let oops = () -> i64 => \"not an int\"",
	})
	res := analyze(t, root)
	errs := res.Errors()
	if len(errs) == 0 {
		t.Fatal("expected a type error from the imported module")
	}
	if !strings.Contains(errs[0].Location.File, "broken.lyra") {
		t.Errorf("diagnostic should name the defining file; got %q", errs[0].Location.File)
	}
}

// A plain `import util.math` binds a *namespace*, so the module's contents are reached
// as `math.double(…)` — the semantics the grammar's `as` / `.{ }` split already implied.
func TestModules_NamespaceQualifiedCall(t *testing.T) {
	root := buildTree(t, map[string]string{
		"app.lyra":       "import util.math\nlet main = () -> u8 => u8(math.double(21))",
		"util/math.lyra": "module util.math\npub let double = (n: i64) -> i64 => n * 2",
	})
	res := analyze(t, root)
	if errs := res.Errors(); len(errs) != 0 {
		t.Errorf("expected a namespaced call to type-check; got %v", errs)
	}
}

// The membership check is not decoration. Top-level names are program-wide unique
// today (a cross-module duplicate is rejected), so a bare lookup would happily resolve
// `math.secret` to *another* module's `secret` — binding a reference the source never
// made, and silently.
func TestModules_NamespaceCannotReachAnotherModule(t *testing.T) {
	root := buildTree(t, map[string]string{
		"app.lyra":         "import util.math\nimport other.thing\nlet main = () -> u8 => u8(math.secret())",
		"util/math.lyra":   "module util.math\npub let double = (n: i64) -> i64 => n * 2",
		"other/thing.lyra": "module other.thing\npub let secret = () -> i64 => 9",
	})
	res := analyze(t, root)
	if !errorsContaining(res, `module "util.math" has no member "secret"`) {
		t.Errorf("expected the namespace to reject a member of another module; got %v", res.Errors())
	}
}

// A member the module genuinely lacks is reported against the module, not as a stray
// field read on an undefined value.
func TestModules_UnknownNamespaceMember(t *testing.T) {
	root := buildTree(t, map[string]string{
		"app.lyra":       "import util.math\nlet main = () -> u8 => u8(math.nosuch(1))",
		"util/math.lyra": "module util.math\npub let double = (n: i64) -> i64 => n * 2",
	})
	res := analyze(t, root)
	if !errorsContaining(res, `has no member "nosuch"`) {
		t.Errorf("expected an unknown-member error; got %v", res.Errors())
	}
}

// A value in scope shadows a namespace of the same name, so `math.double` is an
// ordinary field read — the namespace must not hijack it.
func TestModules_LocalValueShadowsNamespace(t *testing.T) {
	root := buildTree(t, map[string]string{
		"app.lyra": `import util.math
struct Holder { double: i64 }
let main = () -> u8 => {
  let math = Holder { double: 7 }
  u8(math.double)
}`,
		"util/math.lyra": "module util.math\npub let double = (n: i64) -> i64 => n * 2",
	})
	res := analyze(t, root)
	if errs := res.Errors(); len(errs) != 0 {
		t.Errorf("a local value should shadow the namespace; got %v", errs)
	}
}

// `pub` is what crosses a module boundary; within a module everything is visible.
//
// Both halves matter. Enforcing `pub` *inside* a module would make a private helper
// unusable by the module that declared it, which is the opposite of the point — so
// publicFn calling helperFn below must stay legal.
func TestModules_PubGatesCrossModuleAccess(t *testing.T) {
	lib := "module lib.api\n" +
		"pub struct Config { level: i64 }\n" +
		"struct Internal { seed: i64 }\n" +
		"pub let publicFn = () -> i64 => helperFn()\n" +
		"let helperFn = () -> i64 => 21\n"

	cases := []struct {
		name      string
		app       string
		wantError string // empty = must type-check
	}{
		{
			"exported function through a namespace",
			`import lib.api
			 let main = () -> u8 => u8(api.publicFn())`,
			"",
		},
		{
			"private function through a namespace",
			`import lib.api
			 let main = () -> u8 => u8(api.helperFn())`,
			`helperFn is private to module "lib.api"`,
		},
		{
			// The hole a namespace-only check would leave: top-level names share one
			// namespace, so a bare call reaches another module's function without ever
			// naming the module. This is what makes a private function actually
			// private rather than merely unreachable through a namespace.
			"private function through a bare call",
			`import lib.api
			 let main = () -> u8 => u8(helperFn())`,
			`helperFn is private to module "lib.api"`,
		},
		{
			"exported type as an annotation",
			`import lib.api
			 let main = () -> u8 => {
				let c: Config = Config { level: 3 }
				u8(c.level)
			}`,
			"",
		},
		{
			// Types are checked in resolveType, the one place every named type reaches
			// its declaration.
			"private type as an annotation",
			`import lib.api
			 let main = () -> u8 => {
				let c: Internal = Internal { seed: 3 }
				u8(c.seed)
			}`,
			`Internal is private to module "lib.api"`,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			root := buildTree(t, map[string]string{
				"app.lyra":     c.app,
				"lib/api.lyra": lib,
			})
			res := analyze(t, root)
			if c.wantError == "" {
				if errs := res.Errors(); len(errs) != 0 {
					t.Errorf("expected this to be allowed; got %v", errs)
				}
				return
			}
			if !errorsContaining(res, c.wantError) {
				t.Errorf("expected %q; got %v", c.wantError, res.Errors())
			}
		})
	}
}

// A single-file program has no module, so every name is same-module and nothing is
// gated — `pub` must not change the meaning of the programs that already exist.
func TestModules_SingleFileIsUnaffectedByPub(t *testing.T) {
	root := buildTree(t, map[string]string{
		"app.lyra": `
			let helper = () -> i64 => 21
			let main = () -> u8 => u8(helper() * 2)`,
	})
	res := analyze(t, root)
	if errs := res.Errors(); len(errs) != 0 {
		t.Errorf("a single-file program must be unaffected by pub; got %v", errs)
	}
}
