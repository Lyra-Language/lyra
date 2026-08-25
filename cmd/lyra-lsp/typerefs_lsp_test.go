package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/owenrumney/go-lsp/lsp"
	"github.com/owenrumney/go-lsp/servertest"
)

// References and rename on a **type**, which the expression walk cannot reach — the same
// gap go-to-definition had, one feature over. These two share a file because they must
// agree with each other: if rename misses a use that references reports, the rename leaves
// a broken program behind and the editor calls it a success.

// openFileAndWait writes source to disk *and* opens it, which the single-file harness does
// not: module resolution reads siblings from the filesystem, so a test about reaching
// another module needs both halves.
func openFileAndWait(t *testing.T, h *servertest.Harness, dir, name, source string) lsp.DocumentURI {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	uri := lsp.DocumentURI(pathToURI(path))
	if err := h.DidOpen(uri, "lyra", source); err != nil {
		t.Fatalf("DidOpen: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := h.WaitForDiagnostics(ctx, uri); err != nil {
		t.Fatalf("WaitForDiagnostics: %v", err)
	}
	return uri
}

// colOf is the 0-based character offset of needle on a 0-based line of src.
func colOf(t *testing.T, src, needle string, line0 int) int {
	t.Helper()
	col := strings.Index(strings.Split(src, "\n")[line0], needle)
	if col < 0 {
		t.Fatalf("%q not on line %d", needle, line0)
	}
	return col
}

// Every written use of a type: signatures, fields, annotations, generic arguments — and
// the struct **literal**, which is an expression rather than a written type and so comes
// from a different pass. Omitting it would make the answer look complete while missing the
// spelling a reader is most likely hunting for.
func TestReferences_TypeNameFindsEveryWrittenUse(t *testing.T) {
	t.Setenv("LYRA_STD", stdRootDir(t))
	h := servertest.New(t, newHandler())
	src := `module main
struct Point { x: i64, y: i64 }
struct Line { a: Point, b: Point }
let mk = pure (n: i64) -> Point => Point { x: n, y: n }
let use = pure (p: Point, xs: []Point) -> i64 => p.x
let main = () -> void => { let p: Point = mk(1); println(p.x) }`
	uri := openFileAndWait(t, h, t.TempDir(), "app.lyra", src)

	locs, err := h.References(uri, 4, colOf(t, src, "Point,", 4), true)
	if err != nil {
		t.Fatalf("References: %v", err)
	}
	// The declaration, Line's two fields, mk's return type, mk's literal, use's two
	// parameters, main's annotation.
	if len(locs) != 8 {
		t.Errorf("got %d references, want 8: %v", len(locs), locs)
	}
	literal := colOf(t, src, "Point { x: n", 3)
	found := false
	for _, l := range locs {
		if int(l.Range.Start.Line) == 3 && int(l.Range.Start.Character) == literal {
			found = true
		}
	}
	if !found {
		t.Errorf("the struct literal `Point { … }` must count as a use; got %v", locs)
	}
}

// **Cross-file within a module.** The server resolves the open document's import graph
// downward — what it imports, plus its own module's sibling files — so the reachable
// cross-file case is a module split across files, which is what `std.prelude` and
// `std.tui` are. A type declared in one file and used in its sibling is renamed in both.
func TestReferencesAndRename_ReachAModuleSibling(t *testing.T) {
	t.Setenv("LYRA_STD", stdRootDir(t))
	h := servertest.New(t, newHandler())
	dir := t.TempDir()

	// A module across several files is a *directory* named for the module, which is what
	// std.prelude and std.tui are; two files with the same header in an unrelated
	// directory are not one module.
	modDir := filepath.Join(dir, "shapes")
	if err := os.MkdirAll(modDir, 0o755); err != nil {
		t.Fatal(err)
	}
	sibling := "module shapes\nlet area = pure (p: Point) -> i64 => p.x\n"
	if err := os.WriteFile(filepath.Join(modDir, "area.lyra"), []byte(sibling), 0o644); err != nil {
		t.Fatal(err)
	}
	src := "module shapes\nstruct Point { x: i64, y: i64 }\nlet here = pure (p: Point) -> i64 => p.x\n"
	uri := openFileAndWait(t, h, modDir, "point.lyra", src)

	locs, err := h.References(uri, 1, colOf(t, src, "Point {", 1), false)
	if err != nil {
		t.Fatalf("References: %v", err)
	}
	files := map[lsp.DocumentURI]bool{}
	for _, l := range locs {
		files[l.URI] = true
	}
	if len(files) != 2 {
		t.Errorf("references reached %d file(s), want 2: %v", len(files), locs)
	}

	we, err := h.Rename(uri, 1, colOf(t, src, "Point {", 1), "Coord")
	if err != nil {
		t.Fatalf("Rename: %v", err)
	}
	if we == nil {
		t.Fatal("renaming a private type declared here must not be declined")
	}
	if len(we.Changes) != 2 {
		t.Errorf("rename edited %d file(s), want 2 — a rename that stops at the file "+
			"boundary leaves the sibling naming a type that no longer exists: %v", len(we.Changes), we.Changes)
	}
}

// **An exported type renames across the modules that import it.** Resolution runs
// downward only, so an importer is never in the open document's unit set; the server finds
// them by searching the workspace instead (importers.go). Without that, this rename would
// edit the declaring module and leave `other` naming a type that no longer exists — and
// for a few hours on 08/22 the server refused the rename outright for exactly that reason.
func TestRename_AnExportedTypeReachesItsImporters(t *testing.T) {
	t.Setenv("LYRA_STD", stdRootDir(t))
	h := servertest.New(t, newHandler())
	dir := t.TempDir()

	other := "module other\nimport shapes.{ Point }\nlet area = pure (p: Point) -> i64 => p.x\n"
	if err := os.WriteFile(filepath.Join(dir, "other.lyra"), []byte(other), 0o644); err != nil {
		t.Fatal(err)
	}
	src := "module shapes\npub struct Point { x: i64, y: i64 }\nlet here = pure (p: Point) -> i64 => p.x\n"
	uri := openFileAndWait(t, h, dir, "shapes.lyra", src)

	locs, err := h.References(uri, 1, colOf(t, src, "Point {", 1), false)
	if err != nil {
		t.Fatalf("References: %v", err)
	}
	files := map[lsp.DocumentURI]bool{}
	for _, l := range locs {
		files[l.URI] = true
	}
	if len(files) != 2 {
		t.Errorf("references reached %d file(s), want 2 — an importer's use is the whole "+
			"point of searching upward: %v", len(files), locs)
	}

	we, err := h.Rename(uri, 1, colOf(t, src, "Point {", 1), "Coord")
	if err != nil {
		t.Fatalf("Rename: %v", err)
	}
	if we == nil {
		t.Fatal("renaming an exported type must no longer be declined")
	}
	if len(we.Changes) != 2 {
		t.Errorf("rename edited %d file(s), want 2: %v", len(we.Changes), we.Changes)
	}
}

// The declaration must still be in this document. Renaming a **prelude** type from a use
// site would edit the standard library out from under every other program, so it stays
// declined — the rule the cross-file check has always followed, unchanged by references
// now reaching other files.
func TestRename_APreludeTypeIsStillDeclined(t *testing.T) {
	t.Setenv("LYRA_STD", stdRootDir(t))
	h := servertest.New(t, newHandler())
	src := "module main\nlet f = pure (m: Maybe<i64>) -> i64 => 0\nlet main = () -> void => println(1)\n"
	uri := openFileAndWait(t, h, t.TempDir(), "app.lyra", src)

	we, err := h.Rename(uri, 1, colOf(t, src, "Maybe", 1), "Perhaps")
	if err != nil {
		t.Fatalf("Rename: %v", err)
	}
	if we != nil {
		t.Errorf("renaming a prelude type must be declined; got edits in %d file(s)", len(we.Changes))
	}
}

// **A private type does not trigger the workspace search.** It cannot be named outside its
// module, so it has no importers, and the walk is tens of milliseconds against the
// microseconds the rest of a lookup takes. Asserted through behaviour rather than timing:
// a file that imports nothing and would be scanned only by the walk must not appear.
func TestReferences_APrivateTypeSkipsTheWorkspaceSearch(t *testing.T) {
	t.Setenv("LYRA_STD", stdRootDir(t))
	h := servertest.New(t, newHandler())
	dir := t.TempDir()

	// A decoy declaring its own same-named type. If the search ran and matching were by
	// name rather than by declaration, its uses would leak into the answer.
	decoy := "module other\nstruct Point { z: i64 }\nlet f = pure (p: Point) -> i64 => p.z\n"
	if err := os.WriteFile(filepath.Join(dir, "other.lyra"), []byte(decoy), 0o644); err != nil {
		t.Fatal(err)
	}
	src := "module shapes\nstruct Point { x: i64 }\nlet here = pure (p: Point) -> i64 => p.x\n"
	uri := openFileAndWait(t, h, dir, "shapes.lyra", src)

	locs, err := h.References(uri, 1, colOf(t, src, "Point {", 1), true)
	if err != nil {
		t.Fatalf("References: %v", err)
	}
	for _, l := range locs {
		if strings.Contains(string(l.URI), "other.lyra") {
			t.Errorf("a private type's references must not include another module's "+
				"same-named type: %v", locs)
		}
	}
	if len(locs) != 2 {
		t.Errorf("got %d references, want 2 (the declaration and its one use): %v", len(locs), locs)
	}
}
