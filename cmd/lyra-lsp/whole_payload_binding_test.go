package main

import (
	"strings"
	"testing"

	"github.com/owenrumney/go-lsp/servertest"
)

// `Rect pair` is rewritten to `Rect(...pair)` by the typechecker; the editor still treats
// `pair` as the binding written there.
//
//	0: ""
//	1: "data Shape = Rect(i64, i64) | Dot"
//	2: "let f = (s: Shape) -> i64 => match s {"
//	3: "  Rect pair => pair.0 + pair.1,"
//	4: "  Dot => 0,"
//	5: "}"
const wholePayloadSrc = `
data Shape = Rect(i64, i64) | Dot
let f = (s: Shape) -> i64 => match s {
  Rect pair => pair.0 + pair.1,
  Dot => 0,
}`

func TestWholePayloadBinding_HoverIsTheTuple(t *testing.T) {
	h := servertest.New(t, newHandler())
	openAndWait(t, h, wholePayloadSrc)
	hover, err := h.Hover(testURI, 3, 16)
	if err != nil {
		t.Fatalf("Hover: %v", err)
	}
	if hover == nil || !strings.Contains(hover.Contents.Value, "(i64, i64)") {
		t.Fatalf("want the tuple type, got %+v", hover)
	}
}

// References from a use find the other use. (From the binding itself nothing answers yet,
// as for any rest binding — todo.md.)
func TestWholePayloadBinding_ReferencesFromAUse(t *testing.T) {
	h := servertest.New(t, newHandler())
	openAndWait(t, h, wholePayloadSrc)
	locs, err := h.References(testURI, 3, 16, false)
	if err != nil {
		t.Fatalf("References: %v", err)
	}
	if len(locs) != 2 {
		t.Fatalf("want both uses of pair, got %+v", locs)
	}
}
