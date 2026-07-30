package modules_test

import (
	"path/filepath"
	"testing"

	"github.com/Lyra-Language/lyra/pkg/ast/symbols"
	"github.com/Lyra-Language/lyra/pkg/driver"
	"github.com/Lyra-Language/lyra/pkg/modules"
)

// Each module's top-level declarations land in its own ScopeModule, and only a `pub`
// one also lands in the shared global scope — which is what makes a private name
// invisible outside its module while a cross-module reference still resolves.
func TestModuleScopes_PopulatedPerModule(t *testing.T) {
	root := buildTree(t, map[string]string{
		"app.lyra":       "import util.math\nlet appOnly = () -> i64 => 1\nlet main = () -> u8 => u8(math.double(1))",
		"util/math.lyra": "module util.math\npub let double = (n: i64) -> i64 => n * 2\nlet mathOnly = () -> i64 => 3",
	})
	units, diags := modules.Resolve(filepath.Join(root, "app.lyra"), []string{root}, modules.Options{})
	if len(diags) != 0 {
		t.Fatalf("resolve failed: %v", diags)
	}
	st := driver.AnalyzeUnits(units).SymbolTable

	mathScope, ok := st.ModuleScopes["util.math"]
	if !ok {
		t.Fatal(`no scope for "util.math"`)
	}
	// The entry file declares no module, but it gets a scope of its own like any other:
	// sharing the global scope put its declarations in the scope every other module
	// falls through to, so an entry-file name replaced a prelude one program-wide.
	appScope := st.EntryScope()
	if appScope == st.GlobalScope {
		t.Fatal("the entry module should have a scope of its own")
	}

	for _, c := range []struct {
		scope *symbols.Scope
		name  string
		want  bool
		why   string
	}{
		{mathScope, "double", true, "declared in util.math"},
		{mathScope, "mathOnly", true, "private, but still its module's own"},
		{mathScope, "appOnly", false, "belongs to the entry module"},
		{appScope, "appOnly", true, "declared in the entry file"},
		{appScope, "double", false, "exported, so it lives in the global scope, not here"},
		{appScope, "mathOnly", false, "private to util.math — the point of the split"},
	} {
		if _, found := c.scope.LookupLocal(c.name); found != c.want {
			t.Errorf("%s in scope: got %v, want %v (%s)", c.name, found, c.want, c.why)
		}
	}

	// An export is still reachable from another module — through the chain, not by
	// sitting in that module's own scope.
	if _, found := appScope.Lookup("double"); !found {
		t.Error("util.math exports double, so the entry module should resolve it")
	}
	if _, found := appScope.Lookup("mathOnly"); found {
		t.Error("a private name must not be reachable from another module")
	}

	// Siblings, not nested: modules do not contain one another. They hang off the
	// prelude scope, which is what puts the prelude's names between a module's own
	// declarations and the global ones.
	if mathScope.Parent != st.PreludeScope || appScope.Parent != st.PreludeScope {
		t.Error("a module's scope should be a child of the prelude scope")
	}
	if st.PreludeScope.Parent != st.GlobalScope {
		t.Error("the prelude scope should sit under the global scope")
	}
	if mathScope.Kind != symbols.ScopeModule {
		t.Errorf("expected ScopeModule, got %v", mathScope.Kind)
	}
}

// The mechanism the resolution change will rest on: two modules can each hold a
// declaration of the same name, because they are separate scopes. Exercised against the
// symbol table directly, since a program doing this is still rejected by the global
// registration that resolution has yet to be moved off.
func TestModuleScopes_SameNameInTwoModules(t *testing.T) {
	root := buildTree(t, map[string]string{
		"app.lyra": "import a\nlet main = () -> u8 => 0",
		"a.lyra":   "module a\npub let helper = () -> i64 => 1",
	})
	units, _ := modules.Resolve(filepath.Join(root, "app.lyra"), []string{root}, modules.Options{})
	st := driver.AnalyzeUnits(units).SymbolTable

	aScope := st.ModuleScopeFor("a")
	bScope := st.ModuleScopeFor("b")
	if aScope == bScope {
		t.Fatal("distinct modules must get distinct scopes")
	}
	if _, ok := aScope.LookupLocal("helper"); !ok {
		t.Error(`module "a" should hold its own helper`)
	}
	if _, ok := bScope.LookupLocal("helper"); ok {
		t.Error(`module "b" must not see module "a"'s helper`)
	}
}

// The capability the whole module system was building toward: two modules may each
// declare a *private* helper of the same name, and each calls its own.
//
// This is what "private" has to mean to be worth anything. Until resolution went
// per-module the second declaration was rejected as a duplicate of a name it had no
// business seeing — and the backend, keying emitted functions by bare name, kept only
// the last one and left the other module's calls pointing at a function that no longer
// existed.
func TestModuleScopes_SamePrivateNameInTwoModules(t *testing.T) {
	root := buildTree(t, map[string]string{
		"one.lyra": "module one\nlet helper = () -> i64 => 1\npub let fromOne = () -> i64 => helper()",
		"two.lyra": "module two\nlet helper = () -> i64 => 2\npub let fromTwo = () -> i64 => helper()",
		"app.lyra": "import one\nimport two\nlet main = () -> u8 => u8(fromOne() + fromTwo() * 10)",
	})
	res := analyze(t, root)
	if errs := res.Errors(); len(errs) != 0 {
		t.Fatalf("two modules should each be allowed their own private helper; got %v", errs)
	}
}

// Privacy is still enforced — the point is that each module gets its own name, not that
// the name became public.
func TestModuleScopes_PrivateStillUnreachable(t *testing.T) {
	root := buildTree(t, map[string]string{
		"one.lyra": "module one\nlet helper = () -> i64 => 1\npub let fromOne = () -> i64 => helper()",
		"app.lyra": "import one\nlet main = () -> u8 => u8(helper())",
	})
	res := analyze(t, root)
	if !errorsContaining(res, `helper is private to module "one"`) {
		t.Errorf("reaching into a module's private function should still be rejected; got %v", res.Errors())
	}
}

// A module calling its own private function must not be told it is private. The check
// used to ask whether *some* declaration of that name was private — via a name-keyed
// module map that is last-writer-wins — rather than whether the declaration the
// reference actually resolved to was visible, so one module's call to its own function
// reported the other module's privacy.
func TestModuleScopes_OwnPrivateCallIsNotReportedPrivate(t *testing.T) {
	root := buildTree(t, map[string]string{
		"one.lyra": "module one\npub let helper = () -> i64 => 1\npub let fromOne = () -> i64 => helper()",
		"two.lyra": "module two\nlet helper = () -> i64 => 2\npub let fromTwo = () -> i64 => 5",
		"app.lyra": "import one\nimport two\nlet main = () -> u8 => u8(fromOne() + fromTwo())",
	})
	res := analyze(t, root)
	if errs := res.Errors(); len(errs) != 0 {
		t.Errorf("a module calling its own function should be clean; got %v", errs)
	}
}
