package main

import (
	"sort"
	"testing"

	"github.com/owenrumney/go-lsp/lsp"
	"github.com/owenrumney/go-lsp/servertest"
)

// refLines returns the sorted 0-based start lines of the reference locations.
func refLines(locs []lsp.Location) []int {
	lines := make([]int, len(locs))
	for i, l := range locs {
		lines[i] = l.Range.Start.Line
	}
	sort.Ints(lines)
	return lines
}

func TestReferences_LocalVariable(t *testing.T) {
	h := servertest.New(t, newHandler())
	// "let x = 5" (line 0), "let y = x" (line 1), "let z = x" (line 2).
	openAndWait(t, h, "let x = 5\nlet y = x\nlet z = x")
	// Cursor on "x" in "let y = x" (line 1, col 8).
	locs, err := h.References(testURI, 1, 8, false)
	if err != nil {
		t.Fatalf("References: %v", err)
	}
	// Two usages on lines 1 and 2; declaration excluded.
	got := refLines(locs)
	want := []int{1, 2}
	if len(got) != len(want) {
		t.Fatalf("expected %d references, got %d (%v)", len(want), len(got), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("reference %d: expected line %d, got %d", i, want[i], got[i])
		}
	}
}

func TestReferences_IncludeDeclaration(t *testing.T) {
	h := servertest.New(t, newHandler())
	openAndWait(t, h, "let x = 5\nlet y = x")
	// includeDecl=true should add the declaration (line 0) to the two usages.
	locs, err := h.References(testURI, 1, 8, true)
	if err != nil {
		t.Fatalf("References: %v", err)
	}
	got := refLines(locs)
	want := []int{0, 1} // declaration on line 0, usage on line 1
	if len(got) != len(want) {
		t.Fatalf("expected %d references, got %d (%v)", len(want), len(got), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("reference %d: expected line %d, got %d", i, want[i], got[i])
		}
	}
}

func TestReferences_ShadowedNameExcluded(t *testing.T) {
	h := servertest.New(t, newHandler())
	// Top-level "x" used on line 1; a lambda on lines 2-4 shadows "x" with its
	// own binding and uses it. The inner usage must NOT be reported for the outer.
	src := "let x = 1\nlet a = x\nlet f = () -> i32 => {\n  let x = 2\n  x\n}"
	openAndWait(t, h, src)
	// Cursor on outer "x" usage in "let a = x" (line 1, col 8).
	locs, err := h.References(testURI, 1, 8, false)
	if err != nil {
		t.Fatalf("References: %v", err)
	}
	got := refLines(locs)
	// Only the outer usage on line 1; the shadowed inner "x" (line 4) excluded.
	if len(got) != 1 || got[0] != 1 {
		t.Fatalf("expected only line 1, got %v", got)
	}
}

func TestReferences_FromInnerShadow(t *testing.T) {
	h := servertest.New(t, newHandler())
	// Same source; this time the cursor is on the inner shadowing "x" (line 4).
	src := "let x = 1\nlet a = x\nlet f = () -> i32 => {\n  let x = 2\n  x\n}"
	openAndWait(t, h, src)
	locs, err := h.References(testURI, 4, 2, false)
	if err != nil {
		t.Fatalf("References: %v", err)
	}
	got := refLines(locs)
	// Only the inner usage on line 4; the outer "x" usages excluded.
	if len(got) != 1 || got[0] != 4 {
		t.Fatalf("expected only line 4, got %v", got)
	}
}

func TestReferences_Parameter(t *testing.T) {
	h := servertest.New(t, newHandler())
	// Parameter "n" used twice in the body.
	src := "let f = (n: i32) -> i32 => {\n  let m = n\n  n\n}"
	openAndWait(t, h, src)
	// Cursor on "n" in "let m = n" (line 1, col 10).
	locs, err := h.References(testURI, 1, 10, false)
	if err != nil {
		t.Fatalf("References: %v", err)
	}
	got := refLines(locs)
	want := []int{1, 2}
	if len(got) != len(want) {
		t.Fatalf("expected %d references, got %d (%v)", len(want), len(got), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("reference %d: expected line %d, got %d", i, want[i], got[i])
		}
	}
}

func TestReferences_NoResultOnLiteral(t *testing.T) {
	h := servertest.New(t, newHandler())
	openAndWait(t, h, "let x = 42")
	// Cursor on the literal "42" — empty result, no error.
	locs, err := h.References(testURI, 0, 8, true)
	if err != nil {
		t.Fatalf("References: %v", err)
	}
	if len(locs) != 0 {
		t.Errorf("expected no references on a literal, got %d", len(locs))
	}
}
