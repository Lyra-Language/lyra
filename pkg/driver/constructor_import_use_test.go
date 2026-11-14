package driver_test

import (
	"path/filepath"
	"testing"

	diag "github.com/Lyra-Language/lyra/pkg/diagnostic"
)

const toolShapes = `module shapes
pub data Tool = Pen(i64) | Eraser
pub data Cursor = Arrow | Hand
pub let pick = () -> Tool => Pen(1)
`

// w004In is the lyra-W004 messages main.lyra draws, and fails on any error.
func w004In(t *testing.T, files map[string]string) []string {
	t.Helper()
	res := analyzeModules(t, files)
	for _, d := range res.Errors() {
		t.Errorf("unexpected error: %s", d.Message)
	}
	var out []string
	for _, d := range res.Diagnostics {
		if d.Code == diag.CodeUnusedImport && filepath.Base(d.Location.File) == "main.lyra" {
			out = append(out, d.Message)
		}
	}
	return out
}

// An import listing only a data type admits its constructors, so a file using them uses the
// import, though the type's name is written nowhere — a payload constructor, a nullary one,
// and one in a pattern. lyra-W004 called it unused until 11/13 (Termina), and removing it left
// `Pen` undefined.
func TestUnusedImport_ATypeUsedByItsConstructorsIsUsed(t *testing.T) {
	for name, body := range map[string]string{
		"a payload constructor": `let main = () -> void => println("${match Pen(3) { Pen(n) => n, Eraser => 0 }}")`,
		"a nullary constructor": `let main = () -> void => { let e = Eraser
  match e { Pen(_) => println("pen"), Eraser => println("eraser") } }`,
	} {
		t.Run(name, func(t *testing.T) {
			got := w004In(t, map[string]string{
				"shapes.lyra": toolShapes,
				"main.lyra":   "module main\nimport shapes.{ Tool }\n" + body + "\n",
			})
			if len(got) != 0 {
				t.Errorf("want no lyra-W004, got %v", got)
			}
		})
	}
}

// A type whose constructors this file never uses is still unused — here beside one that is,
// and where only another file uses them, since the count is the file's own.
func TestUnusedImport_ATypeWhoseConstructorsGoUnusedIsStillUnused(t *testing.T) {
	got := w004In(t, map[string]string{
		"shapes.lyra": toolShapes,
		"main.lyra": `module main
import shapes.{ Tool, Cursor }
import other
let main = () -> void => println("${match Pen(3) { Pen(n) => n, Eraser => 0 }} ${other.hand()}")
`,
		"other.lyra": `module other
import shapes.{ Cursor }
pub let hand = () -> string => match Hand { Arrow => "arrow", Hand => "hand" }
`,
	})
	if len(got) != 1 || got[0] != `imported name "Cursor" is never used` {
		t.Errorf("want one lyra-W004 for Cursor, got %v", got)
	}
}
