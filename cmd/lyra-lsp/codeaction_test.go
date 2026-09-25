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

// openAndDiags opens a document, waits for analysis, and returns the published
// diagnostics so they can be replayed into a code-action request (as a real
// client would).
func openAndDiags(t *testing.T, h *servertest.Harness, source string) []lsp.Diagnostic {
	t.Helper()
	if err := h.DidOpen(testURI, "lyra", source); err != nil {
		t.Fatalf("DidOpen: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	diags, err := h.WaitForDiagnostics(ctx, testURI)
	if err != nil {
		t.Fatalf("WaitForDiagnostics: %v", err)
	}
	return diags
}

// requestActions replays diags into a full-document code-action request.
func requestActions(t *testing.T, h *servertest.Harness, diags []lsp.Diagnostic) []lsp.CodeAction {
	t.Helper()
	actions, err := h.CodeAction(&lsp.CodeActionParams{
		TextDocument: lsp.TextDocumentIdentifier{URI: testURI},
		Range:        fullRange(),
		Context:      lsp.CodeActionContext{Diagnostics: diags},
	})
	if err != nil {
		t.Fatalf("CodeAction: %v", err)
	}
	return actions
}

// findAction returns the first action whose title contains substr, or nil.
func findAction(actions []lsp.CodeAction, substr string) *lsp.CodeAction {
	for i := range actions {
		if strings.Contains(actions[i].Title, substr) {
			return &actions[i]
		}
	}
	return nil
}

// editText returns the NewText of the single edit in an action's workspace edit.
func editText(t *testing.T, a *lsp.CodeAction) (lsp.TextEdit, string) {
	t.Helper()
	if a.Edit == nil || len(a.Edit.Changes[testURI]) != 1 {
		t.Fatalf("expected exactly one edit, got %+v", a.Edit)
	}
	e := a.Edit.Changes[testURI][0]
	return e, e.NewText
}

func TestCodeAction_MissingMatchArms(t *testing.T) {
	h := servertest.New(t, newHandler())
	// The scrutinee is bound to a *nullary* constructor (`let s = Zero`) and
	// matched on the next line — the exact shape that used to mis-parse (the
	// nullary value swallowing the following `match`), now fixed in the grammar.
	// The missing arms exercise both a payload constructor (Pos → `Pos _`) and a
	// nullary one (Unknown → bare).
	src := `
	data Sign = Pos i32 | Zero | Unknown
	let s = Zero
	match s {
	    Zero => 0,
	}`
	diags := openAndDiags(t, h, src)
	actions := requestActions(t, h, diags)

	a := findAction(actions, "Add missing match arms")
	if a == nil {
		t.Fatalf("no match-arms action; actions=%v", titles(actions))
	}
	_, text := editText(t, a)
	if !strings.Contains(text, "Pos _ => todo()") {
		t.Errorf("expected payload constructor arm `Pos _`, got: %q", text)
	}
	if !strings.Contains(text, "Unknown => todo()") || strings.Contains(text, "Unknown _") {
		t.Errorf("expected bare nullary arm `Unknown`, got: %q", text)
	}
}

func TestCodeAction_MissingStructFields(t *testing.T) {
	h := servertest.New(t, newHandler())
	src := `
	struct Point { x: i64, y: i64 }
	let p = Point { x: 1 }`
	diags := openAndDiags(t, h, src)
	actions := requestActions(t, h, diags)

	a := findAction(actions, "Add missing fields")
	if a == nil {
		t.Fatalf("no struct-fields action; actions=%v", titles(actions))
	}
	_, text := editText(t, a)
	if !strings.Contains(text, "y: todo()") {
		t.Errorf("expected `y: todo()`, got: %q", text)
	}
	if !strings.HasPrefix(text, ", ") {
		t.Errorf("expected a separator comma before the new field, got: %q", text)
	}
}

func TestCodeAction_RemoveUnusedVariable(t *testing.T) {
	h := servertest.New(t, newHandler())
	src := `
	let f = () -> i64 => {
		let unused = 5
		42
	}`
	diags := openAndDiags(t, h, src)
	actions := requestActions(t, h, diags)

	a := findAction(actions, "Remove unused variable")
	if a == nil {
		t.Fatalf("no remove-variable action; actions=%v", titles(actions))
	}
	edit, text := editText(t, a)
	if text != "" {
		t.Errorf("expected an empty (deletion) edit, got: %q", text)
	}
	// The deletion should span the whole `let unused = 5` line (line 2).
	if edit.Range.Start.Line != 2 || edit.Range.End.Line != 3 {
		t.Errorf("expected deletion of line 2, got range %+v", edit.Range)
	}
}

func TestCodeAction_RemoveUnusedImport(t *testing.T) {
	h := servertest.New(t, newHandler())
	src := `
	import foo.bar
	let x = 1
	x`
	diags := openAndDiags(t, h, src)
	actions := requestActions(t, h, diags)

	a := findAction(actions, "Remove unused import")
	if a == nil {
		t.Fatalf("no remove-import action; actions=%v", titles(actions))
	}
	_, text := editText(t, a)
	if text != "" {
		t.Errorf("expected an empty (deletion) edit, got: %q", text)
	}
}

func TestCodeAction_InsertTypeAnnotation(t *testing.T) {
	h := servertest.New(t, newHandler())
	src := "let x = 5"
	// No diagnostics needed — this is a range-driven refactor.
	openAndDiags(t, h, src)
	actions := requestActions(t, h, nil)

	a := findAction(actions, "Insert inferred type annotation")
	if a == nil {
		t.Fatalf("no insert-annotation action; actions=%v", titles(actions))
	}
	edit, text := editText(t, a)
	if !strings.HasPrefix(text, ": ") {
		t.Errorf("expected annotation text to start with ': ', got %q", text)
	}
	// "let x" → insert right after 'x' at col 5 (0-based).
	if edit.Range.Start.Line != 0 || edit.Range.Start.Character != 5 {
		t.Errorf("expected insertion at (0,5), got %+v", edit.Range.Start)
	}
}

func TestCodeAction_AnnotatedBinding_NoInsertAction(t *testing.T) {
	h := servertest.New(t, newHandler())
	openAndDiags(t, h, "let x: i64 = 5")
	actions := requestActions(t, h, nil)
	if a := findAction(actions, "Insert inferred type annotation"); a != nil {
		t.Errorf("did not expect an annotation action for an annotated binding")
	}
}

func titles(actions []lsp.CodeAction) []string {
	out := make([]string, len(actions))
	for i, a := range actions {
		out[i] = a.Title
	}
	return out
}

// **A constructor named for only some payloads gets an arm too.** lyra-E009 has refused
// `Some(0)` beside `None` since 09/13, and until the action shared the check's computation
// (typechecker.MatchGaps) it offered nothing for it — its own walk only listed constructors
// no arm named. The scrutinee is generic, which that walk also could not resolve.
func TestCodeAction_PartlyCoveredConstructor(t *testing.T) {
	h := servertest.New(t, newHandler())
	src := `
	data Opt<t> = Some t | None | Other
	let f = (m: Opt<i64>) -> i64 => match m {
	    Some(0) => 1,
	    None => 2,
	}`
	diags := openAndDiags(t, h, src)
	actions := requestActions(t, h, diags)

	a := findAction(actions, "Add missing match arms")
	if a == nil {
		t.Fatalf("no match-arms action; actions=%v diags=%v", titles(actions), diags)
	}
	_, text := editText(t, a)
	if !strings.Contains(text, "Other => todo()") {
		t.Errorf("expected the missing constructor's arm, got: %q", text)
	}
	if !strings.Contains(text, "Some _ => todo()") {
		t.Errorf("expected a `Some _` arm for the partly covered constructor, got: %q", text)
	}
	if !strings.Contains(a.Title, "Other, Some") {
		t.Errorf("title should list missing then partial, got %q", a.Title)
	}
}

// A method call does not require the method's name in the import list (09/22), so a file
// can reach a function it never named — and the first thing a reader wants is to put the
// name in the list. This is that quick fix, on the diagnostic that already names it in
// prose.
func TestCodeAction_AddImportIntoAnExistingList(t *testing.T) {
	t.Setenv("LYRA_STD", stdRootDir(t))
	h := servertest.New(t, newHandler())
	dir := t.TempDir()
	writeFile(t, dir, "nums/nums.lyra", "module nums\n"+
		"pub let twice = pure (self: i64) -> i64 => self * 2\n"+
		"pub let thrice = pure (self: i64) -> i64 => self * 3\n")
	src := "module app\nimport nums.{ twice }\nlet a = 1.twice()\nlet b = thrice(3)\n"
	uri := openFileAndWait(t, h, dir, "app.lyra", src)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	diags, err := h.WaitForDiagnostics(ctx, uri)
	if err != nil {
		t.Fatalf("WaitForDiagnostics: %v", err)
	}
	actions, err := h.CodeAction(&lsp.CodeActionParams{
		TextDocument: lsp.TextDocumentIdentifier{URI: uri},
		Range:        fullRange(),
		Context:      lsp.CodeActionContext{Diagnostics: diags},
	})
	if err != nil {
		t.Fatalf("CodeAction: %v", err)
	}
	a := findAction(actions, "Import thrice from nums")
	if a == nil {
		t.Fatalf("no add-import action; got %v", titles(actions))
	}
	edits := a.Edit.Changes[uri]
	if len(edits) != 1 {
		t.Fatalf("want one edit, got %d", len(edits))
	}
	// Appended to the existing member list rather than added as a second import line.
	if edits[0].NewText != ", thrice" {
		t.Errorf("want %q, got %q", ", thrice", edits[0].NewText)
	}
	if edits[0].Range.Start.Line != 1 {
		t.Errorf("the edit belongs on the import line; got line %d", edits[0].Range.Start.Line)
	}
}

// A module imported plainly binds no bare names, so the name cannot join a list that is
// not there — a new import line goes in beside it, leaving both spellings working.
func TestCodeAction_AddImportAsANewLine(t *testing.T) {
	t.Setenv("LYRA_STD", stdRootDir(t))
	h := servertest.New(t, newHandler())
	dir := t.TempDir()
	writeFile(t, dir, "nums/nums.lyra", "module nums\n"+
		"pub let thrice = pure (n: i64) -> i64 => n * 3\n")
	src := "module app\nimport nums\nlet b = thrice(3)\n"
	uri := openFileAndWait(t, h, dir, "app.lyra", src)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	diags, err := h.WaitForDiagnostics(ctx, uri)
	if err != nil {
		t.Fatalf("WaitForDiagnostics: %v", err)
	}
	actions, err := h.CodeAction(&lsp.CodeActionParams{
		TextDocument: lsp.TextDocumentIdentifier{URI: uri},
		Range:        fullRange(),
		Context:      lsp.CodeActionContext{Diagnostics: diags},
	})
	if err != nil {
		t.Fatalf("CodeAction: %v", err)
	}
	a := findAction(actions, "Import thrice from nums")
	if a == nil {
		t.Fatalf("no add-import action; got %v", titles(actions))
	}
	edits := a.Edit.Changes[uri]
	if len(edits) != 1 || edits[0].NewText != "import nums.{ thrice }\n" {
		t.Fatalf("want a new import line, got %v", edits)
	}
	if edits[0].Range.Start.Line != 2 {
		t.Errorf("the new line belongs after the last import; got line %d", edits[0].Range.Start.Line)
	}
}

// **The case the feature exists for**, and the one it could not do when it was first
// written: a module the file never mentions. It is not in the symbol table —
// `modules.Resolve` loads what a file imports and no more — so the offer comes from a
// workspace search instead, the same walk rename uses for the other upward question.
func TestCodeAction_AddImportFromAnUnmentionedModule(t *testing.T) {
	t.Setenv("LYRA_STD", stdRootDir(t))
	h := servertest.New(t, newHandler())
	dir := t.TempDir()
	writeFile(t, dir, "nums/nums.lyra", "module nums\n"+
		"pub let thrice = pure (n: i64) -> i64 => n * 3\n")
	src := "module app\nlet b = thrice(3)\n"
	uri := openFileAndWait(t, h, dir, "app.lyra", src)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	diags, err := h.WaitForDiagnostics(ctx, uri)
	if err != nil {
		t.Fatalf("WaitForDiagnostics: %v", err)
	}
	actions, err := h.CodeAction(&lsp.CodeActionParams{
		TextDocument: lsp.TextDocumentIdentifier{URI: uri},
		Range:        fullRange(),
		Context:      lsp.CodeActionContext{Diagnostics: diags},
	})
	if err != nil {
		t.Fatalf("CodeAction: %v", err)
	}
	a := findAction(actions, "Import thrice from nums")
	if a == nil {
		t.Fatalf("no add-import action; got %v", titles(actions))
	}
	edits := a.Edit.Changes[uri]
	if len(edits) != 1 || edits[0].NewText != "import nums.{ thrice }\n" {
		t.Fatalf("want a new import line, got %v", edits)
	}
	// After the `module` declaration, there being no import to follow.
	if edits[0].Range.Start.Line != 1 {
		t.Errorf("want the line after the module declaration; got line %d", edits[0].Range.Start.Line)
	}
}

// **A symlinked module directory is searched through**, which `filepath.WalkDir` does not
// do on its own. `build/std` is a symlink to the real `std/` — deliberately, so a copy
// cannot drift from the edited prelude — and `build` is the std root the *editor* resolves
// against, so without following it the standard library is invisible to this search from
// the root the editor actually uses. The name that sent us here, `parse_args`, lives there.
func TestCodeAction_AddImportThroughASymlinkedRoot(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "real")
	writeFile(t, real, "nums/nums.lyra", "module nums\n"+
		"pub let thrice = pure (n: i64) -> i64 => n * 3\n")
	link := filepath.Join(dir, "linked")
	if err := os.MkdirAll(link, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(real, "nums"), filepath.Join(link, "nums")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	t.Setenv("LYRA_STD", link)
	h := servertest.New(t, newHandler())
	// The document lives somewhere else entirely, so the only way to the module is the
	// std root — through the symlink.
	src := "module app\nlet b = thrice(3)\n"
	elsewhere := filepath.Join(dir, "elsewhere")
	if err := os.MkdirAll(elsewhere, 0o755); err != nil {
		t.Fatal(err)
	}
	uri := openFileAndWait(t, h, elsewhere, "app.lyra", src)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	diags, err := h.WaitForDiagnostics(ctx, uri)
	if err != nil {
		t.Fatalf("WaitForDiagnostics: %v", err)
	}
	actions, err := h.CodeAction(&lsp.CodeActionParams{
		TextDocument: lsp.TextDocumentIdentifier{URI: uri},
		Range:        fullRange(),
		Context:      lsp.CodeActionContext{Diagnostics: diags},
	})
	if err != nil {
		t.Fatalf("CodeAction: %v", err)
	}
	if a := findAction(actions, "Import thrice from nums"); a == nil {
		t.Errorf("a module behind a symlinked root should be offered; got %v", titles(actions))
	}
}
