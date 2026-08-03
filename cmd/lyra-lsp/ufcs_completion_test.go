package main

import (
	"testing"

	"github.com/owenrumney/go-lsp/lsp"
	"github.com/owenrumney/go-lsp/servertest"
)

// Completion after `.` offers the free functions callable on that receiver, alongside its
// struct fields. Whether a candidate qualifies is asked of typechecker.UFCSCallable, so a
// suggestion here is a call that will compile — the list cannot drift from the rule.

func TestCompletion_OffersUFCSMethods(t *testing.T) {
	h := servertest.New(t, newHandler())
	src := `
let scaled = pure (self: i64, by: i64) -> i64 => self * by
let doubled = pure (self: i64) -> i64 => self * 2
let unrelated = pure (n: i64) -> i64 => n
let x = 21
let y = x.
`
	openAndWait(t, h, src)
	// The caret sits just after the `.` on the `let y = x.` line (0-based line 5;
	// the source begins with a newline, so line 0 is empty).
	list, err := h.Completion(testURI, 5, 10)
	if err != nil {
		t.Fatalf("Completion: %v", err)
	}
	got := completionLabels(list)
	for _, name := range []string{"scaled", "doubled"} {
		if kind, ok := got[name]; !ok {
			t.Errorf("expected %q among the receiver's methods, got %v", name, got)
		} else if kind != lsp.CompletionItemKindMethod {
			t.Errorf("%q should complete as a method, got kind %v", name, kind)
		}
	}
	// `unrelated` never opted in, so calling it method-style would not compile.
	if _, ok := got["unrelated"]; ok {
		t.Errorf("a function without a `self` parameter is not callable method-style: %v", got)
	}
}

// A receiver whose type does not fit the `self` parameter offers nothing, for the same
// reason: the call would be rejected.
func TestCompletion_UFCSFiltersByReceiverType(t *testing.T) {
	h := servertest.New(t, newHandler())
	src := `
struct Buf { n: i64 }
let widen = pure (self: Buf, by: i64) -> i64 => self.n + by
let s = "text"
let y = s.
`
	openAndWait(t, h, src)
	list, err := h.Completion(testURI, 4, 10)
	if err != nil {
		t.Fatalf("Completion: %v", err)
	}
	if got := completionLabels(list); len(got) != 0 {
		t.Errorf("a string is not a Buf, so `widen` must not be offered; got %v", got)
	}
}

// Fields and methods coexist: a struct receiver keeps its fields and gains its methods.
func TestCompletion_UFCSAlongsideStructFields(t *testing.T) {
	h := servertest.New(t, newHandler())
	src := `
struct Buf { n: i64 }
let widen = pure (self: Buf, by: i64) -> i64 => self.n + by
let b = Buf { n: 1 }
let y = b.
`
	openAndWait(t, h, src)
	list, err := h.Completion(testURI, 4, 10)
	if err != nil {
		t.Fatalf("Completion: %v", err)
	}
	got := completionLabels(list)
	if kind, ok := got["n"]; !ok || kind != lsp.CompletionItemKindField {
		t.Errorf("expected the field `n`, got %v", got)
	}
	if kind, ok := got["widen"]; !ok || kind != lsp.CompletionItemKindMethod {
		t.Errorf("expected the method `widen`, got %v", got)
	}
}
