package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/owenrumney/go-lsp/servertest"
)

// Formatting runs the real lyrafmt, so the test builds one: the grammar archive from
// tree-sitter-lyra, then the Lyra program with `lyrac`. It skips without a C compiler, the
// tree-sitter runtime or a sibling grammar checkout, since the question is about the
// linker — the same gate cmd/lyrac's lyrafmt test uses.
func buildLyrafmt(t *testing.T) string {
	t.Helper()
	clang, err := exec.LookPath("clang")
	if err != nil {
		t.Skip("clang not found on PATH")
	}
	libdir, err := exec.Command("pkg-config", "--variable=libdir", "tree-sitter").Output()
	if err != nil {
		t.Skip("the tree-sitter runtime is not installed; brew install tree-sitter")
	}
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate this test file")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(thisFile), "..", ".."))
	grammar := filepath.Join(root, "..", "tree-sitter-lyra", "src")
	if _, err := os.Stat(filepath.Join(grammar, "parser.c")); err != nil {
		t.Skip("tree-sitter-lyra is not checked out beside lyra")
	}
	dir := t.TempDir()
	for _, src := range []string{"parser.c", "scanner.c"} {
		obj := filepath.Join(dir, strings.TrimSuffix(src, ".c")+".o")
		if out, err := exec.Command(clang, "-O1", "-std=c11", "-c", filepath.Join(grammar, src), "-I", grammar, "-o", obj).CombinedOutput(); err != nil {
			t.Fatalf("compiling %s: %v\n%s", src, err, out)
		}
	}
	if out, err := exec.Command("ar", "rcs", filepath.Join(dir, "libtree-sitter-lyra.a"),
		filepath.Join(dir, "parser.o"), filepath.Join(dir, "scanner.o")).CombinedOutput(); err != nil {
		t.Fatalf("archiving the grammar: %v\n%s", err, out)
	}
	bin := filepath.Join(dir, "lyrafmt")
	build := exec.Command("go", "run", "./cmd/lyrac", "build", "-o", bin,
		filepath.Join(root, "examples", "lyrafmt", "lyrafmt.lyra"))
	build.Dir = root
	build.Env = append(os.Environ(),
		"LYRA_STD="+root,
		"LIBRARY_PATH="+dir+string(os.PathListSeparator)+strings.TrimSpace(string(libdir)))
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("building lyrafmt: %v\n%s", err, out)
	}
	return bin
}

// A buffer the rules have something to say about comes back as one edit covering the whole
// document, with the formatted text.
func TestFormatting_RunsLyrafmt(t *testing.T) {
	t.Setenv("LYRA_FMT", buildLyrafmt(t))
	resetLyrafmtLookup()

	h := servertest.New(t, newHandler())
	src := "let f = (a: i64,b :i64) -> i64=>{\n  let xs = [1 ,2]\n      a + xs.len()\n}\n"
	openAndWait(t, h, src)

	edits, err := h.Formatting(testURI)
	if err != nil {
		t.Fatalf("Formatting: %v", err)
	}
	if len(edits) != 1 {
		t.Fatalf("expected one whole-document edit, got %d: %v", len(edits), edits)
	}
	const want = "let f = (a: i64, b: i64) -> i64 => {\n  let xs = [1, 2]\n  a + xs.len()\n}\n"
	if edits[0].NewText != want {
		t.Errorf("formatted:\n%q\nwant:\n%q", edits[0].NewText, want)
	}
	// The range must cover the buffer, or applying the edit leaves a tail behind.
	if edits[0].Range.Start.Line != 0 || edits[0].Range.Start.Character != 0 {
		t.Errorf("edit does not start at the top: %v", edits[0].Range)
	}
	lines := strings.Split(src, "\n")
	if edits[0].Range.End.Line != len(lines)-1 || edits[0].Range.End.Character != len(lines[len(lines)-1]) {
		t.Errorf("edit does not reach the end: %v", edits[0].Range)
	}
}

// The second rung of the ladder: no `$LYRA_FMT`, but `lyrafmt` on `$PATH`.
func TestFormatting_FindsLyrafmtOnPath(t *testing.T) {
	bin := buildLyrafmt(t)
	t.Setenv("LYRA_FMT", "")
	t.Setenv("PATH", filepath.Dir(bin))
	resetLyrafmtLookup()

	h := servertest.New(t, newHandler())
	openAndWait(t, h, "let f = (a: i64,b: i64) -> i64 => a\n")
	edits, err := h.Formatting(testURI)
	if err != nil {
		t.Fatalf("Formatting: %v", err)
	}
	if len(edits) != 1 || edits[0].NewText != "let f = (a: i64, b: i64) -> i64 => a\n" {
		t.Errorf("expected the formatted text from PATH, got %v", edits)
	}
}

// A buffer already formatted, and one the parser refuses, both answer with no edits —
// never an error, and never a partial rewrite.
func TestFormatting_DeclinesQuietly(t *testing.T) {
	t.Setenv("LYRA_FMT", buildLyrafmt(t))

	for name, src := range map[string]string{
		"already formatted": "let f = (a: i64) -> i64 => a\n",
		"a syntax error":    "let f = (a: i64) -> i64 => {\n",
	} {
		t.Run(name, func(t *testing.T) {
			resetLyrafmtLookup()
			h := servertest.New(t, newHandler())
			openAndWait(t, h, src)
			edits, err := h.Formatting(testURI)
			if err != nil {
				t.Fatalf("Formatting: %v", err)
			}
			if len(edits) != 0 {
				t.Errorf("expected no edits, got %v", edits)
			}
		})
	}
}

// With no lyrafmt anywhere the request is answered with no edits rather than an error: an
// editor without the formatter built simply has no formatting.
func TestFormatting_WithoutLyrafmt(t *testing.T) {
	t.Setenv("LYRA_FMT", filepath.Join(t.TempDir(), "absent"))
	t.Setenv("PATH", t.TempDir())
	resetLyrafmtLookup()

	h := servertest.New(t, newHandler())
	openAndWait(t, h, "let f = (a: i64,b :i64) -> i64 => a\n")
	edits, err := h.Formatting(testURI)
	if err != nil {
		t.Fatalf("Formatting: %v", err)
	}
	if len(edits) != 0 {
		t.Errorf("expected no edits without lyrafmt, got %v", edits)
	}
}
