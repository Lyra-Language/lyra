package main

import (
	"sort"
	"strings"
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
	src := `
	let x = 5
	let y = x
	let z = x`
	openAndWait(t, h, src)
	// Cursor on "x" in "let y = x" (line 2, col 9).
	locs, err := h.References(testURI, 2, 9, false)
	if err != nil {
		t.Fatalf("References: %v", err)
	}
	// Two usages on lines 2 and 3; declaration excluded.
	got := refLines(locs)
	want := []int{2, 3}
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
	src := `
	let x = 5
	let y = x`
	openAndWait(t, h, src)
	// includeDecl=true should add the declaration (line 1) to the usage on line 2.
	locs, err := h.References(testURI, 2, 9, true)
	if err != nil {
		t.Fatalf("References: %v", err)
	}
	got := refLines(locs)
	want := []int{1, 2} // declaration on line 1, usage on line 2
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
	// Top-level "x" used on line 2; a lambda on lines 3-6 shadows "x" with its
	// own binding and uses it. The inner usage must NOT be reported for the outer.
	src := `
	let x = 1
	let a = x
	let f = () -> i32 => {
		let x = 2
		x
	}`
	openAndWait(t, h, src)
	// Cursor on outer "x" usage in "let a = x" (line 2, col 9).
	locs, err := h.References(testURI, 2, 9, false)
	if err != nil {
		t.Fatalf("References: %v", err)
	}
	got := refLines(locs)
	// Only the outer usage on line 2; the shadowed inner "x" (line 5) excluded.
	if len(got) != 1 || got[0] != 2 {
		t.Fatalf("expected only line 2, got %v", got)
	}
}

func TestReferences_FromInnerShadow(t *testing.T) {
	h := servertest.New(t, newHandler())
	// Same source; this time the cursor is on the inner shadowing "x" (line 5).
	src := `
	let x = 1
	let a = x
	let f = () -> i32 => {
		let x = 2
		x
	}`
	openAndWait(t, h, src)
	locs, err := h.References(testURI, 5, 2, false)
	if err != nil {
		t.Fatalf("References: %v", err)
	}
	got := refLines(locs)
	// Only the inner usage on line 5; the outer "x" usages excluded.
	if len(got) != 1 || got[0] != 5 {
		t.Fatalf("expected only line 5, got %v", got)
	}
}

func TestReferences_Parameter(t *testing.T) {
	h := servertest.New(t, newHandler())
	// Parameter "n" used twice in the body.
	src := `
	let f = (n: i32) -> i32 => {
		let m = n
		n
	}`
	openAndWait(t, h, src)
	// Cursor on "n" in "let m = n" (line 2, col 10).
	locs, err := h.References(testURI, 2, 10, false)
	if err != nil {
		t.Fatalf("References: %v", err)
	}
	got := refLines(locs)
	want := []int{2, 3}
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

// **A constructor's uses span both halves of the AST**, and reporting one half would be
// read as complete: `Box` appears in `match s { Box(n) => … }` as a pattern and in
// `Box(1)` as a value. Patterns were invisible to references until 08/22 — the same gap
// definition and hover had — so a constructor answered with nothing at all.
func TestReferences_ConstructorInPatternsAndExpressions(t *testing.T) {
	h := servertest.New(t, newHandler())
	src := `
data Shape = Dot | Box(i64)
let area = pure (s: Shape) -> i64 => match s {
	Dot => 0,
	Box(n) => n,
}
let mk = pure (n: i64) -> Shape => Box(n)
let two = pure () -> Shape => Box(2)`
	openAndWait(t, h, src)
	col := strings.Index(strings.Split(src, "\n")[4], "Box(n)")
	locs, err := h.References(testURI, 4, col, true)
	if err != nil {
		t.Fatalf("References: %v", err)
	}
	// The pattern on line 4, the two constructions on lines 6 and 7, and the declaration.
	if len(locs) != 4 {
		t.Fatalf("got %d references, want 4 (one pattern, two values, the declaration): %v", len(locs), locs)
	}
	lines := map[int]bool{}
	for _, l := range locs {
		lines[int(l.Range.Start.Line)] = true
	}
	for _, want := range []int{1, 4, 6, 7} {
		if !lines[want] {
			t.Errorf("missing the occurrence on line %d; got %v", want, locs)
		}
	}
}

// **A `match` arm registers nothing**, so a name it binds is invisible to every scope-based
// question — and references declines rather than answering with the binding alone, which a
// reader would take as "this is never used". Pinned so that fixing the collector (todo.md)
// is noticed here: when arms gain scopes this should start answering, and this test should
// be rewritten to assert the uses rather than their absence.
func TestReferences_AMatchArmBindingIsNotYetResolvable(t *testing.T) {
	h := servertest.New(t, newHandler())
	src := `
data Shape = Dot | Box(i64)
let area = pure (s: Shape) -> i64 => match s {
	Dot => 0,
	Box(n) => n + n,
}`
	openAndWait(t, h, src)
	col := strings.Index(strings.Split(src, "\n")[4], "n)")
	locs, err := h.References(testURI, 4, col, true)
	if err != nil {
		t.Fatalf("References: %v", err)
	}
	if len(locs) != 0 {
		t.Errorf("a match-arm binding has no scope entry, so references must decline "+
			"rather than answer partially; got %v", locs)
	}
}
