package main

import (
	"testing"

	"github.com/owenrumney/go-lsp/servertest"
)

func TestFoldingRange_Struct(t *testing.T) {
	h := servertest.New(t, newHandler())
	src := `struct Point {
	x: i64,
	y: i64
}`
	openAndWait(t, h, src)
	ranges, err := h.FoldingRange(testURI)
	if err != nil {
		t.Fatalf("FoldingRange: %v", err)
	}
	if len(ranges) != 1 {
		t.Fatalf("expected 1 fold, got %d: %v", len(ranges), ranges)
	}
	if ranges[0].StartLine != 0 || ranges[0].EndLine != 3 {
		t.Errorf("expected fold [0,3], got [%d,%d]", ranges[0].StartLine, ranges[0].EndLine)
	}
}

func TestFoldingRange_DataType(t *testing.T) {
	h := servertest.New(t, newHandler())
	src := `data Color =
	Red |
	Green |
	Blue`
	openAndWait(t, h, src)
	ranges, err := h.FoldingRange(testURI)
	if err != nil {
		t.Fatalf("FoldingRange: %v", err)
	}
	if len(ranges) != 1 {
		t.Fatalf("expected 1 fold, got %d: %v", len(ranges), ranges)
	}
	if ranges[0].StartLine != 0 || ranges[0].EndLine != 3 {
		t.Errorf("expected fold [0,3], got [%d,%d]", ranges[0].StartLine, ranges[0].EndLine)
	}
}

func TestFoldingRange_Trait(t *testing.T) {
	h := servertest.New(t, newHandler())
	src := `trait Show {
	show: (Self) -> string
}`
	openAndWait(t, h, src)
	ranges, err := h.FoldingRange(testURI)
	if err != nil {
		t.Fatalf("FoldingRange: %v", err)
	}
	if len(ranges) != 1 {
		t.Fatalf("expected 1 fold, got %d: %v", len(ranges), ranges)
	}
	if ranges[0].StartLine != 0 || ranges[0].EndLine != 2 {
		t.Errorf("expected fold [0,2], got [%d,%d]", ranges[0].StartLine, ranges[0].EndLine)
	}
}

func TestFoldingRange_MatchExpr(t *testing.T) {
	h := servertest.New(t, newHandler())
	src := `data Color = Red | Green | Blue
let describe = (c: Color) -> string => match c {
	Red => "red",
	Green => "green",
	Blue => "blue"
}`
	openAndWait(t, h, src)
	ranges, err := h.FoldingRange(testURI)
	if err != nil {
		t.Fatalf("FoldingRange: %v", err)
	}
	// Expect a fold for the data decl (single line — no fold), the lambda block, and the match.
	hasMatch := false
	for _, r := range ranges {
		if r.StartLine == 1 && r.EndLine == 5 {
			hasMatch = true
		}
	}
	if !hasMatch {
		t.Errorf("expected a match fold on lines [1,5], got: %v", ranges)
	}
}

func TestFoldingRange_BlockExpr(t *testing.T) {
	h := servertest.New(t, newHandler())
	src := `let add = (a: i64, b: i64) -> i64 => {
	let result = a + b
	result
}`
	openAndWait(t, h, src)
	ranges, err := h.FoldingRange(testURI)
	if err != nil {
		t.Fatalf("FoldingRange: %v", err)
	}
	// Expect a fold for the block body.
	hasBlock := false
	for _, r := range ranges {
		if r.StartLine == 0 && r.EndLine == 3 {
			hasBlock = true
		}
	}
	if !hasBlock {
		t.Errorf("expected a block fold on lines [0,3], got: %v", ranges)
	}
}

func TestFoldingRange_SingleLine_NoFold(t *testing.T) {
	h := servertest.New(t, newHandler())
	openAndWait(t, h, "struct Point { x: i64, y: i64 }")
	ranges, err := h.FoldingRange(testURI)
	if err != nil {
		t.Fatalf("FoldingRange: %v", err)
	}
	if len(ranges) != 0 {
		t.Errorf("expected no folds for single-line source, got %d: %v", len(ranges), ranges)
	}
}

func TestFoldingRange_Empty(t *testing.T) {
	h := servertest.New(t, newHandler())
	openAndWait(t, h, "")
	ranges, err := h.FoldingRange(testURI)
	if err != nil {
		t.Fatalf("FoldingRange: %v", err)
	}
	if len(ranges) != 0 {
		t.Errorf("expected 0 folds for empty document, got %d", len(ranges))
	}
}
