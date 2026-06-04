package main

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/owenrumney/go-lsp/servertest"
)

const testURI = "file:///test.lyra"

// openAndWait opens a document and waits for the analysis to complete.
func openAndWait(t *testing.T, h *servertest.Harness, source string) {
	t.Helper()
	if err := h.DidOpen(testURI, "lyra", source); err != nil {
		t.Fatalf("DidOpen: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := h.WaitForDiagnostics(ctx, testURI); err != nil {
		t.Fatalf("WaitForDiagnostics: %v", err)
	}
}

func TestHover_IdentifierExpr_ShowsType(t *testing.T) {
	h := servertest.New(t, newHandler())
	openAndWait(t, h, "let xs: [3]int = [1, 2, 3]\nlet y = xs")
	// "xs" in "let y = xs" starts at line 1 (0-based), col 8 (0-based)
	hover, err := h.Hover(testURI, 1, 8)
	if err != nil {
		t.Fatalf("Hover: %v", err)
	}
	if hover == nil {
		t.Fatal("expected hover result, got nil")
	}
	if !strings.Contains(hover.Contents.Value, "xs") {
		t.Errorf("expected hover to mention 'xs', got: %q", hover.Contents.Value)
	}
	if !strings.Contains(hover.Contents.Value, "StaticArray") {
		t.Errorf("expected hover to mention type, got: %q", hover.Contents.Value)
	}
}

func TestHover_IndexExpr_ShowsElementType(t *testing.T) {
	h := servertest.New(t, newHandler())
	openAndWait(t, h, "let xs: [3]int = [1, 2, 3]\nlet y = xs[0]")
	// "xs[0]" starts at line 1, col 8; hover on the whole expression
	hover, err := h.Hover(testURI, 1, 9)
	if err != nil {
		t.Fatalf("Hover: %v", err)
	}
	if hover == nil {
		t.Fatal("expected hover result, got nil")
	}
	if !strings.Contains(hover.Contents.Value, "int") {
		t.Errorf("expected hover to show int type, got: %q", hover.Contents.Value)
	}
}

func TestHover_NoResult_ForBlankPosition(t *testing.T) {
	h := servertest.New(t, newHandler())
	openAndWait(t, h, "let x: int = 42")
	// Line 5 doesn't exist — should return nil without error.
	hover, err := h.Hover(testURI, 5, 0)
	if err != nil {
		t.Fatalf("Hover: %v", err)
	}
	if hover != nil {
		t.Errorf("expected nil hover for out-of-range position, got: %+v", hover)
	}
}

func TestHover_ClosedDoc_ReturnsNil(t *testing.T) {
	h := servertest.New(t, newHandler())
	// Hover on a URI that was never opened — should return nil gracefully.
	hover, err := h.Hover("file:///never-opened.lyra", 0, 0)
	if err != nil {
		t.Fatalf("Hover: %v", err)
	}
	if hover != nil {
		t.Errorf("expected nil hover for unknown URI, got: %+v", hover)
	}
}
