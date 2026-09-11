package driver_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	diag "github.com/Lyra-Language/lyra/pkg/diagnostic"
	"github.com/Lyra-Language/lyra/pkg/driver"
	"github.com/Lyra-Language/lyra/pkg/modules"
)

// analyzeTree is analyzeFiles with subdirectories, so a **directory** module — several
// files sharing one module path — can be written down. That is the only way to express a
// sibling file, since a module is a file *or* a directory and two flat files both saying
// `module main` are not one module: the second is simply never resolved.
func analyzeTree(t *testing.T, files map[string]string) *driver.Result {
	t.Helper()
	dir := t.TempDir()
	for name, src := range files {
		full := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	units, diags := modules.Resolve(filepath.Join(dir, "main.lyra"), []string{dir}, modules.Options{})
	if len(units) == 0 {
		t.Fatalf("resolve produced no units: %v", diags)
	}
	return driver.AnalyzeUnits(units)
}

// shadowWarnings returns the lyra-W001 messages a program produces.
func shadowWarnings(t *testing.T, files map[string]string) []string {
	t.Helper()
	var out []string
	for _, d := range analyzeTree(t, files).Diagnostics {
		if d.Code == diag.CodeShadowing {
			out = append(out, d.Message)
		}
	}
	return out
}

// The library every case below imports from — or deliberately does not.
const shadowLib = `module lib
pub let exported = pure (n: i64) -> i64 => n
pub let unimported = pure (n: i64) -> i64 => n
let private_helper = pure (n: i64) -> i64 => n
pub let also_private_user = pure (n: i64) -> i64 => private_helper(n)
`

// **A local shadows a top-level name only where that name is in scope**, and an import's
// member list is what decides — so a module's *other* exports shadow nothing.
//
// `CheckShadowing` walked the merged program and accumulated every top-level declaration
// it passed, with no notion of modules, so a local named `turns` was reported against
// `bindings.raylib`'s `turns` in a file that never imported it (09/10). Rule 4's shape:
// a pass answering a name question without asking which module is asking.
func TestShadowing_AnUnimportedExportShadowsNothing(t *testing.T) {
	got := shadowWarnings(t, map[string]string{
		"lib.lyra": shadowLib,
		"main.lyra": `module main
import lib.{ exported }
let f = pure (n: i64) -> i64 => { let unimported = n * 2 ; unimported }
let main = () -> void => println("${f(exported(1))}")
`,
	})
	if len(got) != 0 {
		t.Errorf("got %v; want none — `unimported` is not in scope in this file", got)
	}
}

// The decisive case: another module's **private** name is unreachable under any import,
// so declaring one of your own can shadow nothing. `SymbolTable.ModuleExports` is the
// predicate, and its own comment already stated this rule for a different check.
func TestShadowing_APrivateNameOfAnotherModuleShadowsNothing(t *testing.T) {
	got := shadowWarnings(t, map[string]string{
		"lib.lyra": shadowLib,
		"main.lyra": `module main
import lib.{ exported }
let f = pure (n: i64) -> i64 => { let private_helper = n * 2 ; private_helper }
let main = () -> void => println("${f(exported(1))}")
`,
	})
	if len(got) != 0 {
		t.Errorf("got %v; want none — `private_helper` has no `pub` and no import reaches it", got)
	}
}

// The other direction, and the one that says the check still works: a name the file
// **did** import is in scope, so a local of that name shadows it.
func TestShadowing_AnImportedNameIsShadowed(t *testing.T) {
	got := shadowWarnings(t, map[string]string{
		"lib.lyra": shadowLib,
		"main.lyra": `module main
import lib.{ exported }
let f = pure (n: i64) -> i64 => { let exported = n * 2 ; exported }
let main = () -> void => println("${f(1)}")
`,
	})
	if len(got) != 1 || !strings.Contains(got[0], "exported") {
		t.Errorf("got %v; want one warning naming `exported`", got)
	}
}

// A **namespace** import binds `lib.exported`, not `exported`, so no bare name comes into
// scope through one — the same asymmetry the import-boundary rule draws everywhere else.
func TestShadowing_ANamespaceImportBindsNoBareName(t *testing.T) {
	got := shadowWarnings(t, map[string]string{
		"lib.lyra": shadowLib,
		"main.lyra": `module main
import lib
let f = pure (n: i64) -> i64 => { let exported = n * 2 ; exported }
let main = () -> void => println("${f(lib.exported(1))}")
`,
	})
	if len(got) != 0 {
		t.Errorf("got %v; want none — a namespace import binds `lib.exported`, not `exported`", got)
	}
}

// An alias renames what it binds, so the **local** name is the one that can be shadowed.
func TestShadowing_AnAliasedImportIsShadowedUnderItsLocalName(t *testing.T) {
	got := shadowWarnings(t, map[string]string{
		"lib.lyra": shadowLib,
		"main.lyra": `module main
import lib.{ exported as renamed }
let f = pure (n: i64) -> i64 => { let renamed = n * 2 ; renamed }
let g = pure (n: i64) -> i64 => { let exported = n * 2 ; exported }
let main = () -> void => println("${f(1)} ${g(1)} ${renamed(1)}")
`,
	})
	if len(got) != 1 || !strings.Contains(got[0], "renamed") {
		t.Errorf("got %v; want exactly one, naming `renamed` — `exported` is not bound here", got)
	}
}

// A file's **own module** is in scope whole, across its files, and without an import.
func TestShadowing_ASiblingFileOfTheSameModuleIsInScope(t *testing.T) {
	got := shadowWarnings(t, map[string]string{
		"main.lyra": `module main
import lib.{ exported }
let main = () -> void => println("${exported(1)}")
`,
		"lib/a.lyra": `module lib
pub let exported = pure (n: i64) -> i64 => { let neighbour = n * 2 ; neighbour }
`,
		"lib/b.lyra": `module lib
pub let neighbour = pure (n: i64) -> i64 => n
`,
	})
	if len(got) != 1 || !strings.Contains(got[0], "neighbour") {
		t.Errorf("got %v; want one warning naming `neighbour`", got)
	}
}

// **Order is not part of the question.** Lyra has no forward-declaration constraint, so a
// top-level name is in scope throughout its module — including above its own declaration.
// The previous walk accumulated names as it passed them, so this case went unreported.
func TestShadowing_ATopLevelNameDeclaredLaterIsStillShadowed(t *testing.T) {
	got := shadowWarnings(t, map[string]string{
		"main.lyra": `module main
let f = pure (n: i64) -> i64 => { let later = n * 2 ; later }
let later = pure (n: i64) -> i64 => n
let main = () -> void => println("${f(1)} ${later(1)}")
`,
	})
	if len(got) != 1 || !strings.Contains(got[0], "later") {
		t.Errorf("got %v; want one warning naming `later`", got)
	}
}

// **The prelude case has no test here, and that is the harness's limit rather than a gap
// in the rule.** `modules.Resolve` is called with no prelude option, so `PreludeModule` is
// empty and `unwrap_or` is not in the program at all — a test asserting it would pass for
// the wrong reason. The behaviour is kept by `forFile` adding the prelude's *exports*, and
// was checked against the real standard library: a local named `unwrap_or` still warns.
// CLAUDE.md makes the general point — a test that needs the prelude belongs where the
// prelude is real.
