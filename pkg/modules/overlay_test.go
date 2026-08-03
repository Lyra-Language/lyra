package modules

import (
	"path/filepath"
	"strings"
	"testing"
)

// An overlay is what lets a language server analyze the buffer rather than the file.
// These tests pin the two properties that makes it depend on: the buffer wins over what
// is saved, and a file that exists *only* in the buffer still resolves.

// The overlay's source is used in place of the file's own.
func TestResolve_OverlayWinsOverDisk(t *testing.T) {
	root := write(t, map[string]string{
		"app.lyra": "let main = () -> u8 => 0",
	})
	entry := filepath.Join(root, "app.lyra")
	const edited = "let main = () -> u8 => 1 // unsaved\n"

	units, diags := Resolve(entry, []string{root}, Options{
		Overlay: map[string][]byte{entry: []byte(edited)},
	})
	if len(diags) != 0 {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
	if len(units) != 1 {
		t.Fatalf("got %d units; want 1", len(units))
	}
	if got := string(units[0].Source); got != edited {
		t.Errorf("resolved the saved file, not the buffer:\n got %q\nwant %q", got, edited)
	}
}

// A module that has never been saved is still importable: existence is answered by the
// overlay as well as by the disk, or resolution would report a file the editor can
// plainly see as missing.
func TestResolve_OverlayFileNeedNotExistOnDisk(t *testing.T) {
	root := write(t, map[string]string{
		"app.lyra": "import util.math\nlet main = () -> u8 => 0",
	})
	entry := filepath.Join(root, "app.lyra")
	unsaved := filepath.Join(root, "util", "math.lyra")

	units, diags := Resolve(entry, []string{root}, Options{
		Overlay: map[string][]byte{
			unsaved: []byte("module util.math\npub let double = (n: i64) -> i64 => n * 2"),
		},
	})
	if len(diags) != 0 {
		t.Fatalf("an overlaid module must resolve; got %v", diags)
	}
	if got := paths(units); len(got) != 2 || got[0] != "util.math" {
		t.Fatalf("got units %v; want [util.math <entry>]", got)
	}
}

// Keys are normalized, so a caller passing an unclean path (a `./` prefix, a doubled
// separator) still matches the path the resolver constructs from its roots.
func TestResolve_OverlayKeysAreCleaned(t *testing.T) {
	root := write(t, map[string]string{
		"app.lyra": "let main = () -> u8 => 0",
	})
	entry := filepath.Join(root, "app.lyra")
	const edited = "let main = () -> u8 => 2"

	units, _ := Resolve(entry, []string{root}, Options{
		Overlay: map[string][]byte{filepath.Join(root, ".", "app.lyra"): []byte(edited)},
	})
	if len(units) != 1 || string(units[0].Source) != edited {
		t.Errorf("an unclean overlay key must still match; got %q", units[0].Source)
	}
}

// The prelude search consults the overlay too — an editor's own copy of the standard
// library is the one a compile should see.
func TestResolve_OverlayPrelude(t *testing.T) {
	root := write(t, map[string]string{
		"app.lyra": "let main = () -> u8 => 0",
	})
	entry := filepath.Join(root, "app.lyra")
	prelude := filepath.Join(root, "std", "prelude.lyra")

	units, diags := Resolve(entry, []string{root}, Options{
		Prelude: PreludeModule,
		Overlay: map[string][]byte{
			prelude: []byte("module std.prelude\npub data Maybe<t> = None | Some t"),
		},
	})
	if len(diags) != 0 {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
	if len(units) != 2 || units[0].Path != PreludeModule {
		t.Fatalf("got units %v; want the overlaid prelude first", paths(units))
	}
	if !strings.Contains(string(units[0].Source), "Maybe") {
		t.Errorf("prelude unit does not hold the overlaid source: %q", units[0].Source)
	}
}
