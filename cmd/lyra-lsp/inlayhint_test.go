package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/owenrumney/go-lsp/lsp"
	"github.com/owenrumney/go-lsp/servertest"
)

// fullRange returns an LSP range that covers the entire document.
func fullRange() lsp.Range {
	return lsp.Range{
		Start: lsp.Position{Line: 0, Character: 0},
		End:   lsp.Position{Line: 9999, Character: 0},
	}
}

func hintLabels(hints []lsp.InlayHint) []string {
	labels := make([]string, len(hints))
	for i, h := range hints {
		var s string
		_ = json.Unmarshal(h.Label, &s)
		labels[i] = s
	}
	return labels
}

func TestInlayHint_UnannotatedLet(t *testing.T) {
	h := servertest.New(t, newHandler())
	openAndWait(t, h, "let x = 5")
	hints, err := h.InlayHint(testURI, fullRange())
	if err != nil {
		t.Fatalf("InlayHint: %v", err)
	}
	if len(hints) != 1 {
		t.Fatalf("expected 1 hint, got %d: %v", len(hints), hintLabels(hints))
	}
	label := hintLabels(hints)[0]
	if !strings.HasPrefix(label, ": ") {
		t.Errorf("expected label starting with ': ', got %q", label)
	}
}

func TestInlayHint_AnnotatedLet_NoHint(t *testing.T) {
	h := servertest.New(t, newHandler())
	openAndWait(t, h, "let x: int = 5")
	hints, err := h.InlayHint(testURI, fullRange())
	if err != nil {
		t.Fatalf("InlayHint: %v", err)
	}
	if len(hints) != 0 {
		t.Errorf("expected no hints for annotated binding, got %d: %v", len(hints), hintLabels(hints))
	}
}

func TestInlayHint_MultipleUnannotated(t *testing.T) {
	h := servertest.New(t, newHandler())
	src := `
	let x = 5
	let y = true`
	openAndWait(t, h, src)
	hints, err := h.InlayHint(testURI, fullRange())
	if err != nil {
		t.Fatalf("InlayHint: %v", err)
	}
	if len(hints) != 2 {
		t.Fatalf("expected 2 hints, got %d: %v", len(hints), hintLabels(hints))
	}
}

func TestInlayHint_HintPosition(t *testing.T) {
	h := servertest.New(t, newHandler())
	openAndWait(t, h, "let x = 5")
	hints, err := h.InlayHint(testURI, fullRange())
	if err != nil {
		t.Fatalf("InlayHint: %v", err)
	}
	if len(hints) != 1 {
		t.Fatalf("expected 1 hint, got %d", len(hints))
	}
	// "let x" → position after 'x': line 0, col 5 (0-based: 'l'=0,'e'=1,'t'=2,' '=3,'x'=4, hint at 5)
	if hints[0].Position.Line != 0 || hints[0].Position.Character != 5 {
		t.Errorf("expected hint at (0,5), got (%d,%d)", hints[0].Position.Line, hints[0].Position.Character)
	}
}

func TestInlayHint_NestedInLambda(t *testing.T) {
	h := servertest.New(t, newHandler())
	src := `
	let f = () -> i32 => {
		let n = 42
		n
	}`
	openAndWait(t, h, src)
	hints, err := h.InlayHint(testURI, fullRange())
	if err != nil {
		t.Fatalf("InlayHint: %v", err)
	}
	// Should find hints for unannotated bindings: 'f' (lambda) and 'n' (int)
	if len(hints) == 0 {
		t.Error("expected hints for nested unannotated bindings, got none")
	}
}
