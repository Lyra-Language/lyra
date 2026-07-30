package modules_test

import (
	"path/filepath"
	"testing"

	"github.com/Lyra-Language/lyra/pkg/ast/symbols"
	"github.com/Lyra-Language/lyra/pkg/driver"
	"github.com/Lyra-Language/lyra/pkg/modules"
)

// Each module's top-level declarations land in its own ScopeModule.
//
// The scopes are populated but inert: lookups still start from GlobalScope and every
// declaration is still registered there too, so this changes no behaviour. It is the
// structure per-module resolution needs, put in place separately so that the change to
// name *binding* — which has no safe half-applied state, since a reference resolving to
// the wrong declaration compiles clean and runs wrong — can be reviewed on its own.
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
	// The entry file declares no module, and its scope *is* the global one: a program
	// root has nothing to be private from, and giving it a child scope would push every
	// single-file program's declarations out of sight of anything starting from global.
	appScope := st.ModuleScopeFor("")
	if appScope != st.GlobalScope {
		t.Fatal("the entry module's scope should be the global scope")
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
		{appScope, "double", true, "util.math exports it, so it is globally visible"},
		{appScope, "mathOnly", false, "private to util.math — the point of the split"},
	} {
		if _, found := c.scope.LookupLocal(c.name); found != c.want {
			t.Errorf("%s in scope: got %v, want %v (%s)", c.name, found, c.want, c.why)
		}
	}

	// Siblings under the global scope, not nested: modules do not contain one another.
	if mathScope.Parent != st.GlobalScope {
		t.Error("a named module's scope should be a child of the global scope")
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
