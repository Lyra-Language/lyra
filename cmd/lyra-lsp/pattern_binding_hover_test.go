package main

import (
	"testing"

	"github.com/owenrumney/go-lsp/servertest"
)

// Hover and definition on a **pattern binding's own name**, in every form a pattern binds a
// name, must answer as they do from a use of it.
//
// From a use both were right. From the binding, hover in a `match` arm answered with the
// enclosing expression's type (`i64` on `rr` in `rr @ Rect(_, _)`), and in a `let` pattern
// answered nothing; definition answered nothing for a rest, a whole payload and a struct
// shorthand, which have no pattern node of the name's own.
func TestPatternBindingHoverAndDefinition(t *testing.T) {
	const head = "\ndata Shape = Rect(i64, i64) | Dot\nstruct Pt { x: i64, y: i64 }\nlet f = (s: Shape, p: Pt, xs: []i64) -> i64 => {\n"
	cases := []struct {
		name, body, id, hover string
	}{
		{"arm", "  match s { Rect(ww, h) => ww + h, Dot => 0 }\n}", "ww", "ww: i64"},
		{"tuple rest", "  match s { Rect(...wh) => wh.0, Dot => 0 }\n}", "wh", "wh: AnonymousTuple(i64, i64)"},
		{"whole payload", "  match s { Rect wh => wh.0, Dot => 0 }\n}", "wh", "wh: AnonymousTuple(i64, i64)"},
		{"at binding", "  match s { rr @ Rect(_, _) => area(rr), Dot => 0 }\n}\nlet area = (s: Shape) -> i64 => 1", "rr", "rr: Shape"},
		{"array rest", "  match xs { [a, ...more] => more.len() + a, _ => 0 }\n}", "more", "more: DynamicArray<i64>"},
		{"destructuring let", "  let (aa, b) = (1, 2)\n  aa + b\n}", "aa", "aa: i64"},
		{"struct field", "  let Pt { x: xx, y } = p\n  xx + y\n}", "xx", "xx: i64"},
		{"struct shorthand", "  let Pt { x, y } = p\n  x + y\n}", "x", "x: i64"},
		{"if let", "  if let Rect(ww, _) = s { return ww }\n  0\n}", "ww", "ww: i64"},
		{"let else", "  let Rect(ww, _) = s else { return 0 }\n  ww\n}", "ww", "ww: i64"},
		{"tuple parameter", "  0\n}\nlet g = ((aa, b): (i64, i64)) -> i64 => aa + b", "aa", "aa: i64"},
		{"struct shorthand parameter", "  0\n}\nlet g = (Pt { x, y }: Pt) -> i64 => x + y", "x", "x: i64"},
	}
	for _, c := range cases {
		src := head + c.body
		binding, use, _ := occurrences(src, c.id)
		for _, from := range []struct {
			what string
			pos  [2]int
		}{{"binding", binding}, {"use", use}} {
			h := servertest.New(t, newHandler())
			openAndWait(t, h, src)

			hv, err := h.Hover(testURI, from.pos[0], from.pos[1])
			if err != nil || hv == nil {
				t.Errorf("%s from the %s: no hover (%v)", c.name, from.what, err)
			} else if want := "```lyra\n" + c.hover + "\n```"; hv.Contents.Value != want {
				t.Errorf("%s from the %s: hover %q, want %q", c.name, from.what, hv.Contents.Value, want)
			}

			defs, err := h.Definition(testURI, from.pos[0], from.pos[1])
			if err != nil || len(defs) != 1 {
				t.Errorf("%s from the %s: want one definition, got %v (%v)", c.name, from.what, defs, err)
				continue
			}
			start, end := defs[0].Range.Start, defs[0].Range.End
			if int(start.Line) != binding[0] || int(start.Character) != binding[1] ||
				int(end.Line) != binding[0] || int(end.Character) != binding[1]+len(c.id) {
				t.Errorf("%s from the %s: definition %v, want the binding at %v", c.name, from.what, defs[0].Range, binding)
			}
		}
	}
}
