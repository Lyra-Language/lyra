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
	// Two top-level bindings; completion at the start of a third line should
	// surface both names in scope.
	openAndWait(t, h, "let alpha = 1\nlet beta = 2\n")
	list, err := h.Completion(testURI, 2, 0)
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
	src := "struct Point {\n\tx: i64,\n\ty: i64,\n}\nlet f = (n: i32) -> i32 => n\n"
	openAndWait(t, h, src)
	list, err := h.Completion(testURI, 5, 0)
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
	src := "struct Point {\n\tx: i64,\n\ty: i64,\n}\nlet p = Point { x: 1, y: 2 }\np."
	openAndWait(t, h, src)
	list, err := h.Completion(testURI, 5, 2)
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
	src := "struct Point {\n\tx: i64,\n\ty: i64,\n}\n" +
		"struct Line {\n\tstart: Point,\n\tend: Point,\n}\n" +
		"let l = Line { start: Point { x: 0, y: 0 }, end: Point { x: 1, y: 1 } }\n" +
		"l.start."
	openAndWait(t, h, src)
	// "l.start." is on line 9; cursor after the trailing dot (col 8).
	list, err := h.Completion(testURI, 9, 8)
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
