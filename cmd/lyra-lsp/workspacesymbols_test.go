package main

import (
	"slices"
	"testing"

	"github.com/owenrumney/go-lsp/lsp"
	"github.com/owenrumney/go-lsp/servertest"
)

func symNames(syms []lsp.SymbolInformation) []string {
	names := make([]string, len(syms))
	for i, s := range syms {
		names[i] = s.Name
	}
	return names
}

func TestWorkspaceSymbol_EmptyQuery_ReturnsAll(t *testing.T) {
	h := servertest.New(t, newHandler())
	src := `
struct Point { x: i64, y: i64 }
trait Show { show: (Self) -> string }
let add = (a: i64, b: i64) -> i64 => a + b
const MAX: i64 = 100`
	openAndWait(t, h, src)

	syms, err := h.WorkspaceSymbol("")
	if err != nil {
		t.Fatalf("WorkspaceSymbol: %v", err)
	}
	names := symNames(syms)
	for _, want := range []string{"Point", "Show", "add", "MAX"} {
		if !slices.Contains(names, want) {
			t.Errorf("expected %q in results %v", want, names)
		}
	}
}

func TestWorkspaceSymbol_ExactMatch(t *testing.T) {
	h := servertest.New(t, newHandler())
	src := `
struct Player { health: i64 }
struct Enemy { damage: i64 }
let update = (p: Player) -> Player => p`
	openAndWait(t, h, src)

	syms, err := h.WorkspaceSymbol("Player")
	if err != nil {
		t.Fatalf("WorkspaceSymbol: %v", err)
	}
	names := symNames(syms)
	if !slices.Contains(names, "Player") {
		t.Errorf("expected Player in %v", names)
	}
	if slices.Contains(names, "Enemy") {
		t.Errorf("Enemy should not appear for query 'Player', got %v", names)
	}
}

func TestWorkspaceSymbol_FuzzyMatch_Subsequence(t *testing.T) {
	h := servertest.New(t, newHandler())
	src := `
struct PlayerHealth { value: i64 }
let parseAndValidate = (x: i64) -> i64 => x`
	openAndWait(t, h, src)

	// "ph" is a subsequence of "PlayerHealth" (lowercase: p→p, h→h)
	syms, err := h.WorkspaceSymbol("ph")
	if err != nil {
		t.Fatalf("WorkspaceSymbol: %v", err)
	}
	names := symNames(syms)
	if !slices.Contains(names, "PlayerHealth") {
		t.Errorf("expected PlayerHealth for fuzzy query 'ph', got %v", names)
	}
}

func TestWorkspaceSymbol_CaseInsensitive(t *testing.T) {
	h := servertest.New(t, newHandler())
	openAndWait(t, h, "struct Viewport { w: i64, h: i64 }")

	syms, err := h.WorkspaceSymbol("viewport")
	if err != nil {
		t.Fatalf("WorkspaceSymbol: %v", err)
	}
	names := symNames(syms)
	if !slices.Contains(names, "Viewport") {
		t.Errorf("expected Viewport for lowercase query, got %v", names)
	}
}

func TestWorkspaceSymbol_SymbolKinds(t *testing.T) {
	h := servertest.New(t, newHandler())
	src := `
struct Vec2 { x: i64, y: i64 }
data Color = Red | Green | Blue
trait Drawable { draw: (Self) -> string }
let normalize = (v: Vec2) -> Vec2 => v
const PI: f64 = 3`
	openAndWait(t, h, src)

	syms, err := h.WorkspaceSymbol("")
	if err != nil {
		t.Fatalf("WorkspaceSymbol: %v", err)
	}
	kinds := make(map[string]lsp.SymbolKind)
	for _, s := range syms {
		kinds[s.Name] = s.Kind
	}
	tests := []struct {
		name string
		want lsp.SymbolKind
	}{
		{"Vec2", lsp.SymbolKindStruct},
		{"Color", lsp.SymbolKindEnum},
		{"Drawable", lsp.SymbolKindInterface},
		{"normalize", lsp.SymbolKindFunction},
		{"PI", lsp.SymbolKindConstant},
	}
	for _, tt := range tests {
		if got, ok := kinds[tt.name]; !ok {
			t.Errorf("symbol %q not found in results", tt.name)
		} else if got != tt.want {
			t.Errorf("symbol %q: expected kind %d, got %d", tt.name, tt.want, got)
		}
	}
}

func TestWorkspaceSymbol_PlainVarExcluded(t *testing.T) {
	h := servertest.New(t, newHandler())
	// Plain var bindings should not appear (too noisy); functions and types should.
	src := `
var counter: i64 = 0
let increment = (x: i64) -> i64 => x`
	openAndWait(t, h, src)

	syms, err := h.WorkspaceSymbol("")
	if err != nil {
		t.Fatalf("WorkspaceSymbol: %v", err)
	}
	names := symNames(syms)
	if slices.Contains(names, "counter") {
		t.Errorf("plain var 'counter' should be excluded from workspace symbols, got %v", names)
	}
	if !slices.Contains(names, "increment") {
		t.Errorf("function 'increment' should be included, got %v", names)
	}
}

func TestWorkspaceSymbol_NoMatch(t *testing.T) {
	h := servertest.New(t, newHandler())
	openAndWait(t, h, "struct Foo { x: i64 }")

	syms, err := h.WorkspaceSymbol("zzz")
	if err != nil {
		t.Fatalf("WorkspaceSymbol: %v", err)
	}
	if len(syms) != 0 {
		t.Errorf("expected no results for 'zzz', got %v", symNames(syms))
	}
}
