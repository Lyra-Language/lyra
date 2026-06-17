package main

import (
	"testing"

	"github.com/owenrumney/go-lsp/servertest"
)

func TestDefinition_LocalVariable(t *testing.T) {
	h := servertest.New(t, newHandler())
	src := `
	let x = 5
	let y = x`
	openAndWait(t, h, src)
	// "x" in "let y = x" is at line 2 (0-based), col 9 (0-based).
	locs, err := h.Definition(testURI, 2, 9)
	if err != nil {
		t.Fatalf("Definition: %v", err)
	}
	if len(locs) != 1 {
		t.Fatalf("expected 1 location, got %d", len(locs))
	}
	// "let x = 5" is on line 1 (0-based).
	if locs[0].Range.Start.Line != 1 {
		t.Errorf("expected definition on line 1, got line %d", locs[0].Range.Start.Line)
	}
}

func TestDefinition_StructTypeName(t *testing.T) {
	h := servertest.New(t, newHandler())
	src := `
	struct Point {
		x: i32,
		y: i32,
	}
	var origin = Point {
		x: 0,
		y: 0,
	}`
	openAndWait(t, h, src)
	// "Point" in "var origin = Point {" starts at line 5, col 14 (0-based).
	locs, err := h.Definition(testURI, 5, 14)
	if err != nil {
		t.Fatalf("Definition: %v", err)
	}
	if len(locs) != 1 {
		t.Fatalf("expected 1 location for struct type name, got %d", len(locs))
	}
	// struct declaration is on line 1.
	if locs[0].Range.Start.Line != 1 {
		t.Errorf("expected definition on line 1, got line %d", locs[0].Range.Start.Line)
	}
}

func TestDefinition_NoResult_OnLiteral(t *testing.T) {
	h := servertest.New(t, newHandler())
	openAndWait(t, h, "let x = 42")
	// cursor on the literal "42" — should return empty, not an error
	locs, err := h.Definition(testURI, 0, 8)
	if err != nil {
		t.Fatalf("Definition: %v", err)
	}
	if len(locs) != 0 {
		t.Errorf("expected no locations for literal, got %d", len(locs))
	}
}

func TestDefinition_NestedScope(t *testing.T) {
	h := servertest.New(t, newHandler())
	src := `
	let f = () -> i32 => {
		let n = 10
		n
	}`
	openAndWait(t, h, src)
	// "n" on line 3 (0-based), col 2.
	locs, err := h.Definition(testURI, 3, 2)
	if err != nil {
		t.Fatalf("Definition: %v", err)
	}
	if len(locs) != 1 {
		t.Fatalf("expected 1 location, got %d", len(locs))
	}
	// "let n = 10" is on line 2 (0-based).
	if locs[0].Range.Start.Line != 2 {
		t.Errorf("expected definition on line 2, got line %d", locs[0].Range.Start.Line)
	}
}

func TestDefinition_OuterScopeFromInner(t *testing.T) {
	h := servertest.New(t, newHandler())
	src := `
	let x = 99
	let f = () -> i32 => {
		x
	}`
	openAndWait(t, h, src)
	// "x" on line 3 (0-based), col 2.
	locs, err := h.Definition(testURI, 3, 2)
	if err != nil {
		t.Fatalf("Definition: %v", err)
	}
	if len(locs) != 1 {
		t.Fatalf("expected 1 location, got %d", len(locs))
	}
	// "let x = 99" is on line 1.
	if locs[0].Range.Start.Line != 1 {
		t.Errorf("expected definition on line 1, got line %d", locs[0].Range.Start.Line)
	}
}
