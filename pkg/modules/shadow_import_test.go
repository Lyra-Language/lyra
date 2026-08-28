package modules_test

import (
	"strings"
	"testing"

	diag "github.com/Lyra-Language/lyra/pkg/diagnostic"
	"github.com/Lyra-Language/lyra/pkg/driver"
)

// A module's own declaration of an imported name is allowed, wins locally, and warns.
//
// This was a hard error — `function "map" is already defined at .../util/seq.lyra` —
// which read as "the module you imported owns that name and your program may not have
// one". The prelude, whose names you never asked for, had always taken the soft path, so
// the explicit act was punished and the implicit one forgiven.
func TestModules_LocalDeclarationShadowsAnImportedName(t *testing.T) {
	root := buildTree(t, map[string]string{
		"app.lyra": `import util.seq
let map = (n: i64) -> i64 => n + 1
let main = () -> u8 => u8(map(1))`,
		"util/seq.lyra": "module util.seq\npub let map = (n: i64) -> i64 => n * 2",
	})
	res := analyze(t, root)
	if errs := res.Errors(); len(errs) != 0 {
		t.Fatalf("a local declaration over an imported name must be allowed; got %v", errs)
	}
	if !warnsWith(res, diag.CodeImportShadowed, `reach the imported one as `+"`seq.map`") {
		t.Errorf("expected a shadow warning naming the namespace; got %v", res.Diagnostics)
	}
}

// "Wins" is a claim about which declaration the call resolves to, so it is checked by
// giving the two incompatible types rather than by reading the symbol table: if the
// imported `map` were still what a bare `map` means, the annotation below would not
// type-check.
func TestModules_ShadowingDeclarationIsTheOneCalled(t *testing.T) {
	root := buildTree(t, map[string]string{
		"app.lyra": `import util.seq
let map = (n: i64) -> i64 => n + 1
let main = () -> u8 => {
  let local: i64 = map(1)
  u8(local)
}`,
		"util/seq.lyra": "module util.seq\npub let map = (n: i64) -> string => \"nope\"",
	})
	res := analyze(t, root)
	if errs := res.Errors(); len(errs) != 0 {
		t.Errorf("the local declaration should win a bare call; got %v", errs)
	}
}

// The other half of the bargain: the shadowed declaration is not withdrawn, it is
// reached through the namespace the import already binds. Without this the fix would
// only have traded a hard error for a silently unreachable module.
func TestModules_ShadowedImportStaysReachableThroughItsNamespace(t *testing.T) {
	root := buildTree(t, map[string]string{
		"app.lyra": `import util.seq
let map = (n: i64) -> i64 => n + 1
let main = () -> u8 => {
  let theirs: string = seq.map(1)
  let mine: i64 = map(1)
  u8(mine)
}`,
		"util/seq.lyra": "module util.seq\npub let map = (n: i64) -> string => \"theirs\"",
	})
	res := analyze(t, root)
	if errs := res.Errors(); len(errs) != 0 {
		t.Errorf("the shadowed import should stay reachable as seq.map; got %v", errs)
	}
}

// A module that declares nothing of its own is unaffected: the imported declaration
// keeps the bare key, which is what makes the shadow local to the module that made it.
func TestModules_ShadowIsConfinedToTheDeclaringModule(t *testing.T) {
	root := buildTree(t, map[string]string{
		"app.lyra": `import util.seq
import util.other
let map = (n: i64) -> i64 => n + 1
let main = () -> u8 => u8(other.use())`,
		"util/seq.lyra":   "module util.seq\npub let map = (n: i64) -> i64 => n * 2",
		"util/other.lyra": "module util.other\nimport util.seq.{ map }\npub let use = () -> u8 => u8(map(21))",
	})
	res := analyze(t, root)
	if errs := res.Errors(); len(errs) != 0 {
		t.Errorf("another module should still reach the imported map bare; got %v", errs)
	}
}

// Types take the same key, so they take the same rule.
func TestModules_LocalTypeShadowsAnImportedType(t *testing.T) {
	root := buildTree(t, map[string]string{
		"app.lyra": `import util.shapes
struct Point { label: string }
let main = () -> u8 => {
  let p: Point = Point { label: "mine" }
  u8(p.label.len())
}`,
		"util/shapes.lyra": "module util.shapes\npub struct Point { x: i64 }",
	})
	res := analyze(t, root)
	if errs := res.Errors(); len(errs) != 0 {
		t.Fatalf("a local type over an imported one must be allowed; got %v", errs)
	}
	if !warnsWith(res, diag.CodeImportShadowed, "Point shadows") {
		t.Errorf("expected a shadow warning for the type; got %v", res.Diagnostics)
	}
}

// A *private* declaration never reached the importer, so declaring one of your own
// shadows nothing and there is nothing to warn about.
func TestModules_PrivateImportedNameIsNotShadowed(t *testing.T) {
	root := buildTree(t, map[string]string{
		"app.lyra": `import util.seq
let helper = () -> i64 => 1
let main = () -> u8 => u8(seq.pub_fn() + helper())`,
		"util/seq.lyra": "module util.seq\nlet helper = () -> i64 => 2\npub let pub_fn = () -> i64 => helper()",
	})
	res := analyze(t, root)
	if errs := res.Errors(); len(errs) != 0 {
		t.Fatalf("expected a clean program; got %v", errs)
	}
	for _, d := range res.Diagnostics {
		if d.Code == diag.CodeImportShadowed {
			t.Errorf("a private declaration is not shadowed; got %v", d)
		}
	}
}

// The genuine cross-module duplicate stays an error. Two modules exporting one name,
// neither importing the other, is not something a shadowing rule can resolve: a bare
// reference from a third module could mean either, and neither has a local declaration
// that is obviously meant to win.
func TestModules_UnrelatedDuplicateExportsStillCollide(t *testing.T) {
	root := buildTree(t, map[string]string{
		"app.lyra": "import one\nimport two\nlet main = () -> u8 => u8(one.helper())",
		"one.lyra": "module one\npub let helper = () -> i64 => 1",
		"two.lyra": "module two\npub let helper = () -> i64 => 2",
	})
	res := analyze(t, root)
	if !errorsContaining(res, `function "helper" is already defined`) {
		t.Errorf("expected the cross-module duplicate to stay an error; got %v", res.Errors())
	}
}

// The shadow rule keys a declaration apart from the imported one; it does not withdraw
// the *export*. A module that re-exports a name it also imports is claiming the
// program-wide name a second time, which is the duplicate above wearing an import.
func TestModules_ReExportingAnImportedNameStillCollides(t *testing.T) {
	root := buildTree(t, map[string]string{
		"app.lyra":      "import util.wrap\nlet main = () -> u8 => u8(wrap.map(1))",
		"util/seq.lyra": "module util.seq\npub let map = (n: i64) -> i64 => n * 2",
		"util/wrap.lyra": `module util.wrap
import util.seq
pub let map = (n: i64) -> i64 => n + 1`,
	})
	res := analyze(t, root)
	if !errorsContaining(res, `function "map" is already defined`) {
		t.Errorf("expected a re-export of an imported name to be reported; got %v", res.Errors())
	}
}

// The import can sit in a *later* file of a multi-file module than the declaration that
// shadows what it brings in. A type is keyed as it is registered, mid-walk, so a graph
// assembled file by file would key this one before knowing the import existed — and the
// key computed at every later lookup, with the graph complete, would miss it.
func TestModules_ShadowWorksWhenTheImportIsInAnotherFileOfTheModule(t *testing.T) {
	root := buildTree(t, map[string]string{
		"app.lyra": "import app.lib\nlet main = () -> u8 => u8(lib.mine())",
		"app/lib/a.lyra": `module app.lib
struct Point { label: string }
pub let mine = () -> u8 => {
  let p: Point = Point { label: "x" }
  u8(p.label.len())
}`,
		"app/lib/b.lyra":   "module app.lib\nimport util.shapes",
		"util/shapes.lyra": "module util.shapes\npub struct Point { x: i64 }",
	})
	res := analyze(t, root)
	if errs := res.Errors(); len(errs) != 0 {
		t.Errorf("the shadow must not depend on which file carries the import; got %v", errs)
	}
}

// A file that shadows a **prelude** name and reaches into a module through its namespace
// is the same key rule arriving by the other route, and it was broken before an imported
// name could be shadowed at all: a prelude shadow has qualified the shadowing
// declaration's key since 07/30, and the namespace path's `pub` check looked the binding
// up by *bare name* through a last-writer-wins map — so it read the entry file's own
// declaration and reported the imported module's exported function as private to itself.
// Nothing did both at once, so nothing found it.
func TestModules_PreludeShadowDoesNotHideANamespaceMember(t *testing.T) {
	root := buildTree(t, map[string]string{
		"app.lyra": `import util.seq
let print = (n: i64) -> i64 => n * 10
let main = () -> u8 => u8(print(1) + seq.count(1))`,
		"util/seq.lyra": "module util.seq\npub let count = (n: i64) -> i64 => n * 2",
	})
	res := analyze(t, root)
	if errs := res.Errors(); len(errs) != 0 {
		t.Errorf("a prelude shadow must not hide a namespace member; got %v", errs)
	}
}

func warnsWith(res *driver.Result, code, substring string) bool {
	for _, d := range res.Diagnostics {
		if d.Code == code && strings.Contains(d.Message, substring) {
			return true
		}
	}
	return false
}
