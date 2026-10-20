package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/owenrumney/go-lsp/lsp"
	"github.com/owenrumney/go-lsp/servertest"
)

// buildLui builds tools/lui, a Lyra program over std alone, so a C compiler is all it
// needs; and points the server at it.
func buildLui(t *testing.T) (lui, root string) {
	t.Helper()
	if _, err := exec.LookPath("clang"); err != nil {
		t.Skip("clang not found on PATH")
	}
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate this test file")
	}
	root = filepath.Clean(filepath.Join(filepath.Dir(thisFile), "..", ".."))
	lui = filepath.Join(t.TempDir(), "lui")
	build := exec.Command("go", "run", "./cmd/lyrac", "build", "-o", lui, filepath.Join(root, "tools", "lui", "lui.lyra"))
	build.Dir = root
	build.Env = append(os.Environ(), "LYRA_STD="+root)
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("building lui: %v\n%s", err, out)
	}
	t.Setenv("LYRA_LUI", lui)
	t.Setenv("LYRA_STD", root)
	luiTool.reset()
	t.Cleanup(luiTool.reset)
	return lui, root
}

// openMarkup writes source as dir/view.lui, opens it, and answers what was published.
func openMarkup(t *testing.T, h *servertest.Harness, source string) []lsp.Diagnostic {
	t.Helper()
	path := filepath.Join(t.TempDir(), "view.lui")
	if err := os.WriteFile(path, []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	uri := lsp.DocumentURI(pathToURI(path))
	if err := h.DidOpen(uri, "lui", source); err != nil {
		t.Fatalf("DidOpen: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	diags, err := h.WaitForDiagnostics(ctx, uri)
	if err != nil {
		t.Fatalf("WaitForDiagnostics: %v", err)
	}
	return diags
}

// lyrac's errors in the module lui generates land on the markup's lines: one in an
// attribute at its element's line, one in the script at its own — the column is the
// line's text, not the generated module's.
func TestMarkup_LyraErrorsAtTheMarkupsLines(t *testing.T) {
	buildLui(t)
	h := servertest.New(t, newHandler())
	source := `<script>
  pub data Ev = Go

  let broken = () -> bool => 7
</script>
<component name="view" event="Ev">
  <Row>
    <Button on_click={Gone}>Go</Button>
  </Row>
</component>
`
	diags := openMarkup(t, h, source)
	on := func(line int, text string) bool {
		for _, d := range diags {
			if d.Range.Start.Line == line && strings.Contains(d.Message, text) {
				return true
			}
		}
		return false
	}
	if !on(3, "expected bool") {
		t.Errorf("the script's error is not on its line (4): %v", diags)
	}
	if !on(7, "Gone") {
		t.Errorf("the attribute's error is not on its element's line (8): %v", diags)
	}
	seen := map[string]bool{}
	for _, d := range diags {
		if d.Range.Start.Line != 3 && d.Range.Start.Line != 7 {
			t.Errorf("a diagnostic off the two lines: %v", d)
		}
		if d.Range.Start.Line == 7 && d.Range.Start.Character != 4 {
			t.Errorf("the range should start at the tag, past the indentation: %v", d.Range)
		}
		key := fmt.Sprintf("%d %s", d.Range.Start.Line, d.Message)
		if seen[key] {
			t.Errorf("published twice: %v", d)
		}
		seen[key] = true
	}
}

// A mistake in the markup itself is lui's, at its line; a clean file has nothing.
func TestMarkup_MarkupMistakesAndCleanFiles(t *testing.T) {
	buildLui(t)
	h := servertest.New(t, newHandler())
	diags := openMarkup(t, h, "<component name=\"a\">\n  <Buton/>\n</component>\n")
	if len(diags) != 1 || diags[0].Range.Start.Line != 1 || !strings.Contains(diags[0].Message, "<Buton> is not an element") {
		t.Errorf("want lui's own error on line 2: %v", diags)
	}
	clean := openMarkup(t, h, "<component name=\"a\" props={{ go: e }}>\n  <Button on_click={go}>Go</Button>\n</component>\n")
	if len(clean) != 0 {
		t.Errorf("a clean file was given diagnostics: %v", clean)
	}
}

// Without lui there is nothing to say, as without lyrafmt nothing is formatted.
func TestMarkup_WithoutLuiSaysNothing(t *testing.T) {
	t.Setenv("LYRA_LUI", filepath.Join(t.TempDir(), "absent"))
	t.Setenv("PATH", t.TempDir())
	luiTool.reset()
	t.Cleanup(luiTool.reset)
	h := servertest.New(t, newHandler())
	if diags := openMarkup(t, h, "<component name=\"a\">\n  <Buton/>\n</component>\n"); len(diags) != 0 {
		t.Errorf("diagnostics without lui: %v", diags)
	}
}

// The marker format has two readers, `lui map` and markupLineOf; on every line of the
// example's generated module they answer alike.
func TestMarkup_MapsAsLuiMapDoes(t *testing.T) {
	lui, root := buildLui(t)
	module := filepath.Join(root, "examples", "imgui", "components_view.lyra")
	code, err := os.ReadFile(module)
	if err != nil {
		t.Fatal(err)
	}
	rows := strings.Split(string(code), "\n")
	var in strings.Builder
	for row := 1; row <= len(rows); row++ {
		fmt.Fprintf(&in, "%s:%d:1: x\n", module, row)
	}
	cmd := exec.Command(lui, "map")
	cmd.Stdin = strings.NewReader(in.String())
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("lui map: %v", err)
	}
	mapped := strings.Split(strings.TrimRight(string(out), "\n"), "\n")
	markup := filepath.Join(root, "examples", "imgui", "components_view.lui")
	for row := 1; row <= len(rows); row++ {
		want := fmt.Sprintf("%s:%d:1: x", module, row)
		if n, ok := markupLineOf(rows, "components_view.lui", row); ok {
			want = fmt.Sprintf("%s:%d: x", markup, n)
		}
		if mapped[row-1] != want {
			t.Errorf("line %d: lui map says %q, the server %q", row, mapped[row-1], want)
		}
	}
}
