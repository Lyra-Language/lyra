package main

import (
	"strings"
	"testing"

	"github.com/owenrumney/go-lsp/servertest"
)

// The LSP under per-module type identity.
//
// SymbolTable.Types is keyed by declKey, so a *private* type is filed under
// `<module>::<name>`. Several features read the map by bare name, which stops finding
// such a type and — for the type-name completion list, which used the map key as the
// label — would offer the raw key `one::Point`, not a name anyone can type. Those reads
// now go through LookupTypeFrom, the choke point pkg/ast/symbols/README.md already
// requires.
//
// **What these tests can and cannot catch.** The server analyses one document at a time
// through driver.Analyze, which builds a modules.Unit with no Path — so the collector
// sees module "" whatever the source says, and a qualified key never arises here today.
// A `module one` line in the fixtures below is therefore inert, and reverting the fix
// still passes: these pin the observable behaviour (the features work; no key reaches a
// label) rather than proving the keyed lookup. They become load-bearing the moment the
// server resolves a real import graph, which is exactly when a silent bare-name read
// would start returning another module's declaration.

func TestTypeIdentity_TypeNamesCompleteAndNoKeyLeaks(t *testing.T) {
	h := servertest.New(t, newHandler())
	src := `module one
	struct Point {
		x: i64,
		y: i64,
	}
	let mk = () -> i64 => 1
`
	openAndWait(t, h, src)

	list, err := h.Completion(testURI, 6, 0)
	if err != nil {
		t.Fatalf("Completion: %v", err)
	}
	got := completionLabels(list)
	if _, ok := got["Point"]; !ok {
		t.Errorf("expected Point in completions, got %v", got)
	}
	// The guard that survives regardless of how the label is derived: a symbol-table key
	// is not a source name, and must never be offered as one.
	for label := range got {
		if strings.Contains(label, "::") {
			t.Errorf("completion offered a raw symbol-table key %q, not a source name", label)
		}
	}
}

func TestTypeIdentity_StructFieldsCompleteThroughTheKeyedLookup(t *testing.T) {
	h := servertest.New(t, newHandler())
	src := `module one
	struct Point {
		x: i64,
		y: i64,
	}
	let p = Point { x: 1, y: 2 }
	p.`
	openAndWait(t, h, src)

	// "p." is on line 6; cursor after the dot (col 3) — as TestCompletion_StructFieldsAfterDot.
	list, err := h.Completion(testURI, 6, 3)
	if err != nil {
		t.Fatalf("Completion: %v", err)
	}
	got := completionLabels(list)
	for _, field := range []string{"x", "y"} {
		if _, ok := got[field]; !ok {
			t.Errorf("expected field %q, got %v", field, got)
		}
	}
}
