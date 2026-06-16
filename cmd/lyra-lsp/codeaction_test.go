package main

import (
	"context"
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
	// The scrutinee is bound to a payload constructor (`Pos 1`); a nullary value
	// like `let s = Zero` currently mis-parses (the value swallows the following
	// `match`), an unrelated grammar bug. The missing arms still exercise both a
	// payload constructor (Neg → `Neg _`) and a nullary one (Zero → bare).
	src := "data Sign = Pos i32 | Neg i32 | Zero\nlet s = Pos 1\nmatch s {\n    Pos x => 1,\n}"
	diags := openAndDiags(t, h, src)
	actions := requestActions(t, h, diags)

	a := findAction(actions, "Add missing match arms")
	if a == nil {
		t.Fatalf("no match-arms action; actions=%v", titles(actions))
	}
	_, text := editText(t, a)
	if !strings.Contains(text, "Neg _ => todo()") {
		t.Errorf("expected payload constructor arm `Neg _`, got: %q", text)
	}
	if !strings.Contains(text, "Zero => todo()") || strings.Contains(text, "Zero _") {
		t.Errorf("expected bare nullary arm `Zero`, got: %q", text)
	}
}

func TestCodeAction_MissingStructFields(t *testing.T) {
	h := servertest.New(t, newHandler())
	src := "struct Point { x: i64, y: i64 }\nlet p = Point { x: 1 }"
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
	src := "let f = () -> i64 => {\n  let unused = 5\n  42\n}"
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
	// The deletion should span the whole `let unused = 5` line (line 1).
	if edit.Range.Start.Line != 1 || edit.Range.End.Line != 2 {
		t.Errorf("expected deletion of line 1, got range %+v", edit.Range)
	}
}

func TestCodeAction_RemoveUnusedImport(t *testing.T) {
	h := servertest.New(t, newHandler())
	src := "import foo.bar\nlet x = 1\nx"
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
