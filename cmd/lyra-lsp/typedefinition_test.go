package main

import (
	"strings"
	"testing"

	"github.com/owenrumney/go-lsp/servertest"
)

// Go to Type Definition is the one of the three "Go to …" items that asks a different
// question. Definition and Declaration answer "where does this name come from"; this answers
// "what is this thing" — on a value, the declaration of its type.

func TestTypeDefinition_FromAUseAndFromTheDeclaration(t *testing.T) {
	h := servertest.New(t, newHandler())
	src := `
struct Point { x: i64, y: i64 }
let main = () -> void => {
	let p = Point { x: 1, y: 2 }
	println(p.x)
}`
	openAndWait(t, h, src)
	for _, tc := range []struct {
		name   string
		line   int
		needle string
	}{
		{"at the binding's name", 3, "p = Point"},
		{"at a use", 4, "p.x"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			col := strings.Index(strings.Split(src, "\n")[tc.line], tc.needle)
			locs, err := h.TypeDefinition(testURI, tc.line, col)
			if err != nil {
				t.Fatalf("TypeDefinition: %v", err)
			}
			if len(locs) != 1 {
				t.Fatalf("expected 1 location, got %d", len(locs))
			}
			// `struct Point` is on line 1 — the *type*, not the binding.
			if got := int(locs[0].Range.Start.Line); got != 1 {
				t.Errorf("jumped to line %d; want 1 (the type declaration)", got)
			}
		})
	}
}

// On a constructor it is the data type the constructor belongs to — `Box` answers `Shape`,
// which is a different answer from Definition only in what it means, not in where it lands.
func TestTypeDefinition_OnAConstructorPattern(t *testing.T) {
	h := servertest.New(t, newHandler())
	src := `
data Shape = Dot | Box(i64)
let area = pure (s: Shape) -> i64 => match s {
	Dot => 0,
	Box(n) => n,
}`
	openAndWait(t, h, src)
	col := strings.Index(strings.Split(src, "\n")[4], "Box(n)")
	locs, err := h.TypeDefinition(testURI, 4, col)
	if err != nil {
		t.Fatalf("TypeDefinition: %v", err)
	}
	if len(locs) != 1 || locs[0].Range.Start.Line != 1 {
		t.Errorf("got %v; want one location on line 1 (data Shape)", locs)
	}
}

// **A structural type has no declaration to jump to**, and answering nothing is the honest
// result — not the nearest enclosing something. `i64`, a tuple, an array and a function are
// all types a program never declared.
func TestTypeDefinition_AStructuralTypeAnswersNothing(t *testing.T) {
	h := servertest.New(t, newHandler())
	src := `
let main = () -> void => {
	let n = 42
	let pair = (1, 2)
	println(n + pair.0)
}`
	openAndWait(t, h, src)
	for _, tc := range []struct {
		name   string
		line   int
		needle string
	}{
		{"an integer", 2, "n = 42"},
		{"a tuple", 3, "pair = ("},
	} {
		t.Run(tc.name, func(t *testing.T) {
			col := strings.Index(strings.Split(src, "\n")[tc.line], tc.needle)
			locs, err := h.TypeDefinition(testURI, tc.line, col)
			if err != nil {
				t.Fatalf("TypeDefinition: %v", err)
			}
			if len(locs) != 0 {
				t.Errorf("got %v; want nothing — the type is structural", locs)
			}
		})
	}
}
