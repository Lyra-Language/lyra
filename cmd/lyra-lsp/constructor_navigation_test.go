package main

import (
	"strings"
	"testing"

	"github.com/owenrumney/go-lsp/servertest"
)

// A constructor in expression position — bare (`Quit`, a DataConstructorExpr), applied
// (`Width(4)`, a TupleLiteralExpr carrying the name) or as a function value (`apply(Width)`,
// the driver's lambda presenting its ConstructorValue) — navigates as the pattern form
// `Width(n) =>` always did: definition reaches the `data` line, and references from any
// spelling list every use of that constructor, patterns and values alike.
func TestConstructorInExpression_DefinitionAndReferences(t *testing.T) {
	h := servertest.New(t, newHandler())
	src := `
data Ev = Width(i64) | Quit
let apply = pure (f: (i64) -> Ev) -> Ev => f(3)
let size = pure (e: Ev) -> i64 => match e {
	Width(n) => n,
	Quit => 0,
}
let main = () -> void => {
	let q = Quit
	let w = Width(4)
	let e = apply(Width)
}`
	openAndWait(t, h, src)
	lines := strings.Split(src, "\n")
	// at is the 0-based position of name at its first column after prefix on line.
	at := func(line int, prefix, name string) [2]int {
		col := strings.Index(lines[line], prefix+name)
		if col < 0 {
			t.Fatalf("%q not on line %d", prefix+name, line)
		}
		return [2]int{line, col + len(prefix)}
	}

	widthPattern, quitPattern := at(4, "", "Width"), at(5, "", "Quit")
	quit, width, widthValue := at(8, "= ", "Quit"), at(9, "= ", "Width"), at(10, "apply(", "Width")
	wantWidth := [][2]int{widthPattern, width, widthValue}
	wantQuit := [][2]int{quitPattern, quit}

	for name, pos := range map[string][2]int{"Quit": quit, "Width(4)": width, "apply(Width)": widthValue} {
		locs, err := h.Definition(testURI, pos[0], pos[1]+1)
		if err != nil {
			t.Fatalf("Definition on %s: %v", name, err)
		}
		if len(locs) != 1 || locs[0].Range.Start.Line != 1 {
			t.Errorf("definition on %s = %v; want the `data Ev` line", name, locs)
		}
	}

	for name, c := range map[string]struct {
		pos  [2]int
		want [][2]int
	}{
		"Quit":         {quit, wantQuit},
		"Width(4)":     {width, wantWidth},
		"apply(Width)": {widthValue, wantWidth},
		"Width(n) =>":  {widthPattern, wantWidth},
		"Quit =>":      {quitPattern, wantQuit},
	} {
		locs, err := h.References(testURI, c.pos[0], c.pos[1]+1, false)
		if err != nil {
			t.Fatalf("References on %s: %v", name, err)
		}
		var got [][2]int
		for _, l := range locs {
			got = append(got, [2]int{int(l.Range.Start.Line), int(l.Range.Start.Character)})
		}
		if !equalPositions(got, c.want) {
			t.Errorf("references from %s = %v; want %v", name, got, c.want)
		}
	}
}

func equalPositions(a, b [][2]int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
