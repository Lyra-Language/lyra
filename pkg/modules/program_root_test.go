package modules

import (
	"path/filepath"
	"testing"
)

// A file of a module in its own directory, analyzed alone (as an editor does), finds the
// program's other modules beneath the directory above its module's — `src`, for
// `src/ui/grid.lyra` declaring `module ui` and importing `vt`.
func TestResolve_AModuleFileFindsItsProgramsModules(t *testing.T) {
	root := write(t, map[string]string{
		"src/app.lyra":       "import ui\nlet main = () -> u8 => 0",
		"src/ui/grid.lyra":   "module ui\nimport vt.{ cell }\npub let draw = () -> i64 => cell()",
		"src/ui/other.lyra":  "module ui\npub let other = () -> i64 => 2",
		"src/vt/screen.lyra": "module vt\npub let cell = () -> i64 => 1",
	})
	entry := filepath.Join(root, "src/ui/grid.lyra")
	units, diags := Resolve(entry, []string{filepath.Dir(entry)}, Options{})
	if len(diags) != 0 {
		t.Fatalf("diagnostics: %v", diags)
	}
	got := map[string]int{}
	for _, u := range units {
		got[u.Path]++
	}
	if got["vt"] != 1 || got["ui"] != 2 {
		t.Fatalf("units = %v, want vt's file and ui's two", paths(units))
	}
}

// A single-file module named by its path finds its program's modules too.
func TestResolve_AFileModuleFindsItsProgramsModules(t *testing.T) {
	root := write(t, map[string]string{
		"src/net/http.lyra": "module net.http\nimport util\npub let get = () -> i64 => util.one()",
		"src/util.lyra":     "module util\npub let one = () -> i64 => 1",
	})
	entry := filepath.Join(root, "src/net/http.lyra")
	if _, diags := Resolve(entry, []string{filepath.Dir(entry)}, Options{}); len(diags) != 0 {
		t.Fatalf("diagnostics: %v", diags)
	}
}

// A file whose place does not match its module path is a module with neighbours, as
// before: no root is implied, and an import of a module it cannot see is still reported.
func TestResolve_NoRootForAMisplacedModule(t *testing.T) {
	root := write(t, map[string]string{
		"src/misc/grid.lyra": "module ui\nimport vt\npub let x = () -> i64 => 1",
		"vt.lyra":            "module vt\npub let y = () -> i64 => 1",
	})
	entry := filepath.Join(root, "src/misc/grid.lyra")
	if _, diags := Resolve(entry, []string{filepath.Dir(entry)}, Options{}); len(diags) == 0 {
		t.Fatalf("a misplaced module found vt through an implied root")
	}
}
