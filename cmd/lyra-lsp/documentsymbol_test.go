package main

import (
	"testing"

	"github.com/owenrumney/go-lsp/lsp"
	"github.com/owenrumney/go-lsp/servertest"
)

func symbolNames(syms []lsp.DocumentSymbol) []string {
	names := make([]string, len(syms))
	for i, s := range syms {
		names[i] = s.Name
	}
	return names
}

func symbolKinds(syms []lsp.DocumentSymbol) []lsp.SymbolKind {
	kinds := make([]lsp.SymbolKind, len(syms))
	for i, s := range syms {
		kinds[i] = s.Kind
	}
	return kinds
}

func TestDocumentSymbol_TypeDecl_Struct(t *testing.T) {
	h := servertest.New(t, newHandler())
	openAndWait(t, h, "struct Point { x: int, y: int }")
	syms, err := h.DocumentSymbol(testURI)
	if err != nil {
		t.Fatalf("DocumentSymbol: %v", err)
	}
	if len(syms) != 1 {
		t.Fatalf("expected 1 symbol, got %d: %v", len(syms), symbolNames(syms))
	}
	if syms[0].Name != "Point" {
		t.Errorf("expected name 'Point', got %q", syms[0].Name)
	}
	if syms[0].Kind != lsp.SymbolKindStruct {
		t.Errorf("expected SymbolKindStruct (%d), got %d", lsp.SymbolKindStruct, syms[0].Kind)
	}
}

func TestDocumentSymbol_TypeDecl_Data(t *testing.T) {
	h := servertest.New(t, newHandler())
	openAndWait(t, h, "data Option<t> = None | Some t")
	syms, err := h.DocumentSymbol(testURI)
	if err != nil {
		t.Fatalf("DocumentSymbol: %v", err)
	}
	if len(syms) != 1 {
		t.Fatalf("expected 1 symbol, got %d: %v", len(syms), symbolNames(syms))
	}
	if syms[0].Kind != lsp.SymbolKindEnum {
		t.Errorf("expected SymbolKindEnum (%d), got %d", lsp.SymbolKindEnum, syms[0].Kind)
	}
}

func TestDocumentSymbol_TraitDecl(t *testing.T) {
	h := servertest.New(t, newHandler())
	openAndWait(t, h, "trait Show {\n  show: (Self) -> string\n}")
	syms, err := h.DocumentSymbol(testURI)
	if err != nil {
		t.Fatalf("DocumentSymbol: %v", err)
	}
	if len(syms) != 1 {
		t.Fatalf("expected 1 symbol, got %d: %v", len(syms), symbolNames(syms))
	}
	if syms[0].Name != "Show" {
		t.Errorf("expected name 'Show', got %q", syms[0].Name)
	}
	if syms[0].Kind != lsp.SymbolKindInterface {
		t.Errorf("expected SymbolKindInterface (%d), got %d", lsp.SymbolKindInterface, syms[0].Kind)
	}
}

func TestDocumentSymbol_FunctionDecl(t *testing.T) {
	h := servertest.New(t, newHandler())
	openAndWait(t, h, "let add = (a: int, b: int) -> int => a + b")
	syms, err := h.DocumentSymbol(testURI)
	if err != nil {
		t.Fatalf("DocumentSymbol: %v", err)
	}
	if len(syms) != 1 {
		t.Fatalf("expected 1 symbol, got %d: %v", len(syms), symbolNames(syms))
	}
	if syms[0].Name != "add" {
		t.Errorf("expected name 'add', got %q", syms[0].Name)
	}
	if syms[0].Kind != lsp.SymbolKindFunction {
		t.Errorf("expected SymbolKindFunction (%d), got %d", lsp.SymbolKindFunction, syms[0].Kind)
	}
}

func TestDocumentSymbol_ConstDecl(t *testing.T) {
	h := servertest.New(t, newHandler())
	openAndWait(t, h, "const MAX: int = 100")
	syms, err := h.DocumentSymbol(testURI)
	if err != nil {
		t.Fatalf("DocumentSymbol: %v", err)
	}
	if len(syms) != 1 {
		t.Fatalf("expected 1 symbol, got %d: %v", len(syms), symbolNames(syms))
	}
	if syms[0].Kind != lsp.SymbolKindConstant {
		t.Errorf("expected SymbolKindConstant (%d), got %d", lsp.SymbolKindConstant, syms[0].Kind)
	}
}

func TestDocumentSymbol_VarDecl(t *testing.T) {
	h := servertest.New(t, newHandler())
	openAndWait(t, h, "var count: int = 0")
	syms, err := h.DocumentSymbol(testURI)
	if err != nil {
		t.Fatalf("DocumentSymbol: %v", err)
	}
	if len(syms) != 1 {
		t.Fatalf("expected 1 symbol, got %d: %v", len(syms), symbolNames(syms))
	}
	if syms[0].Kind != lsp.SymbolKindVariable {
		t.Errorf("expected SymbolKindVariable (%d), got %d", lsp.SymbolKindVariable, syms[0].Kind)
	}
}

func TestDocumentSymbol_Mixed(t *testing.T) {
	src := "struct Color { r: int, g: int, b: int }\n" +
		"trait Render {\n  render: (Self) -> string\n}\n" +
		"let makeColor = (r: int, g: int, b: int) -> Color => Color { r: r, g: g, b: b }\n" +
		"const BLACK: Color = Color { r: 0, g: 0, b: 0 }"
	h := servertest.New(t, newHandler())
	openAndWait(t, h, src)
	syms, err := h.DocumentSymbol(testURI)
	if err != nil {
		t.Fatalf("DocumentSymbol: %v", err)
	}
	if len(syms) != 4 {
		t.Fatalf("expected 4 symbols, got %d: %v", len(syms), symbolNames(syms))
	}
	kinds := symbolKinds(syms)
	want := []lsp.SymbolKind{
		lsp.SymbolKindStruct,
		lsp.SymbolKindInterface,
		lsp.SymbolKindFunction,
		lsp.SymbolKindConstant,
	}
	for i, k := range want {
		if kinds[i] != k {
			t.Errorf("symbol[%d] %q: expected kind %d, got %d", i, syms[i].Name, k, kinds[i])
		}
	}
}

func TestDocumentSymbol_Empty(t *testing.T) {
	h := servertest.New(t, newHandler())
	openAndWait(t, h, "")
	syms, err := h.DocumentSymbol(testURI)
	if err != nil {
		t.Fatalf("DocumentSymbol: %v", err)
	}
	if len(syms) != 0 {
		t.Errorf("expected 0 symbols for empty document, got %d", len(syms))
	}
}
