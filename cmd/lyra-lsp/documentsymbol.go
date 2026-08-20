package main

import (
	"context"
	"log"
	"runtime/debug"

	"github.com/owenrumney/go-lsp/lsp"

	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/types"
)

// DocumentSymbol implements textDocument/documentSymbol, returning the symbol
// outline (type decls, functions, constants) for the breadcrumb/outline view.
func (h *Handler) DocumentSymbol(_ context.Context, params *lsp.DocumentSymbolParams) (result []lsp.DocumentSymbol, retErr error) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("documentSymbol panic: %v\n%s", r, debug.Stack())
			result, retErr = nil, nil
		}
	}()

	uri := string(params.TextDocument.URI)
	h.mu.Lock()
	analysis, ok := h.analysisStore[uri]
	source := h.docStore[uri]
	h.mu.Unlock()
	if !ok {
		return nil, nil
	}

	var syms []lsp.DocumentSymbol
	for _, node := range analysis.program.Statements {
		if sym := stmtToSymbol(source, node); sym != nil {
			syms = append(syms, *sym)
		}
	}
	return syms, nil
}

// stmtToSymbol converts a top-level AST statement to a DocumentSymbol, or
// returns nil for statements that don't contribute to the outline. A
// half-typed declaration (e.g. a lone `let`) collects with an empty Name;
// LSP forbids a falsy symbol name, so such nodes are skipped.
func stmtToSymbol(source string, node ast.AstNode) *lsp.DocumentSymbol {
	switch s := node.(type) {
	case *ast.TypeDeclStmt:
		if s.Name == "" {
			return nil
		}
		kind := typeDeclKind(s.Type)
		loc := s.GetLocation()
		return &lsp.DocumentSymbol{
			Name:           s.Name,
			Kind:           kind,
			Range:          locToRange(source, loc),
			SelectionRange: nameRange(source, loc, s.Name),
		}

	case *ast.TraitDeclStmt:
		if s.Name == "" {
			return nil
		}
		loc := s.GetLocation()
		return &lsp.DocumentSymbol{
			Name:           s.Name,
			Kind:           lsp.SymbolKindInterface,
			Range:          locToRange(source, loc),
			SelectionRange: nameRange(source, loc, s.Name),
		}

	case *ast.VarDeclStmt:
		if s.Name == "" {
			return nil
		}
		kind := varDeclSymbolKind(s)
		loc := s.GetLocation()
		return &lsp.DocumentSymbol{
			Name:           s.Name,
			Kind:           kind,
			Range:          locToRange(source, loc),
			SelectionRange: nameRange(source, loc, s.Name),
		}

	case *ast.ExternDeclStmt:
		if s.Name == "" {
			return nil
		}
		// A foreign function is a function in the outline. The selection range comes
		// from **NameLocation** rather than from the declaration's start, which the
		// other three can get away with: an extern's span begins at its `@link` or
		// `unsafe` token, so measuring len(name) from there would highlight the wrong
		// text — and does, if this is written like its neighbours.
		return &lsp.DocumentSymbol{
			Name:           s.Name,
			Kind:           lsp.SymbolKindFunction,
			Range:          locToRange(source, s.GetLocation()),
			SelectionRange: nameRange(source, s.NameLocation, s.Name),
		}
	}
	return nil
}

// typeDeclKind maps a type declaration's underlying type to an LSP SymbolKind.
func typeDeclKind(t types.Type) lsp.SymbolKind {
	switch t.(type) {
	case types.NamedStructType, types.AnonymousStructType:
		return lsp.SymbolKindStruct
	case types.DataType:
		return lsp.SymbolKindEnum
	default:
		return lsp.SymbolKindClass
	}
}

// varDeclSymbolKind returns Function for lambda-valued bindings, Constant for
// const declarations, and Variable for everything else.
func varDeclSymbolKind(s *ast.VarDeclStmt) lsp.SymbolKind {
	if s.BindingKind == ast.BindingConst {
		return lsp.SymbolKindConstant
	}
	if _, isLambda := s.Value.(*ast.LambdaExpr); isLambda {
		return lsp.SymbolKindFunction
	}
	return lsp.SymbolKindVariable
}

// nameRange returns a single-line range covering just the symbol name, starting
// at the declaration's StartLine/StartCol. SelectionRange should highlight only
// the name token, not the whole declaration. The name span is measured in bytes
// (len(name)) then converted to UTF-16 columns via source.
func nameRange(source string, loc ast.Location, name string) lsp.Range {
	startByteCol := lspPos(loc.StartCol)
	return lsp.Range{
		Start: lsp.Position{Line: lspPos(loc.StartLine), Character: utf16Column(source, lspPos(loc.StartLine), startByteCol)},
		End:   lsp.Position{Line: lspPos(loc.StartLine), Character: utf16Column(source, lspPos(loc.StartLine), startByteCol+len(name))},
	}
}
