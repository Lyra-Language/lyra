package main

import (
	"os"
	"path/filepath"
	"testing"
)

// **`m.X` reaches a module's `const` and top-level `var`** (09/30), as `m.f()` reaches its
// functions — `pad.LEFT` is how a game reads a button. Run, not just checked: the backend
// resolves the binding from its declaration, so a local of the same name in the reading
// function must not capture it.
func TestRun_ANamespaceReachesAModulesBindings(t *testing.T) {
	root := repoRoot(t)
	t.Setenv("LYRA_STD", root)
	dir := t.TempDir()
	for name, text := range map[string]string{
		"util/settings.lyra": "module util.settings\n" +
			"pub const LIMIT: i64 = 30\n" +
			"pub const TABLE: [3]i64 = #[4, 5, 6]\n" +
			"pub var count: i64 = 2\n",
		"main.lyra": "import util.settings\n" +
			"let main = () -> u8 => {\n" +
			"  let count = 1000\n" + // a local sharing the name must not win
			"  u8(settings.LIMIT + settings.TABLE[2] + settings.count + count - 1000)\n" +
			"}\n",
	} {
		path := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if _, stderr, code := captureRun(t, "run", filepath.Join(dir, "main.lyra")); code != 38 {
		t.Errorf("expected 30 + 6 + 2 = 38; exit %d, stderr: %s", code, stderr)
	}
}
