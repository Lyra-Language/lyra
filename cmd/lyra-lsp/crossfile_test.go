package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/owenrumney/go-lsp/lsp"
	"github.com/owenrumney/go-lsp/servertest"
)

// References and rename on a **binding** reach every file of the program (09/13). Only types
// did before, through the collector's index; a function's uses were searched for in the open
// document alone, so a rename edited one file and left the rest calling a name that no longer
// existed, and a rename started from a use of another file's function declined silently.

func writeFile(t *testing.T, dir, name, source string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, name)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
}

// editsIn returns the new text's ranges per file basename, so an assertion can name files.
func editsIn(we *lsp.WorkspaceEdit) map[string][]lsp.TextEdit {
	out := map[string][]lsp.TextEdit{}
	for uri, edits := range we.Changes {
		out[filepath.Base(uriToPath(string(uri)))] = append(out[filepath.Base(uriToPath(string(uri)))], edits...)
	}
	return out
}

// A module across two files: a function declared in one and called from the other, renamed
// and searched for **from the use**, in the file that does not declare it.
func TestCrossFile_ASiblingModuleFunction(t *testing.T) {
	t.Setenv("LYRA_STD", stdRootDir(t))
	h := servertest.New(t, newHandler())
	dir := t.TempDir()
	writeFile(t, dir, "shapes/util.lyra", "module shapes\nlet double = pure (n: i64) -> i64 => n * 2\n")
	src := "module shapes\nlet quad = pure (n: i64) -> i64 => double(double(n))\n"
	uri := openFileAndWait(t, h, filepath.Join(dir, "shapes"), "quad.lyra", src)

	locs, err := h.References(uri, 1, colOf(t, src, "double(n)", 1), true)
	if err != nil {
		t.Fatalf("References: %v", err)
	}
	if len(locs) != 3 {
		t.Errorf("want the declaration and both calls (3), got %d: %v", len(locs), locs)
	}

	prep, err := h.PrepareRename(uri, 1, colOf(t, src, "double(n)", 1))
	if err != nil || prep == nil {
		t.Fatalf("PrepareRename must not decline a declaration in a sibling file: %v %v", prep, err)
	}
	if prep.Range.Start.Line != 1 || prep.Range.Start.Character != colOf(t, src, "double(n)", 1) {
		t.Errorf("the placeholder must cover the use under the cursor in this file, got %+v", prep.Range)
	}

	we, err := h.Rename(uri, 1, colOf(t, src, "double(n)", 1), "twice")
	if err != nil || we == nil {
		t.Fatalf("Rename declined: %v", err)
	}
	got := editsIn(we)
	if len(got["util.lyra"]) != 1 || len(got["quad.lyra"]) != 2 {
		t.Errorf("want 1 edit in util.lyra and 2 in quad.lyra, got %v", got)
	}
}

// **An exported function reaches its importers in every spelling of a use**: a selective
// import's member list, a bare call, a call through a namespace and a method-style (UFCS)
// call. Any one left behind is a call to a function that no longer exists. A decoy module
// declaring its own `double` must be untouched.
func TestCrossFile_AnExportedFunctionAndItsImporters(t *testing.T) {
	t.Setenv("LYRA_STD", stdRootDir(t))
	h := servertest.New(t, newHandler())
	dir := t.TempDir()
	writeFile(t, dir, "picks.lyra", "module picks\nimport shapes.{ double }\nlet a = pure (n: i64) -> i64 => double(n) + n.double()\n")
	writeFile(t, dir, "names.lyra", "module names\nimport shapes\nlet b = pure (n: i64) -> i64 => shapes.double(n)\n")
	writeFile(t, dir, "decoy.lyra", "module decoy\nlet double = pure (n: i64) -> i64 => n + n\nlet c = pure (n: i64) -> i64 => double(n)\n")
	src := "module shapes\npub let double = pure (self: i64) -> i64 => self * 2\n"
	uri := openFileAndWait(t, h, dir, "shapes.lyra", src)

	we, err := h.Rename(uri, 1, colOf(t, src, "double", 1), "twice")
	if err != nil || we == nil {
		t.Fatalf("Rename declined: %v", err)
	}
	got := editsIn(we)
	if len(got["shapes.lyra"]) != 1 {
		t.Errorf("shapes.lyra: want the declaration, got %v", got["shapes.lyra"])
	}
	if len(got["picks.lyra"]) != 3 {
		t.Errorf("picks.lyra: want the import member, the call and the method call (3), got %v", got["picks.lyra"])
	}
	if len(got["names.lyra"]) != 1 {
		t.Errorf("names.lyra: want the namespace call, got %v", got["names.lyra"])
	}
	if len(got["decoy.lyra"]) != 0 {
		t.Errorf("decoy.lyra declares its own double and must not be edited, got %v", got["decoy.lyra"])
	}

	// The import member's edit must cover the name alone.
	picks, _ := os.ReadFile(filepath.Join(dir, "picks.lyra"))
	for _, e := range got["picks.lyra"] {
		if e.Range.Start.Line == 1 {
			line := strings.Split(string(picks), "\n")[1]
			if span := line[e.Range.Start.Character:e.Range.End.Character]; span != "double" {
				t.Errorf("import member edit covers %q, want \"double\"", span)
			}
		}
	}
}

// A standard-library function is still declined: every program depends on it and none of
// them is in the workspace. References to it still answer.
func TestCrossFile_APreludeFunctionIsDeclined(t *testing.T) {
	t.Setenv("LYRA_STD", stdRootDir(t))
	h := servertest.New(t, newHandler())
	src := "module main\nlet f = pure (a: i64) -> i64 => min(a, 3)\n"
	uri := openFileAndWait(t, h, t.TempDir(), "app.lyra", src)

	we, err := h.Rename(uri, 1, colOf(t, src, "min", 1), "smallest")
	if err != nil {
		t.Fatalf("Rename: %v", err)
	}
	if we != nil {
		t.Errorf("renaming a prelude function must be declined; got edits in %d file(s)", len(we.Changes))
	}
	locs, err := h.References(uri, 1, colOf(t, src, "min", 1), false)
	if err != nil || len(locs) == 0 {
		t.Errorf("references to a prelude function should still answer: %v %v", locs, err)
	}
}
