package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/owenrumney/go-lsp/lsp"

	"github.com/Lyra-Language/lyra/pkg/ast"
	diag "github.com/Lyra-Language/lyra/pkg/diagnostic"
)

// The regression these cover: the server analyzed each buffer as a single unit with no
// prelude, so `Some`, `None`, `Maybe` and every other standard-library name was
// reported undefined in the editor — `undefined tuple type "Some"` — on files that
// `lyrac check` compiled cleanly.

// stdRootDir points LYRA_STD at the repo's own std/, so these exercise the shipped
// prelude rather than a fixture: a prelude that stopped resolving through the language
// server is exactly the failure being pinned.
func stdRootDir(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "std", "prelude.lyra")); err != nil {
		t.Skipf("no shipped prelude under %s: %v", root, err)
	}
	return root
}

// openDoc puts source in the handler's store under a path inside dir and returns the
// document's URI and filesystem path.
func openDoc(t *testing.T, h *Handler, dir, name, source string) (lsp.DocumentURI, string) {
	t.Helper()
	path := filepath.Join(dir, name)
	uri := lsp.DocumentURI(pathToURI(path))
	h.docStore[string(uri)] = source
	return uri, path
}

func errorMessages(diags []diag.Diagnostic) []string {
	var out []string
	for _, d := range diags {
		if d.Severity == diag.SeverityError {
			out = append(out, d.Message)
		}
	}
	return out
}

// A buffer using prelude names analyzes clean, because the server resolves the
// document's import graph — prelude included — instead of the buffer alone.
func TestAnalyzeDocument_PreludeNamesResolve(t *testing.T) {
	t.Setenv("LYRA_STD", stdRootDir(t))
	h := newHandler()
	uri, _ := openDoc(t, h, t.TempDir(), "app.lyra", `
let m: Maybe<i64> = Some 1
let n = unwrap_or(m, 0)
let main = () -> u8 => 0
`)

	res, file := h.analyzeDocument(uri, h.docStore[string(uri)])
	if file == "" {
		t.Fatal("a file:// document must analyze against its own path")
	}
	if msgs := errorMessages(diagnosticsFor(res.Diagnostics, file)); len(msgs) != 0 {
		t.Errorf("prelude names must resolve in the editor; got errors %v", msgs)
	}
}

// …and the same buffer analyzed with no standard library still reports them, which is
// what the test above is distinguishing itself from. Without this, a broken LYRA_STD
// would make the test above pass by analyzing nothing at all.
func TestAnalyzeDocument_WithoutStdPreludeNamesDoNot(t *testing.T) {
	t.Setenv("LYRA_STD", t.TempDir()) // a root with no std/ under it
	h := newHandler()
	uri, _ := openDoc(t, h, t.TempDir(), "app.lyra", "let m = Some 1\n")

	res, file := h.analyzeDocument(uri, h.docStore[string(uri)])
	if msgs := errorMessages(diagnosticsFor(res.Diagnostics, file)); len(msgs) == 0 {
		t.Error("with no prelude reachable, `Some` must still be undefined — otherwise this pair proves nothing")
	}
}

// The buffer is analyzed, not the saved file: a server that read the disk would report
// diagnostics about text the user has already replaced.
func TestAnalyzeDocument_AnalyzesTheBufferNotTheFile(t *testing.T) {
	t.Setenv("LYRA_STD", stdRootDir(t))
	dir := t.TempDir()
	path := filepath.Join(dir, "app.lyra")
	if err := os.WriteFile(path, []byte("let broken = @@@\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	h := newHandler()
	uri, _ := openDoc(t, h, dir, "app.lyra", "let ok = Some 1\n")
	res, file := h.analyzeDocument(uri, h.docStore[string(uri)])
	if msgs := errorMessages(diagnosticsFor(res.Diagnostics, file)); len(msgs) != 0 {
		t.Errorf("analysis used the saved file, not the buffer; got %v", msgs)
	}
}

// An unsaved buffer has no path to resolve modules against, so it falls back to the
// single-unit pipeline rather than failing.
func TestAnalyzeDocument_UntitledBuffer(t *testing.T) {
	h := newHandler()
	res, file := h.analyzeDocument("untitled:Untitled-1", "let x = 1\n")
	if file != "" {
		t.Errorf("a non-file URI has no path; got %q", file)
	}
	if res.Program == nil {
		t.Error("an untitled buffer must still be analyzed")
	}
}

// Only this document's diagnostics may be published against its URI: the program now
// spans several files, and another unit's line numbers mean nothing here.
func TestDiagnosticsFor_ExcludesOtherFiles(t *testing.T) {
	diags := []diag.Diagnostic{
		{Message: "mine (stamped)", File: "/w/app.lyra"},
		{Message: "mine (on the location)", Location: ast.Location{File: "/w/app.lyra", StartLine: 3}},
		{Message: "theirs", File: "/w/std/prelude.lyra"},
		{Message: "theirs (on the location)", Location: ast.Location{File: "/w/std/prelude.lyra"}},
		{Message: "program-level"},
	}
	var got []string
	for _, d := range diagnosticsFor(diags, "/w/app.lyra") {
		got = append(got, d.Message)
	}
	want := []string{"mine (stamped)", "mine (on the location)", "program-level"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("got %v; want %v", got, want)
	}
}

// Every position-based handler walks docAnalysis.program, so it must hold only this
// file's statements — otherwise the prelude's line 40 answers a request about the
// user's line 40.
func TestDocProgram_KeepsOnlyThisFile(t *testing.T) {
	t.Setenv("LYRA_STD", stdRootDir(t))
	h := newHandler()
	uri, path := openDoc(t, h, t.TempDir(), "app.lyra", "let m = Some 1\nlet main = () -> u8 => 0\n")

	res, file := h.analyzeDocument(uri, h.docStore[string(uri)])
	if len(res.Program.Statements) <= 2 {
		t.Fatalf("expected the prelude's statements in the whole program; got %d", len(res.Program.Statements))
	}
	doc := docProgram(res.Program, file)
	if len(doc.Statements) != 2 {
		t.Fatalf("got %d statements for the document; want its own 2", len(doc.Statements))
	}
	for _, stmt := range doc.Statements {
		if got := stmt.GetLocation().File; got != path {
			t.Errorf("statement from %q leaked into the document's program", got)
		}
	}
}

// A file:// URI survives the round trip, and a non-file URI has no path at all.
func TestURIPathRoundTrip(t *testing.T) {
	for _, path := range []string{"/w/app.lyra", "/w/a b/app.lyra", "/w/ünïcode.lyra"} {
		if got := uriToPath(pathToURI(path)); got != path {
			t.Errorf("round trip of %q gave %q", path, got)
		}
	}
	if got := uriToPath("untitled:Untitled-1"); got != "" {
		t.Errorf("a non-file URI must have no path; got %q", got)
	}
}
