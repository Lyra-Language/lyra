package main

import (
	"sort"
	"strings"
	"testing"

	"github.com/owenrumney/go-lsp/lsp"
	"github.com/owenrumney/go-lsp/servertest"

	"github.com/Lyra-Language/lyra/pkg/driver"
)

// Rename and references on a **pattern binding**, from the binding and from a use, in every
// form a pattern binds a name.
//
// Two failures, both found here. From the binding nothing answered, since every lookup began
// at an expression. From a use the declaration's edit covered the wrong span, and the source
// came out corrupted: the whole `let (a, b) = …` statement, all of `rr @ Rect(_, _)`, or
// `...more` without its dots. So each case applies the edits and requires the result to
// analyze cleanly with the name replaced.
func TestPatternBindingRename(t *testing.T) {
	const head = "\ndata Shape = Rect(i64, i64) | Dot\nstruct Pt { x: i64, y: i64 }\nlet f = (s: Shape, p: Pt, xs: []i64) -> i64 => {\n"
	cases := []struct {
		name, body, id, want string
	}{
		{"arm", "  match s { Rect(ww, h) => ww + h, Dot => 0 }\n}",
			"ww", "  match s { Rect(zz, h) => zz + h, Dot => 0 }"},
		{"tuple rest", "  match s { Rect(...wh) => wh.0, Dot => 0 }\n}",
			"wh", "  match s { Rect(...zz) => zz.0, Dot => 0 }"},
		{"whole payload", "  match s { Rect wh => wh.0, Dot => 0 }\n}",
			"wh", "  match s { Rect zz => zz.0, Dot => 0 }"},
		{"at binding", "  match s { rr @ Rect(_, _) => area(rr), Dot => 0 }\n}\nlet area = (s: Shape) -> i64 => 1",
			"rr", "  match s { zz @ Rect(_, _) => area(zz), Dot => 0 }"},
		{"array rest", "  match xs { [a, ...more] => more.len() + a, _ => 0 }\n}",
			"more", "  match xs { [a, ...zz] => zz.len() + a, _ => 0 }"},
		{"destructuring let", "  let (aa, b) = (1, 2)\n  aa + b\n}",
			"aa", "  let (zz, b) = (1, 2)"},
		{"struct field", "  let Pt { x: xx, y } = p\n  xx + y\n}",
			"xx", "  let Pt { x: zz, y } = p"},
		{"struct shorthand", "  let Pt { x, y } = p\n  x + y\n}",
			"x", "  let Pt { x: zz, y } = p"},
		{"if let", "  if let Rect(ww, _) = s { return ww }\n  0\n}",
			"ww", "  if let Rect(zz, _) = s { return zz }"},
		{"let else", "  let Rect(ww, _) = s else { return 0 }\n  ww\n}",
			"ww", "  let Rect(zz, _) = s else { return 0 }"},
	}
	for _, c := range cases {
		src := head + c.body
		if res := driver.Analyze([]byte(src)); res.HasErrors() {
			t.Fatalf("%s: the source itself should analyze cleanly: %v", c.name, res.Errors())
		}
		binding, use, uses := occurrences(src, c.id)
		for _, from := range []struct {
			what string
			pos  [2]int
		}{{"binding", binding}, {"use", use}} {
			h := servertest.New(t, newHandler())
			openAndWait(t, h, src)

			locs, err := h.References(testURI, from.pos[0], from.pos[1], true)
			if err != nil {
				t.Fatalf("%s from the %s: References: %v", c.name, from.what, err)
			}
			if len(locs) != uses {
				t.Errorf("%s from the %s: want %d references (declaration included), got %v", c.name, from.what, uses, locs)
			}

			we, err := h.Rename(testURI, from.pos[0], from.pos[1], "zz")
			if err != nil || we == nil {
				t.Errorf("%s from the %s: no rename (%v)", c.name, from.what, err)
				continue
			}
			out := applyEdits(src, we.Changes[testURI])
			if !strings.Contains(out, c.want) {
				t.Errorf("%s from the %s: want a line %q in\n%s", c.name, from.what, c.want, out)
				continue
			}
			if res := driver.Analyze([]byte(out)); res.HasErrors() {
				t.Errorf("%s from the %s: the renamed source does not analyze: %v\n%s", c.name, from.what, res.Errors(), out)
			}
		}
	}
}

// occurrences finds id as a whole word below the head: the first (the binding), the last (a
// use), and how many there are.
func occurrences(src, id string) (first, last [2]int, n int) {
	for i, line := range strings.Split(src, "\n") {
		if i < 4 {
			continue
		}
		for j := 0; j+len(id) <= len(line); j++ {
			if line[j:j+len(id)] != id || (j > 0 && isWordByte(line[j-1])) || (j+len(id) < len(line) && isWordByte(line[j+len(id)])) {
				continue
			}
			if n == 0 {
				first = [2]int{i, j}
			}
			last = [2]int{i, j}
			n++
		}
	}
	return first, last, n
}

func isWordByte(b byte) bool {
	return b == '_' || b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= '0' && b <= '9'
}

// applyEdits applies single-line edits to an ASCII source, last first so earlier ranges hold.
func applyEdits(src string, edits []lsp.TextEdit) string {
	lines := strings.Split(src, "\n")
	sort.Slice(edits, func(i, j int) bool {
		a, b := edits[i].Range.Start, edits[j].Range.Start
		return a.Line > b.Line || a.Line == b.Line && a.Character > b.Character
	})
	for _, e := range edits {
		l := lines[e.Range.Start.Line]
		lines[e.Range.Start.Line] = l[:e.Range.Start.Character] + e.NewText + l[e.Range.End.Character:]
	}
	return strings.Join(lines, "\n")
}
