package main

import (
	"strings"
	"testing"

	"github.com/owenrumney/go-lsp/servertest"
)

// A constructor written as a function value is a lambda the driver made (pkg/driver's
// eraseConstructorValues), whose parameters and body sit at the constructor's own span —
// the narrowest nodes at every column of `Width`, so without care the editor answers about
// `__Width0`, a parameter nobody wrote. Hover names the constructor's function type, and
// definition never lands on that parameter (a binding's definition is itself, which
// would put the cursor back where it was); it reaches the `data` line, as for `Quit` or
// `Width(4)` (constructor_navigation_test.go).
func TestConstructorValue_EditorAnswersAboutTheConstructor(t *testing.T) {
	h := servertest.New(t, newHandler())
	src := `
data Ev = Width(i64) | Quit
let apply = pure (f: (i64) -> Ev) -> Ev => f(3)
let main = () -> void => {
	let e = apply(Width)
}`
	openAndWait(t, h, src)
	col := strings.Index(strings.Split(src, "\n")[4], "Width")

	hover, err := h.Hover(testURI, 4, col+2)
	if err != nil {
		t.Fatalf("Hover: %v", err)
	}
	if hover == nil || !strings.Contains(hover.Contents.Value, "(i64) -> Ev") || strings.Contains(hover.Contents.Value, "__") {
		t.Errorf("hover = %v; want the constructor's function type, and no invented name", hover)
	}

	locs, err := h.Definition(testURI, 4, col+2)
	if err != nil {
		t.Fatalf("Definition: %v", err)
	}
	for _, l := range locs {
		if l.Range.Start.Line == 4 {
			t.Errorf("definition = %v; it answered with the lambda's own parameter", locs)
		}
	}
}
