package main

import (
	"sort"
	"strings"
	"testing"

	"github.com/owenrumney/go-lsp/lsp"
	"github.com/owenrumney/go-lsp/servertest"
)

// editLines returns the sorted 0-based start lines of the TextEdits from a WorkspaceEdit.
func editLines(we *lsp.WorkspaceEdit, uri lsp.DocumentURI) []int {
	if we == nil {
		return nil
	}
	var lines []int
	for _, edit := range we.Changes[uri] {
		lines = append(lines, edit.Range.Start.Line)
	}
	sort.Ints(lines)
	return lines
}

func assertEditLines(t *testing.T, we *lsp.WorkspaceEdit, want []int) {
	t.Helper()
	got := editLines(we, testURI)
	if len(got) != len(want) {
		t.Fatalf("expected edits on lines %v, got %v", want, got)
	}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("edit %d: expected line %d, got %d", i, w, got[i])
		}
	}
	for _, edit := range we.Changes[testURI] {
		if edit.NewText != "renamed" {
			t.Errorf("expected new name %q, got %q", "renamed", edit.NewText)
		}
	}
}

// Source layout (tab-indented, 0-based lines):
//
//	0: ""
//	1: "\tlet x = 5"          declaration of x
//	2: "\tlet y = x"          usage
//	3: "\tlet z = x"          usage
const src3 = `
	let x = 5
	let y = x
	let z = x`

func TestRename_FromUsage(t *testing.T) {
	h := servertest.New(t, newHandler())
	openAndWait(t, h, src3)
	// Cursor on "x" in "let y = x" (line 2, col 9 — usage position).
	we, err := h.Rename(testURI, 2, 9, "renamed")
	if err != nil {
		t.Fatalf("Rename: %v", err)
	}
	if we == nil {
		t.Fatal("expected WorkspaceEdit, got nil")
	}
	// Declaration name on line 1, two usages on lines 2 and 3.
	assertEditLines(t, we, []int{1, 2, 3})
}

func TestRename_FromDeclarationName(t *testing.T) {
	h := servertest.New(t, newHandler())
	openAndWait(t, h, src3)
	// Cursor on "x" in "let x = 5" (line 1, col 5 — declaration-name position).
	we, err := h.Rename(testURI, 1, 5, "renamed")
	if err != nil {
		t.Fatalf("Rename: %v", err)
	}
	if we == nil {
		t.Fatal("expected WorkspaceEdit, got nil (cursor on declaration name should work)")
	}
	// Declaration name on line 1, two usages on lines 2 and 3.
	assertEditLines(t, we, []int{1, 2, 3})
}

func TestRename_ShadowedBindingNotAffected(t *testing.T) {
	h := servertest.New(t, newHandler())
	src := `
	let x = 1
	let a = x
	let f = () -> i32 => {
		let x = 2
		x
	}`
	openAndWait(t, h, src)
	// Rename outer "x" from its usage on line 2.
	we, err := h.Rename(testURI, 2, 9, "outer")
	if err != nil {
		t.Fatalf("Rename: %v", err)
	}
	lines := editLines(we, testURI)
	// Outer declaration (line 1) and outer usage (line 2) — inner shadow excluded.
	want := []int{1, 2}
	if len(lines) != len(want) {
		t.Fatalf("expected edits on lines %v, got %v", want, lines)
	}
	for i, w := range want {
		if lines[i] != w {
			t.Errorf("edit %d: expected line %d, got %d", i, w, lines[i])
		}
	}
}

func TestRename_Parameter(t *testing.T) {
	h := servertest.New(t, newHandler())
	src := `
	let f = (n: i32) -> i32 => {
		let m = n
		n
	}`
	openAndWait(t, h, src)
	// Rename "n" from its usage on line 2 (col 10 = tab+tab+"let m = n", 'n' at pos 10).
	we, err := h.Rename(testURI, 2, 10, "count")
	if err != nil {
		t.Fatalf("Rename: %v", err)
	}
	lines := editLines(we, testURI)
	// Parameter decl on line 1, two usages on lines 2 and 3.
	want := []int{1, 2, 3}
	if len(lines) != len(want) {
		t.Fatalf("expected edits on lines %v, got %v", want, lines)
	}
}

func TestRename_FromParameterDeclaration(t *testing.T) {
	h := servertest.New(t, newHandler())
	// Source (tab-indented):
	//   line 0: ""
	//   line 1: "\tlet f = (n: i32) -> i32 => {"   ← cursor on "n" at col 10
	//   line 2: "\t\tlet m = n"
	//   line 3: "\t\tn"
	//   line 4: "\t}"
	src := `
	let f = (n: i32) -> i32 => {
		let m = n
		n
	}`
	openAndWait(t, h, src)
	// Cursor on "n" in the parameter list (line 1, col 10).
	we, err := h.Rename(testURI, 1, 10, "count")
	if err != nil {
		t.Fatalf("Rename: %v", err)
	}
	if we == nil {
		t.Fatal("expected WorkspaceEdit for parameter rename from declaration, got nil")
	}
	lines := editLines(we, testURI)
	// Parameter decl on line 1, two usages on lines 2 and 3.
	want := []int{1, 2, 3}
	if len(lines) != len(want) {
		t.Fatalf("expected edits on lines %v, got %v", want, lines)
	}
}

func TestPrepareRename_OnParameterDeclaration(t *testing.T) {
	h := servertest.New(t, newHandler())
	src := `
	let f = (n: i32) -> i32 => {
		n
	}`
	openAndWait(t, h, src)
	// Cursor on "n" in the parameter list (line 1, col 10).
	result, err := h.PrepareRename(testURI, 1, 10)
	if err != nil {
		t.Fatalf("PrepareRename: %v", err)
	}
	if result == nil {
		t.Fatal("expected PrepareRenameResult on parameter declaration, got nil")
	}
	if result.Placeholder != "n" {
		t.Errorf("expected placeholder %q, got %q", "n", result.Placeholder)
	}
}

func TestRename_NoResultOnLiteral(t *testing.T) {
	h := servertest.New(t, newHandler())
	openAndWait(t, h, "let x = 42")
	we, err := h.Rename(testURI, 0, 8, "foo")
	if err != nil {
		t.Fatalf("Rename: %v", err)
	}
	if we != nil && len(we.Changes[testURI]) != 0 {
		t.Errorf("expected no edits on a literal, got %d", len(we.Changes[testURI]))
	}
}

// TestRename_DeclarationEditIsNameOnly verifies that the edit at the declaration
// site covers only the bound name, not the full "let name = value" statement.
func TestRename_DeclarationEditIsNameOnly(t *testing.T) {
	h := servertest.New(t, newHandler())
	// "x" is at col 5 (0-based) on line 0 in "let x = 42".
	openAndWait(t, h, "let x = 42")
	we, err := h.Rename(testURI, 0, 4, "renamed")
	if err != nil {
		t.Fatalf("Rename: %v", err)
	}
	if we == nil || len(we.Changes[testURI]) == 0 {
		t.Fatal("expected at least one edit")
	}
	for _, edit := range we.Changes[testURI] {
		start := edit.Range.Start.Character
		end := edit.Range.End.Character
		length := end - start
		// "x" is 1 char; if the range covered "let x = 42" it would be much longer.
		if length != 1 {
			t.Errorf("declaration edit range should be 1 char (just 'x'), got %d chars (col %d–%d)", length, start, end)
		}
	}
}

func TestPrepareRename_OnUsage(t *testing.T) {
	h := servertest.New(t, newHandler())
	// "myVar" usage is on line 2 at col 9 (0-based: tab + "let b = " = 9 chars).
	src := `
	let myVar = 5
	let b = myVar`
	openAndWait(t, h, src)
	result, err := h.PrepareRename(testURI, 2, 9)
	if err != nil {
		t.Fatalf("PrepareRename: %v", err)
	}
	if result == nil {
		t.Fatal("expected PrepareRenameResult on usage, got nil")
	}
	if result.Placeholder != "myVar" {
		t.Errorf("expected placeholder %q, got %q", "myVar", result.Placeholder)
	}
}

func TestPrepareRename_OnDeclarationName(t *testing.T) {
	h := servertest.New(t, newHandler())
	// "myVar" declaration name is on line 1 at col 5 (0-based: tab + "let " = 5 chars).
	src := `
	let myVar = 5
	let b = myVar`
	openAndWait(t, h, src)
	result, err := h.PrepareRename(testURI, 1, 5)
	if err != nil {
		t.Fatalf("PrepareRename: %v", err)
	}
	if result == nil {
		t.Fatal("expected PrepareRenameResult on declaration name, got nil")
	}
	if result.Placeholder != "myVar" {
		t.Errorf("expected placeholder %q, got %q", "myVar", result.Placeholder)
	}
}

func TestPrepareRename_OnLiteral(t *testing.T) {
	h := servertest.New(t, newHandler())
	openAndWait(t, h, "let x = 42")
	result, err := h.PrepareRename(testURI, 0, 8)
	if err != nil {
		t.Fatalf("PrepareRename: %v", err)
	}
	if result != nil {
		t.Errorf("expected nil result on a literal, got %+v", result)
	}
}

// **A declaration position must be renameable**, and for a local or a parameter it was
// not: the walk that finds a declaration name returned false from its expression
// callback, so it stopped at the LambdaExpr that every function body lives under. Renaming
// from a *use* worked, which is why it went unnoticed — the obvious place to start a
// rename is the `let` itself.
func TestRename_FromALocalDeclarationName(t *testing.T) {
	h := servertest.New(t, newHandler())
	src := `
let f = pure (n: i64) -> i64 => {
	let doubled = n * 2
	doubled + 1
}`
	openAndWait(t, h, src)
	// `doubled` at its own `let`, line 2 (0-based), just past the tab.
	we, err := h.Rename(testURI, 2, 5, "scaled")
	if err != nil {
		t.Fatalf("Rename: %v", err)
	}
	if we == nil {
		t.Fatal("renaming a local from its declaration must not be declined")
	}
	edits := we.Changes[testURI]
	if len(edits) != 2 {
		t.Errorf("got %d edit(s), want 2 (the declaration and its use): %v", len(edits), edits)
	}
}

// A **parameter of an expression-bodied function**, which is the shape most of the
// standard library is written in. Its uses resolve through the lambda's own scope — a body
// that is not a block records none of its own — so before 08/22 the rename edited the
// declaration and left every use behind, which is worse than declining.
func TestRename_FromAParameterOfAnExpressionBody(t *testing.T) {
	h := servertest.New(t, newHandler())
	src := `
let f = pure (n: i64) -> i64 => n + n`
	openAndWait(t, h, src)
	we, err := h.Rename(testURI, 1, strings.Index(strings.Split(src, "\n")[1], "n: i64"), "count")
	if err != nil {
		t.Fatalf("Rename: %v", err)
	}
	if we == nil {
		t.Fatal("renaming a parameter of an expression-bodied function must not be declined")
	}
	edits := we.Changes[testURI]
	if len(edits) != 3 {
		t.Errorf("got %d edit(s), want 3 (the parameter and both uses): %v", len(edits), edits)
	}
}
