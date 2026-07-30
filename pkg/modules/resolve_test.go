package modules

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// write lays out a module tree under a temp dir and returns its root.
func write(t *testing.T, files map[string]string) string {
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

func paths(units []Unit) []string {
	out := make([]string, len(units))
	for i, u := range units {
		out[i] = u.Path
	}
	return out
}

// Imports resolve transitively, and each module is emitted before the ones that
// import it — so a dependency's own diagnostics precede its dependents'.
func TestResolve_DependencyOrder(t *testing.T) {
	root := write(t, map[string]string{
		"app.lyra":       "import util.math\nlet main = () -> u8 => 0",
		"util/math.lyra": "module util.math\nimport util.core\npub let double = (n: i64) -> i64 => n * 2",
		"util/core.lyra": "module util.core\npub let one = () -> i64 => 1",
	})
	units, diags := Resolve(filepath.Join(root, "app.lyra"), []string{root}, Options{})
	if len(diags) != 0 {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
	got := paths(units)
	want := []string{"util.core", "util.math", ""}
	if len(got) != len(want) {
		t.Fatalf("got %v; want %v", got, want)
	}
	for i := range want {
		// The entry file declares no module, so its path is empty.
		if want[i] != "" && got[i] != want[i] {
			t.Errorf("position %d: got %q; want %q (dependencies must come first)", i, got[i], want[i])
		}
	}
}

// A module imported from two places is emitted once, not twice — otherwise its
// declarations would be collected twice and collide with themselves.
func TestResolve_SharedDependencyEmittedOnce(t *testing.T) {
	root := write(t, map[string]string{
		"app.lyra":    "import a\nimport b\nlet main = () -> u8 => 0",
		"a.lyra":      "module a\nimport shared\npub let fa = () -> i64 => 1",
		"b.lyra":      "module b\nimport shared\npub let fb = () -> i64 => 2",
		"shared.lyra": "module shared\npub let s = () -> i64 => 3",
	})
	units, diags := Resolve(filepath.Join(root, "app.lyra"), []string{root}, Options{})
	if len(diags) != 0 {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
	count := 0
	for _, p := range paths(units) {
		if p == "shared" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("the shared module appears %d times; want 1", count)
	}
}

// A cycle is reported rather than followed — with no lazy-initialization semantics
// defined, neither half can sensibly observe the other.
func TestResolve_CycleReported(t *testing.T) {
	root := write(t, map[string]string{
		"app.lyra": "import a\nlet main = () -> u8 => 0",
		"a.lyra":   "module a\nimport b\npub let fa = () -> i64 => 1",
		"b.lyra":   "module b\nimport a\npub let fb = () -> i64 => 2",
	})
	_, diags := Resolve(filepath.Join(root, "app.lyra"), []string{root}, Options{})
	if len(diags) == 0 {
		t.Fatal("expected a cycle diagnostic")
	}
	if !strings.Contains(diags[0].Message, "import cycle") {
		t.Errorf("expected an import-cycle diagnostic, got %q", diags[0].Message)
	}
}

// A missing module names the paths that were tried, since the mapping is by
// convention and the likely mistake is a misplaced file.
func TestResolve_MissingModuleListsCandidates(t *testing.T) {
	root := write(t, map[string]string{
		"app.lyra": "import util.nope\nlet main = () -> u8 => 0",
	})
	_, diags := Resolve(filepath.Join(root, "app.lyra"), []string{root}, Options{})
	if len(diags) == 0 {
		t.Fatal("expected an unresolved-import diagnostic")
	}
	msg := diags[0].Message
	if !strings.Contains(msg, "util.nope") || !strings.Contains(msg, "nope.lyra") {
		t.Errorf("expected the message to name the module and the path tried, got %q", msg)
	}
}

// Roots are searched in order, so a program's own files are found before the
// standard library.
func TestResolve_RootOrder(t *testing.T) {
	projectRoot := write(t, map[string]string{
		"app.lyra":  "import util\nlet main = () -> u8 => 0",
		"util.lyra": "module util\npub let local = () -> i64 => 1",
	})
	stdRoot := write(t, map[string]string{
		"util.lyra": "module util\npub let fromStd = () -> i64 => 2",
	})
	units, diags := Resolve(filepath.Join(projectRoot, "app.lyra"), []string{projectRoot, stdRoot}, Options{})
	if len(diags) != 0 {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
	for _, u := range units {
		if u.Path == "util" && !strings.HasPrefix(u.File, projectRoot) {
			t.Errorf("resolved util from %q; the first root should win", u.File)
		}
	}
}
