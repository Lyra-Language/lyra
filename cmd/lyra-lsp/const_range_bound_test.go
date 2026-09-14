package main

import (
	"strings"
	"testing"

	"github.com/owenrumney/go-lsp/servertest"
)

// A `const` range-pattern bound is a use of the const, for every position feature. The
// typechecker folds the bound to a literal, so the name survives only in
// RangePattern.ConstBounds, and a feature walking expressions never saw it: renaming the
// const left `LOW..<=HIGH` naming a const that no longer existed.
//
//	0: ""
//	1: "const LOW = 10"
//	2: "let f = (n: i64) -> i64 => match n {"
//	3: "  LOW..<=20 => 1,"
//	4: "  _ => LOW,"
//	5: "}"
const constBoundSrc = `
const LOW = 10
let f = (n: i64) -> i64 => match n {
  LOW..<=20 => 1,
  _ => LOW,
}`

func TestConstRangeBound_Definition(t *testing.T) {
	h := servertest.New(t, newHandler())
	openAndWait(t, h, constBoundSrc)
	locs, err := h.Definition(testURI, 3, 3)
	if err != nil {
		t.Fatalf("Definition: %v", err)
	}
	if len(locs) != 1 || locs[0].Range.Start.Line != 1 {
		t.Fatalf("want the const on line 1, got %v", locs)
	}
}

func TestConstRangeBound_Hover(t *testing.T) {
	h := servertest.New(t, newHandler())
	openAndWait(t, h, constBoundSrc)
	hover, err := h.Hover(testURI, 3, 3)
	if err != nil {
		t.Fatalf("Hover: %v", err)
	}
	if hover == nil || !strings.Contains(hover.Contents.Value, "LOW") {
		t.Fatalf("want a hover naming LOW, got %+v", hover)
	}
}

func TestConstRangeBound_References(t *testing.T) {
	h := servertest.New(t, newHandler())
	openAndWait(t, h, constBoundSrc)
	for _, pos := range [][2]int{{3, 3}, {4, 8}} {
		locs, err := h.References(testURI, pos[0], pos[1], false)
		if err != nil {
			t.Fatalf("References: %v", err)
		}
		if got := refLines(locs); len(got) != 2 || got[0] != 3 || got[1] != 4 {
			t.Errorf("from %v: want uses on lines 3 and 4, got %v", pos, got)
		}
	}
}

func TestConstRangeBound_Rename(t *testing.T) {
	h := servertest.New(t, newHandler())
	openAndWait(t, h, constBoundSrc)
	we, err := h.Rename(testURI, 4, 8, "renamed")
	if err != nil {
		t.Fatalf("Rename: %v", err)
	}
	assertEditLines(t, we, []int{1, 3, 4})
	for _, e := range we.Changes[testURI] {
		if e.Range.Start.Line == 3 && (e.Range.Start.Character != 2 || e.Range.End.Character != 5) {
			t.Errorf("the bound's edit should cover LOW exactly, got %+v", e.Range)
		}
	}
}

func TestConstRangeBound_Highlight(t *testing.T) {
	h := servertest.New(t, newHandler())
	openAndWait(t, h, constBoundSrc)
	hl, err := h.DocumentHighlight(testURI, 3, 3)
	if err != nil {
		t.Fatalf("DocumentHighlight: %v", err)
	}
	lines := map[int]bool{}
	for _, x := range hl {
		lines[x.Range.Start.Line] = true
	}
	if !lines[3] || !lines[4] {
		t.Errorf("want highlights on lines 3 and 4, got %v", hl)
	}
}
