package modules

import (
	"path/filepath"
	"strings"
	"testing"
)

// A module may be a **directory** of files rather than one file: `util.math` is
// `util/math.lyra` or every `*.lyra` inside `util/math/`. Both forms produce the same
// module — one path, one namespace — so a module that outgrows a file splits without any
// of its declarations changing meaning.
//
// That is the point of the feature rather than a side effect of it. Receiver-keyed
// overloading and prelude shadowing are both keyed on the *module*, so a grown module
// split into several modules instead would silently change what its names mean; splitting
// within one module is what leaves them alone. See README.md.
func TestResolve_ModuleMayBeADirectory(t *testing.T) {
	root := write(t, map[string]string{
		"app.lyra":            "import util.math\nlet main = () -> u8 => 0",
		"util/math/add.lyra":  "module util.math\npub let add = (a: i64, b: i64) -> i64 => a + b",
		"util/math/mul.lyra":  "module util.math\npub let mul = (a: i64, b: i64) -> i64 => a * b",
		"util/math/note.txt":  "not source, and must not be read as any",
		"util/math/sub.lyrax": "not a .lyra file either",
	})
	units, diags := Resolve(filepath.Join(root, "app.lyra"), []string{root}, Options{})
	if len(diags) != 0 {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
	var files []string
	for _, u := range units {
		if u.Path == "util.math" {
			files = append(files, filepath.Base(u.File))
		}
	}
	// Name order, so a compile is reproducible: unit order feeds diagnostic order, and a
	// directory listing is not ordered on every filesystem.
	if got, want := strings.Join(files, ","), "add.lyra,mul.lyra"; got != want {
		t.Errorf("got module files %q; want %q", got, want)
	}
}

// Every file of a directory module must say which module it is in. Membership by
// location alone would be less to type, but a file's own text would then no longer say
// what namespace its declarations join — in a module where a name may be a receiver
// overload of one declared three files away.
func TestResolve_ModuleDirectoryFileNeedsTheHeader(t *testing.T) {
	cases := []struct {
		name, body, want string
	}{
		{"no header", "pub let one = () -> i64 => 1", "must begin with `module util.math`"},
		{"wrong header", "module util.other\npub let one = () -> i64 => 1", `it declares module "util.other"`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := write(t, map[string]string{
				"app.lyra":           "import util.math\nlet main = () -> u8 => 0",
				"util/math/ok.lyra":  "module util.math\npub let two = () -> i64 => 2",
				"util/math/bad.lyra": tc.body,
			})
			_, diags := Resolve(filepath.Join(root, "app.lyra"), []string{root}, Options{})
			if len(diags) == 0 {
				t.Fatal("a file in a module directory that does not declare that module must be reported")
			}
			if !strings.Contains(diags[0].Message, tc.want) {
				t.Errorf("got %q; want it to contain %q", diags[0].Message, tc.want)
			}
			// Reported against the offending file, not against the import that reached it:
			// the importer may be several modules away with nothing to fix.
			if filepath.Base(diags[0].File) != "bad.lyra" {
				t.Errorf("diagnostic filed against %s; want bad.lyra", diags[0].File)
			}
		})
	}
}

// A single-file module needs no header — its path is its location — but a header that
// contradicts its location is still an error. One of the two is wrong, and picking which
// is not the compiler's to do.
func TestResolve_SingleFileModuleHeaderMustAgree(t *testing.T) {
	t.Run("absent is fine", func(t *testing.T) {
		root := write(t, map[string]string{
			"app.lyra":       "import util.math\nlet main = () -> u8 => 0",
			"util/math.lyra": "pub let one = () -> i64 => 1",
		})
		if _, diags := Resolve(filepath.Join(root, "app.lyra"), []string{root}, Options{}); len(diags) != 0 {
			t.Errorf("a module file without a header must resolve by its location; got %v", diags)
		}
	})
	t.Run("contradictory is not", func(t *testing.T) {
		root := write(t, map[string]string{
			"app.lyra":       "import util.math\nlet main = () -> u8 => 0",
			"util/math.lyra": "module util.other\npub let one = () -> i64 => 1",
		})
		_, diags := Resolve(filepath.Join(root, "app.lyra"), []string{root}, Options{})
		if len(diags) == 0 {
			t.Fatal("a module file declaring a different module must be reported")
		}
		if !strings.Contains(diags[0].Message, `declares module "util.other"`) {
			t.Errorf("got %q; want it to name the declared path", diags[0].Message)
		}
	})
}

// A root offering both forms is an error rather than a silent preference. Which one won
// would decide what half the program's names mean, and a reader looking at
// `util/math/add.lyra` has no way to see that `util/math.lyra` beside it is the real
// module.
func TestResolve_BothFormsInOneRootIsAmbiguous(t *testing.T) {
	root := write(t, map[string]string{
		"app.lyra":           "import util.math\nlet main = () -> u8 => 0",
		"util/math.lyra":     "module util.math\npub let one = () -> i64 => 1",
		"util/math/add.lyra": "module util.math\npub let two = () -> i64 => 2",
	})
	_, diags := Resolve(filepath.Join(root, "app.lyra"), []string{root}, Options{})
	if len(diags) == 0 {
		t.Fatal("a module that is both a file and a directory must be reported")
	}
	if !strings.Contains(diags[0].Message, "is both") {
		t.Errorf("got %q; want it to say the module is both forms", diags[0].Message)
	}
}

// Across *different* roots there is no ambiguity: the earlier root wins, which is the
// ordinary shadowing every other lookup here does — a project's own module takes
// precedence over the standard library's, in either form.
func TestResolve_EarlierRootWinsAcrossForms(t *testing.T) {
	std := write(t, map[string]string{
		"util/math.lyra": "module util.math\npub let one = () -> i64 => 1",
	})
	app := write(t, map[string]string{
		"app.lyra":           "import util.math\nlet main = () -> u8 => 0",
		"util/math/add.lyra": "module util.math\npub let two = () -> i64 => 2",
	})
	units, diags := Resolve(filepath.Join(app, "app.lyra"), []string{app, std}, Options{})
	if len(diags) != 0 {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
	for _, u := range units {
		if u.Path == "util.math" && !strings.HasPrefix(u.File, app) {
			t.Errorf("resolved util.math to %s; want the earlier root's directory form", u.File)
		}
	}
}

// A subdirectory is the next module path down, not more of its parent. Recursing would
// swallow `util.math.big` into `util.math` and make two spellings of a name mean the same
// thing.
func TestResolve_SubdirectoryIsItsOwnModule(t *testing.T) {
	root := write(t, map[string]string{
		"app.lyra":                "import util.math\nlet main = () -> u8 => 0",
		"util/math/add.lyra":      "module util.math\npub let two = () -> i64 => 2",
		"util/math/big/huge.lyra": "module util.math.big\npub let three = () -> i64 => 3",
	})
	units, diags := Resolve(filepath.Join(root, "app.lyra"), []string{root}, Options{})
	if len(diags) != 0 {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
	for _, u := range units {
		if strings.Contains(u.File, "big") {
			t.Errorf("%s was pulled into util.math; a subdirectory is its own module", u.File)
		}
	}
}

// A module reached from two importers is still emitted once — with every file once,
// which is the multi-file form of the same rule. Collecting a file twice would make its
// declarations collide with themselves.
func TestResolve_DirectoryModuleEmittedOnce(t *testing.T) {
	root := write(t, map[string]string{
		"app.lyra":           "import util.a\nimport util.b\nlet main = () -> u8 => 0",
		"util/a.lyra":        "module util.a\nimport util.math\npub let x = () -> i64 => 1",
		"util/b.lyra":        "module util.b\nimport util.math\npub let y = () -> i64 => 2",
		"util/math/add.lyra": "module util.math\npub let add = (a: i64, b: i64) -> i64 => a + b",
		"util/math/mul.lyra": "module util.math\npub let mul = (a: i64, b: i64) -> i64 => a * b",
	})
	units, diags := Resolve(filepath.Join(root, "app.lyra"), []string{root}, Options{})
	if len(diags) != 0 {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
	seen := map[string]int{}
	for _, u := range units {
		seen[u.File]++
	}
	for file, n := range seen {
		if n != 1 {
			t.Errorf("%s appears %d times; every file must be emitted once", file, n)
		}
	}
}

// An import written in **any** file of a module binds for the module, so a dependency
// reached only from its second file still resolves. `SymbolTable.Imports` is keyed by
// module path, so following only the first file's imports would leave the module using
// names it never pulled in.
func TestResolve_ImportsOfEveryFileAreFollowed(t *testing.T) {
	root := write(t, map[string]string{
		"app.lyra":           "import util.math\nlet main = () -> u8 => 0",
		"util/math/add.lyra": "module util.math\npub let add = (a: i64, b: i64) -> i64 => a + b",
		"util/math/mul.lyra": "module util.math\nimport util.core\npub let mul = (a: i64, b: i64) -> i64 => a * b",
		"util/core.lyra":     "module util.core\npub let one = () -> i64 => 1",
	})
	units, diags := Resolve(filepath.Join(root, "app.lyra"), []string{root}, Options{})
	if len(diags) != 0 {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
	found := false
	for _, u := range units {
		if u.Path == "util.core" {
			found = true
		}
	}
	if !found {
		t.Error("util.core is imported by the module's second file and was not resolved")
	}
}

// Entering a compile *at one file* of a multi-file module brings its siblings.
//
// Without this, `lyrac check std/prelude/strings.lyra` would analyze a fragment of the
// prelude and report everything declared in a sibling as undefined — so "the prelude
// compiles standalone", the property that makes it an ordinary module, would hold only
// while it fitted in one file.
func TestResolve_EntryFileBringsItsModule(t *testing.T) {
	root := write(t, map[string]string{
		"util/math/add.lyra": "module util.math\npub let add = (a: i64, b: i64) -> i64 => a + b",
		"util/math/mul.lyra": "module util.math\npub let mul = (a: i64, b: i64) -> i64 => a * b",
	})
	units, diags := Resolve(filepath.Join(root, "util", "math", "add.lyra"), []string{root}, Options{})
	if len(diags) != 0 {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
	if len(units) != 2 {
		t.Fatalf("got %d units; want both files of the entry's own module", len(units))
	}
}

// The test above must not become "every file beside the entry comes too". A file
// declaring `module app.util` in a directory called `src` is a single-file module that
// happens to have neighbours, and its neighbours are not its business.
func TestResolve_EntryFileDoesNotGatherUnrelatedNeighbours(t *testing.T) {
	root := write(t, map[string]string{
		"src/util.lyra":  "module app.util\npub let one = () -> i64 => 1",
		"src/other.lyra": "module app.other\npub let two = () -> i64 => 2",
	})
	units, diags := Resolve(filepath.Join(root, "src", "util.lyra"), []string{root}, Options{})
	if len(diags) != 0 {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
	if len(units) != 1 {
		t.Fatalf("got %d units; a single-file module is just itself", len(units))
	}
}

// The overlay reaches into a module directory, so an editor's unsaved — or never-saved —
// file counts as a member of the module it declares. That is the same rule the overlay
// already applied to a single-file module, and the reason it exists: a language server's
// buffer is by definition not what is on disk.
func TestResolve_OverlayFileJoinsAModuleDirectory(t *testing.T) {
	root := write(t, map[string]string{
		"app.lyra":           "import util.math\nlet main = () -> u8 => 0",
		"util/math/add.lyra": "module util.math\npub let add = (a: i64, b: i64) -> i64 => a + b",
	})
	unsaved := filepath.Join(root, "util", "math", "mul.lyra")
	units, diags := Resolve(filepath.Join(root, "app.lyra"), []string{root}, Options{
		Overlay: map[string][]byte{
			unsaved: []byte("module util.math\npub let mul = (a: i64, b: i64) -> i64 => a * b"),
		},
	})
	if len(diags) != 0 {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
	for _, u := range units {
		if u.File == unsaved {
			return
		}
	}
	t.Errorf("the never-saved %s did not join its module; got %v", filepath.Base(unsaved), paths(units))
}

// A module directory holding no source is reported rather than silently treated as an
// empty module — an import that resolves to nothing would report every name it uses as
// undefined, which points at the wrong file.
func TestResolve_EmptyModuleDirectoryIsReported(t *testing.T) {
	root := write(t, map[string]string{
		"app.lyra":           "import util.math\nlet main = () -> u8 => 0",
		"util/math/note.txt": "no source here",
	})
	_, diags := Resolve(filepath.Join(root, "app.lyra"), []string{root}, Options{})
	if len(diags) == 0 {
		t.Fatal("a module directory with no .lyra files must be reported")
	}
	if !strings.Contains(diags[0].Message, "holds no .lyra files") {
		t.Errorf("got %q; want it to say the directory holds no source", diags[0].Message)
	}
}

// An unresolvable import names **both** forms it looked for, since either is a fix and
// the likely mistake is a misplaced file.
func TestResolve_MissingModuleNamesBothForms(t *testing.T) {
	root := write(t, map[string]string{
		"app.lyra": "import util.math\nlet main = () -> u8 => 0",
	})
	_, diags := Resolve(filepath.Join(root, "app.lyra"), []string{root}, Options{})
	if len(diags) == 0 {
		t.Fatal("expected an unresolved-import diagnostic")
	}
	for _, want := range []string{"math.lyra", "math" + string(filepath.Separator)} {
		if !strings.Contains(diags[0].Message, want) {
			t.Errorf("got %q; want it to mention %q", diags[0].Message, want)
		}
	}
}
