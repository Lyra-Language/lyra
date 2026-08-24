package modules_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Lyra-Language/lyra/pkg/modules"
)

// The cache must never serve a tree for bytes that are no longer the file's.
//
// It exists because a language server re-resolves a document's whole import graph on every
// keystroke and so re-parses every unit in it — 12 files for a small program with the
// standard prelude, of which 11 cannot have changed. Resolve dropped from 8.4 ms to 1.7 ms
// with it, about a third of the per-keystroke budget.
//
// A performance cache that can go stale is worse than no cache, so the key is the file's
// **contents**: the bytes are read either way and only the parse is skipped, which makes a
// stale hit unreachable rather than unlikely. These tests are about that property, not about
// the speed.

// The case the key is chosen for: the same path, different bytes.
func TestParseCache_EditedFileIsReparsed(t *testing.T) {
	dir := t.TempDir()
	app := filepath.Join(dir, "main.lyra")
	write := func(src string) {
		if err := os.WriteFile(app, []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cache := modules.NewParseCache()
	opts := modules.Options{ParseCache: cache}

	// The versions differ in their **number of declarations**, so the check below reads the
	// tree's shape rather than the text beside it. That distinction is the test: Source
	// always comes from a fresh read, and Root.Utf8Text slices *that* source by byte range,
	// so a text comparison passes with a completely stale tree. An earlier version of this
	// test did exactly that and passed against a deliberately path-keyed cache.
	write("module main\nlet first = () -> i64 => 1\n")
	units, _ := modules.Resolve(app, []string{dir}, opts)
	if len(units) != 1 {
		t.Fatalf("first resolve produced %d units", len(units))
	}
	if got := units[0].Root.NamedChildCount(); got != 2 {
		t.Fatalf("first tree has %d named children; want 2 (module + one let)", got)
	}

	// Same path, new contents. A path- or mtime-keyed cache is what gets this wrong.
	write("module main\nlet second = () -> i64 => 2\nlet third = () -> i64 => 3\n")
	units, _ = modules.Resolve(app, []string{dir}, opts)
	if len(units) != 1 {
		t.Fatalf("second resolve produced %d units", len(units))
	}
	if !strings.Contains(string(units[0].Source), "second") {
		t.Error("resolve returned the old source")
	}
	if got := units[0].Root.NamedChildCount(); got != 3 {
		t.Errorf("the cached tree is stale: %d named children, want 3 (module + two lets)", got)
	}
}

// An overlay is the editor's unsaved buffer, and it changes on every keystroke while the
// file on disk does not — the exact case the cache must not confuse.
func TestParseCache_OverlayChangeIsReparsed(t *testing.T) {
	dir := t.TempDir()
	app := filepath.Join(dir, "main.lyra")
	if err := os.WriteFile(app, []byte("module main\nlet saved = () -> i64 => 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cache := modules.NewParseCache()
	// Each step adds a declaration, so the tree's shape says whether it was re-parsed.
	for step, decls := range [][]string{{"a"}, {"a", "b"}, {"a", "b", "c"}} {
		src := "module main\n"
		for _, d := range decls {
			src += "let " + d + " = () -> i64 => 1\n"
		}
		units, _ := modules.Resolve(app, []string{dir}, modules.Options{
			ParseCache: cache,
			Overlay:    map[string][]byte{app: []byte(src)},
		})
		if len(units) != 1 {
			t.Fatalf("resolve produced %d units", len(units))
		}
		if got, want := int(units[0].Root.NamedChildCount()), len(decls)+1; got != want {
			t.Errorf("overlay step %d: tree has %d named children, want %d", step, got, want)
		}
	}
}

// A cached run must produce the same units as an uncached one — same count, same paths,
// same source. The cache is an optimization and may not change what resolution *means*.
func TestParseCache_AgreesWithNoCache(t *testing.T) {
	dir := t.TempDir()
	write := func(name, src string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("lib.lyra", "module lib\npub let helper = pure (n: i64) -> i64 => n * 2\n")
	write("main.lyra", "module main\nimport lib.{ helper }\nlet main = () -> void => println(helper(21))\n")
	app := filepath.Join(dir, "main.lyra")

	plain, _ := modules.Resolve(app, []string{dir}, modules.Options{})
	cache := modules.NewParseCache()
	opts := modules.Options{ParseCache: cache}
	modules.Resolve(app, []string{dir}, opts) // warm
	cached, _ := modules.Resolve(app, []string{dir}, opts)

	if len(plain) != len(cached) {
		t.Fatalf("unit count differs: %d without the cache, %d with", len(plain), len(cached))
	}
	for i := range plain {
		if plain[i].Path != cached[i].Path || plain[i].File != cached[i].File {
			t.Errorf("unit %d differs: %q/%q vs %q/%q",
				i, plain[i].Path, plain[i].File, cached[i].Path, cached[i].File)
		}
		if string(plain[i].Source) != string(cached[i].Source) {
			t.Errorf("unit %d source differs", i)
		}
	}
}

// A nil cache is the default and must simply mean "no reuse" — that is what a one-shot
// `lyrac` invocation passes, and it must not become a nil dereference.
func TestParseCache_NilIsDisabledNotACrash(t *testing.T) {
	dir := t.TempDir()
	app := filepath.Join(dir, "main.lyra")
	if err := os.WriteFile(app, []byte("module main\nlet main = () -> void => println(1)\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	units, _ := modules.Resolve(app, []string{dir}, modules.Options{ParseCache: nil})
	if len(units) != 1 {
		t.Fatalf("resolve with a nil cache produced %d units", len(units))
	}
}
