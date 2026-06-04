package main

import (
	"testing"

	"github.com/owenrumney/go-lsp/servertest"
)

func TestDefinition_LocalVariable(t *testing.T) {
	h := servertest.New(t, newHandler())
	// "let x = 5" on line 0, "let y = x" on line 1; cursor on "x" in "let y = x"
	openAndWait(t, h, "let x = 5\nlet y = x")
	// "x" in "let y = x" is at line 1 (0-based), col 8 (0-based)
	locs, err := h.Definition(testURI, 1, 8)
	if err != nil {
		t.Fatalf("Definition: %v", err)
	}
	if len(locs) != 1 {
		t.Fatalf("expected 1 location, got %d", len(locs))
	}
	// "let x = 5" is on line 0 (0-based)
	if locs[0].Range.Start.Line != 0 {
		t.Errorf("expected definition on line 0, got line %d", locs[0].Range.Start.Line)
	}
}

func TestDefinition_StructTypeName(t *testing.T) {
	h := servertest.New(t, newHandler())
	// "Point" struct defined on line 0, used as struct literal on line 1.
	src := "struct Point {\n  x: i32,\n  y: i32,\n}\nvar origin = Point {\n  x: 0,\n  y: 0,\n}"
	openAndWait(t, h, src)
	// "Point" in "var origin = Point {" starts at line 4, col 13 (0-based)
	locs, err := h.Definition(testURI, 4, 13)
	if err != nil {
		t.Fatalf("Definition: %v", err)
	}
	if len(locs) != 1 {
		t.Fatalf("expected 1 location for struct type name, got %d", len(locs))
	}
	// struct declaration is on line 0
	if locs[0].Range.Start.Line != 0 {
		t.Errorf("expected definition on line 0, got line %d", locs[0].Range.Start.Line)
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
	// lambda with inner variable; cursor on inner use of "n"
	src := "let f = () -> i32 => {\n  let n = 10\n  n\n}"
	openAndWait(t, h, src)
	// "n" on line 2 (0-based), col 2
	locs, err := h.Definition(testURI, 2, 2)
	if err != nil {
		t.Fatalf("Definition: %v", err)
	}
	if len(locs) != 1 {
		t.Fatalf("expected 1 location, got %d", len(locs))
	}
	// "let n = 10" is on line 1 (0-based)
	if locs[0].Range.Start.Line != 1 {
		t.Errorf("expected definition on line 1, got line %d", locs[0].Range.Start.Line)
	}
}

func TestDefinition_OuterScopeFromInner(t *testing.T) {
	h := servertest.New(t, newHandler())
	// outer variable referenced inside a lambda
	src := "let x = 99\nlet f = () -> i32 => {\n  x\n}"
	openAndWait(t, h, src)
	// "x" on line 2 (0-based), col 2
	locs, err := h.Definition(testURI, 2, 2)
	if err != nil {
		t.Fatalf("Definition: %v", err)
	}
	if len(locs) != 1 {
		t.Fatalf("expected 1 location, got %d", len(locs))
	}
	// "let x = 99" is on line 0
	if locs[0].Range.Start.Line != 0 {
		t.Errorf("expected definition on line 0, got line %d", locs[0].Range.Start.Line)
	}
}
