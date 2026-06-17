package main

import (
	"testing"

	"github.com/owenrumney/go-lsp/lsp"
	"github.com/owenrumney/go-lsp/servertest"
)

func TestDocumentHighlight_LocalVariable(t *testing.T) {
	h := servertest.New(t, newHandler())
	src := `
	let x = 5
	let y = x
	let z = x`
	openAndWait(t, h, src)
	// Cursor on "x" in "let y = x" (line 2, col 9).
	highlights, err := h.DocumentHighlight(testURI, 2, 9)
	if err != nil {
		t.Fatalf("DocumentHighlight: %v", err)
	}
	// Declaration on line 1 + two usages on lines 2 and 3.
	lines := refLines(highlightLocs(highlights))
	want := []int{1, 2, 3}
	if len(lines) != len(want) {
		t.Fatalf("expected %d highlights, got %d (%v)", len(want), len(lines), lines)
	}
	for i := range want {
		if lines[i] != want[i] {
			t.Errorf("highlight %d: expected line %d, got %d", i, want[i], lines[i])
		}
	}
}

func TestDocumentHighlight_ShadowedNameExcluded(t *testing.T) {
	h := servertest.New(t, newHandler())
	src := `
	let x = 1
	let a = x
	let f = () -> i32 => {
		let x = 2
		x
	}`
	openAndWait(t, h, src)
	// Cursor on outer "x" in "let a = x" (line 2, col 9).
	highlights, err := h.DocumentHighlight(testURI, 2, 9)
	if err != nil {
		t.Fatalf("DocumentHighlight: %v", err)
	}
	lines := refLines(highlightLocs(highlights))
	// Outer decl (line 1) + outer usage (line 2); inner shadow excluded.
	want := []int{1, 2}
	if len(lines) != len(want) {
		t.Fatalf("expected %d highlights, got %d (%v)", len(want), len(lines), lines)
	}
	for i := range want {
		if lines[i] != want[i] {
			t.Errorf("highlight %d: expected line %d, got %d", i, want[i], lines[i])
		}
	}
}

func TestDocumentHighlight_NoResultOnLiteral(t *testing.T) {
	h := servertest.New(t, newHandler())
	openAndWait(t, h, "let x = 42")
	highlights, err := h.DocumentHighlight(testURI, 0, 8)
	if err != nil {
		t.Fatalf("DocumentHighlight: %v", err)
	}
	if len(highlights) != 0 {
		t.Errorf("expected no highlights on a literal, got %d", len(highlights))
	}
}

// highlightLocs extracts lsp.Location values (URI+range) from highlights so
// that refLines (defined in references_test.go) can sort them by start line.
func highlightLocs(hs []lsp.DocumentHighlight) []lsp.Location {
	out := make([]lsp.Location, len(hs))
	for i, h := range hs {
		out[i].Range = h.Range
	}
	return out
}
