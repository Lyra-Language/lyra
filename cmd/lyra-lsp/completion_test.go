package main

import (
	"testing"

	"github.com/owenrumney/go-lsp/lsp"
	"github.com/owenrumney/go-lsp/servertest"
)

// completionLabels returns the labels of the items as a set for easy assertion.
func completionLabels(list *lsp.CompletionList) map[string]lsp.CompletionItemKind {
	out := map[string]lsp.CompletionItemKind{}
	if list == nil {
		return out
	}
	for _, item := range list.Items {
		var kind lsp.CompletionItemKind
		if item.Kind != nil {
			kind = *item.Kind
		}
		out[item.Label] = kind
	}
	return out
}

func TestCompletion_IdentifiersInScope(t *testing.T) {
	h := servertest.New(t, newHandler())
	// Two top-level bindings; completion at the start of a blank line 3 should
	// surface both names in scope.
	src := `
	let alpha = 1
	let beta = 2
`
	openAndWait(t, h, src)
	list, err := h.Completion(testURI, 3, 0)
	if err != nil {
		t.Fatalf("Completion: %v", err)
	}
	got := completionLabels(list)
	for _, name := range []string{"alpha", "beta"} {
		if _, ok := got[name]; !ok {
			t.Errorf("expected %q in completions, got %v", name, got)
		}
	}
}

func TestCompletion_FunctionAndTypeKinds(t *testing.T) {
	h := servertest.New(t, newHandler())
	src := `
	struct Point {
		x: i64,
		y: i64,
	}
	let f = (n: i32) -> i32 => n
`
	openAndWait(t, h, src)
	list, err := h.Completion(testURI, 6, 0)
	if err != nil {
		t.Fatalf("Completion: %v", err)
	}
	got := completionLabels(list)
	if got["f"] != lsp.CompletionItemKindFunction {
		t.Errorf("expected f to be Function kind, got %v", got["f"])
	}
	if got["Point"] != lsp.CompletionItemKindStruct {
		t.Errorf("expected Point to be Struct kind, got %v", got["Point"])
	}
}

func TestCompletion_StructFieldsAfterDot(t *testing.T) {
	h := servertest.New(t, newHandler())
	// `p.` on the final line should complete with Point's fields.
	src := `
	struct Point {
		x: i64,
		y: i64,
	}
	let p = Point { x: 1, y: 2 }
	p.`
	openAndWait(t, h, src)
	// "p." is on line 6; cursor after the dot (col 3).
	list, err := h.Completion(testURI, 6, 3)
	if err != nil {
		t.Fatalf("Completion: %v", err)
	}
	got := completionLabels(list)
	for _, name := range []string{"x", "y"} {
		if got[name] != lsp.CompletionItemKindField {
			t.Errorf("expected field %q (Field kind), got %v in %v", name, got[name], got)
		}
	}
	// Scope identifiers must NOT leak into member completion.
	if _, ok := got["p"]; ok {
		t.Errorf("member completion should not include scope names, got %v", got)
	}
}

func TestCompletion_NestedStructFieldChain(t *testing.T) {
	h := servertest.New(t, newHandler())
	src := `
	struct Point {
		x: i64,
		y: i64,
	}
	struct Line {
		start: Point,
		end: Point,
	}
	let l = Line { start: Point { x: 0, y: 0 }, end: Point { x: 1, y: 1 } }
	l.start.`
	openAndWait(t, h, src)
	// "l.start." is on line 10; cursor after the trailing dot (col 9).
	list, err := h.Completion(testURI, 10, 9)
	if err != nil {
		t.Fatalf("Completion: %v", err)
	}
	got := completionLabels(list)
	for _, name := range []string{"x", "y"} {
		if got[name] != lsp.CompletionItemKindField {
			t.Errorf("expected nested field %q, got %v in %v", name, got[name], got)
		}
	}
}
