package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/owenrumney/go-lsp/lsp"
	"github.com/owenrumney/go-lsp/servertest"
)

// The quick fixes that edit an import's member list, asked through the server and applied
// to the buffer as a client would: the text that comes back is compared whole, checked
// again for errors, and — where the list started out as lyrafmt writes it — run through
// lyrafmt, which must leave it alone. Reported 11/11 from Zed as "doesn't add commas in the
// right places": the comma went one character late, `{ twice , thrice}`.

// numsModule exports every name the cases import.
const numsModule = "module nums\n" +
	"pub let twice = (n: i64) -> i64 => n * 2\n" +
	"pub let half = (n: i64) -> i64 => n / 2\n" +
	"pub let thrice = (n: i64) -> i64 => n * 3\n" +
	"pub let quadruple_the_value_it_is_given = (n: i64) -> i64 => n * 4\n" +
	"pub let multiply_the_value_by_eight = (n: i64) -> i64 => n * 8\n" +
	"pub let bump = pure (self: i64) -> i64 => self + 1\n"

type importFixCase struct {
	name string
	// The file's imports, and what they must read after the fix; the body below them is the
	// case's or the shared one.
	imports, want string
	body          string
	// Whether the original is lyrafmt's output, so the result must be too.
	formatted bool
}

func TestCodeAction_AddImportKeepsTheListValid(t *testing.T) {
	const body = "let a = twice(1)\nlet c = half(4)\nlet b = thrice(3)\n"
	cases := []importFixCase{
		{name: "one name", formatted: true,
			imports: "import nums.{ twice, half }",
			want:    "import nums.{ twice, half, thrice }"},
		{name: "a lone name", formatted: true, body: "let a = twice(1)\nlet b = thrice(3)\n",
			imports: "import nums.{ twice }",
			want:    "import nums.{ twice, thrice }"},
		{name: "a trailing comma", formatted: true,
			imports: "import nums.{ twice, half, }",
			want:    "import nums.{ twice, half, thrice, }"},
		{name: "an alias", formatted: true, body: "let a = tw(1)\nlet c = half(4)\nlet b = thrice(3)\n",
			imports: "import nums.{ twice as tw, half }",
			want:    "import nums.{ twice as tw, half, thrice }"},
		{name: "a block comment", formatted: true,
			imports: "import nums.{ twice, half /* halves */ }",
			want:    "import nums.{ twice, half, thrice /* halves */ }"},
		{name: "a name per line", formatted: true,
			imports: "import nums.{\n  twice,\n  half\n}",
			want:    "import nums.{\n  twice,\n  half,\n  thrice\n}"},
		{name: "a name per line with a trailing comma", formatted: true,
			imports: "import nums.{\n  twice,\n  half,\n}",
			want:    "import nums.{\n  twice,\n  half,\n  thrice,\n}"},
		{name: "a name per line with comments", formatted: true,
			imports: "import nums.{\n  twice, // doubles\n  half // halves\n}",
			want:    "import nums.{\n  twice, // doubles\n  half, // halves\n  thrice\n}"},
		// Not formatted: lyrafmt indents a comment after the last member a step further
		// (todo.md), so the case cannot be one it leaves alone.
		{name: "a comment line before the brace",
			imports: "import nums.{\n  twice,\n  half\n  // more to come\n}",
			want:    "import nums.{\n  twice,\n  half,\n  thrice\n  // more to come\n}"},
		{name: "a list past lyrafmt's width", formatted: true,
			body: "let a = twice(1)\nlet c = half(4)\nlet d = quadruple_the_value_it_is_given(1)\n" +
				"let e = multiply_the_value_by_eight(1)\nlet b = thrice(3)\n",
			imports: "import nums.{ twice, half, quadruple_the_value_it_is_given, multiply_the_value_by_eight }",
			want: "import nums.{\n  twice,\n  half,\n  quadruple_the_value_it_is_given,\n" +
				"  multiply_the_value_by_eight,\n  thrice\n}"},
		// `{}` does not parse (a list holds at least one name), so this one is not formatted
		// to begin with; the fix makes it parse.
		{name: "an empty list", body: "let b = thrice(3)\n",
			imports: "import nums.{}",
			want:    "import nums.{ thrice }"},
		{name: "no list", formatted: true, body: "let a = nums.twice(1)\nlet b = thrice(3)\n",
			imports: "import nums",
			want:    "import nums\nimport nums.{ thrice }"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if c.body == "" {
				c.body = body
			}
			runImportFix(t, c, "Import thrice from nums")
		})
	}
}

// An unused member of several is taken out of the list, which the fix declined to do
// before 11/11 ("comma surgery we don't attempt") — while lyra-W004 points at the member.
func TestCodeAction_RemoveUnusedImportMember(t *testing.T) {
	const body = "let a = twice(1)\n"
	cases := []importFixCase{
		{name: "the last of two", formatted: true,
			imports: "import nums.{ twice, half }", want: "import nums.{ twice }"},
		{name: "the first of two", formatted: true,
			imports: "import nums.{ half, twice }", want: "import nums.{ twice }"},
		{name: "a trailing comma", formatted: true,
			imports: "import nums.{ twice, half, }", want: "import nums.{ twice, }"},
		{name: "the last line", formatted: true,
			imports: "import nums.{\n  twice,\n  half\n}", want: "import nums.{\n  twice\n}"},
		{name: "the first line", formatted: true,
			imports: "import nums.{\n  half,\n  twice\n}", want: "import nums.{\n  twice\n}"},
		{name: "the last line with a trailing comma", formatted: true,
			imports: "import nums.{\n  twice,\n  half,\n}", want: "import nums.{\n  twice,\n}"},
		{name: "the last line with comments", formatted: true,
			imports: "import nums.{\n  twice, // doubles\n  half // halves\n}",
			want:    "import nums.{\n  twice // doubles\n}"},
		{name: "a lone member", formatted: true,
			imports: "import nums.{ half }\nimport nums.{ twice }", want: "import nums.{ twice }"},
		// lyra-W004 on a name only ever called method-style says `import nums` alone is
		// enough; deleting the line, as the fix did, broke the call it was offered for.
		{name: "a lone member called method-style", formatted: true, body: "let a = 1.bump()\n",
			imports: "import nums.{ bump }", want: "import nums"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if c.body == "" {
				c.body = body
			}
			runImportFix(t, c, "Remove unused import")
		})
	}
}

// runImportFix opens the case's file beside nums, applies the action titled title, and
// checks the result.
func runImportFix(t *testing.T, c importFixCase, title string) {
	t.Helper()
	t.Setenv("LYRA_STD", stdRootDir(t))
	h := servertest.New(t, newHandler())
	dir := t.TempDir()
	writeFile(t, dir, "nums/nums.lyra", numsModule)
	src := "module app\n" + c.imports + "\n" + c.body
	uri, diags := openForDiagnostics(t, h, dir, "app.lyra", src)
	actions, err := h.CodeAction(&lsp.CodeActionParams{
		TextDocument: lsp.TextDocumentIdentifier{URI: uri},
		Range:        fullRange(),
		Context:      lsp.CodeActionContext{Diagnostics: diags},
	})
	if err != nil {
		t.Fatalf("CodeAction: %v", err)
	}
	a := findAction(actions, title)
	if a == nil {
		t.Fatalf("no %q action; got %v", title, titles(actions))
	}
	got := applyTextEdits(t, src, a.Edit.Changes[uri])
	want := "module app\n" + c.want + "\n" + c.body
	if got != want {
		t.Fatalf("after the fix:\n%s\nwant:\n%s", got, want)
	}

	// Valid Lyra: no syntax or other error in what the fix wrote.
	_, after := openForDiagnostics(t, h, dir, "after.lyra", got)
	for _, d := range after {
		if d.Severity != nil && *d.Severity == lsp.SeverityError {
			t.Errorf("the fixed file has an error: %s (line %d)", d.Message, d.Range.Start.Line)
		}
	}

	if !c.formatted {
		return
	}
	formatter := lyrafmtForTest(t)
	if formatter == "" {
		return
	}
	if out, ok := runLyrafmt(formatter, src); !ok || out != src {
		t.Fatalf("the case is not as lyrafmt writes it:\n%s", out)
	}
	if out, ok := runLyrafmt(formatter, got); !ok || out != got {
		t.Errorf("lyrafmt changes the fixed file:\n%s", out)
	}
}

// openForDiagnostics opens a file in dir and returns its URI with the diagnostics published
// for it.
func openForDiagnostics(t *testing.T, h *servertest.Harness, dir, name, source string) (lsp.DocumentURI, []lsp.Diagnostic) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	uri := lsp.DocumentURI(pathToURI(path))
	if err := h.DidOpen(uri, "lyra", source); err != nil {
		t.Fatalf("DidOpen: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	diags, err := h.WaitForDiagnostics(ctx, uri)
	if err != nil {
		t.Fatalf("WaitForDiagnostics: %v", err)
	}
	return uri, diags
}

// lyrafmtForTest is the lyrafmt build.sh built beside the server, or "" — the formatting
// half of these checks is skipped without one (formatting_test.go builds its own; these
// tests ask only that the edit is one lyrafmt would keep).
func lyrafmtForTest(t *testing.T) string {
	t.Helper()
	if env := os.Getenv("LYRA_FMT"); env != "" {
		return env
	}
	bin, err := filepath.Abs(filepath.Join("..", "..", "build", "lyrafmt"))
	if err == nil {
		if _, err := exec.LookPath(bin); err == nil {
			return bin
		}
	}
	t.Log("no build/lyrafmt: the lyrafmt check is skipped (run ./build.sh)")
	return ""
}

// applyTextEdits applies LSP edits — multi-line ones included — to an ASCII source, as a
// client does: every range against the original text, the last first.
func applyTextEdits(t *testing.T, src string, edits []lsp.TextEdit) string {
	t.Helper()
	if len(edits) == 0 {
		t.Fatal("the action has no edits")
	}
	offset := func(p lsp.Position) int {
		at := lineStartByte(src, p.Line) + p.Character
		if at > len(src) {
			t.Fatalf("position %+v is past the end of the source", p)
		}
		return at
	}
	sorted := append([]lsp.TextEdit(nil), edits...)
	sort.SliceStable(sorted, func(i, j int) bool {
		return offset(sorted[i].Range.Start) > offset(sorted[j].Range.Start)
	})
	out := src
	for _, e := range sorted {
		start, end := offset(e.Range.Start), offset(e.Range.End)
		if start > end {
			t.Fatalf("an edit's range runs backwards: %+v", e.Range)
		}
		out = out[:start] + e.NewText + out[end:]
	}
	return out
}
