package main

import (
	"strings"
	"testing"

	"github.com/owenrumney/go-lsp/servertest"
)

func TestHover_ShowsDocCommentBelowTheType(t *testing.T) {
	h := servertest.New(t, newHandler())
	src := "/// Doubles its argument.\n" +
		"///\n" +
		"/// # Panics\n" +
		"///\n" +
		"/// Traps if the result overflows.\n" +
		"let twice = (n: i64) -> i64 => n * 2\n" +
		"let r = twice(21)\n"
	openAndWait(t, h, src)

	// `twice` in the call on the last line, 0-based line 6, col 8.
	hover, err := h.Hover(testURI, 6, 8)
	if err != nil {
		t.Fatalf("Hover: %v", err)
	}
	if hover == nil {
		t.Fatal("expected a hover result")
	}
	got := hover.Contents.Value
	if !strings.Contains(got, "Doubles its argument.") {
		t.Errorf("hover is missing the doc comment: %q", got)
	}
	if !strings.Contains(got, "# Panics") {
		t.Errorf("hover is missing the Panics section: %q", got)
	}
	// The type must come first: hover is read at a glance, and a long doc above
	// the signature pushes the signature out of a fixed-height popup.
	if sig, doc := strings.Index(got, "```"), strings.Index(got, "Doubles"); sig > doc {
		t.Errorf("the doc comment precedes the signature block: %q", got)
	}
}

func TestHover_UndocumentedSymbolIsUnchanged(t *testing.T) {
	h := servertest.New(t, newHandler())
	src := "let twice = (n: i64) -> i64 => n * 2\nlet r = twice(21)\n"
	openAndWait(t, h, src)

	hover, err := h.Hover(testURI, 1, 8)
	if err != nil {
		t.Fatalf("Hover: %v", err)
	}
	if hover == nil {
		t.Fatal("expected a hover result")
	}
	// No doc means no separator rule — an empty documentation section would be
	// worse than none.
	if strings.Contains(hover.Contents.Value, "---") {
		t.Errorf("an undocumented symbol got a documentation separator: %q", hover.Contents.Value)
	}
}

func TestHover_StructFieldShowsItsDoc(t *testing.T) {
	h := servertest.New(t, newHandler())
	src := "struct Point {\n" +
		"  /// Distance along the x axis.\n" +
		"  x: f64,\n" +
		"}\n" +
		"let p = Point { x: 1.0 }\n" +
		"let v = p.x\n"
	openAndWait(t, h, src)

	// `x` in `p.x` on 0-based line 5, col 10.
	hover, err := h.Hover(testURI, 5, 10)
	if err != nil {
		t.Fatalf("Hover: %v", err)
	}
	if hover == nil {
		t.Fatal("expected a hover result")
	}
	if !strings.Contains(hover.Contents.Value, "Distance along the x axis.") {
		t.Errorf("field hover is missing its doc: %q", hover.Contents.Value)
	}
}
