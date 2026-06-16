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
	h.mu.Unlock()
	if !ok {
		return nil, nil
	}

	var syms []lsp.DocumentSymbol
	for _, node := range analysis.program.Statements {
		if sym := stmtToSymbol(node); sym != nil {
			syms = append(syms, *sym)
		}
	}
	return syms, nil
}

// stmtToSymbol converts a top-level AST statement to a DocumentSymbol, or
// returns nil for statements that don't contribute to the outline. A
// half-typed declaration (e.g. a lone `let`) collects with an empty Name;
// LSP forbids a falsy symbol name, so such nodes are skipped.
func stmtToSymbol(node ast.AstNode) *lsp.DocumentSymbol {
	switch s := node.(type) {
	case *ast.TypeDeclStmt:
		if s.Name == "" {
			return nil
		}
		kind := typeDeclKind(s.Type)
		loc := s.GetLocation()
		r := astLocToRange(loc)
		return &lsp.DocumentSymbol{
			Name:           s.Name,
			Kind:           kind,
			Range:          r,
			SelectionRange: nameRange(loc, s.Name),
		}

	case *ast.TraitDeclStmt:
		if s.Name == "" {
			return nil
		}
		loc := s.GetLocation()
		r := astLocToRange(loc)
		return &lsp.DocumentSymbol{
			Name:           s.Name,
			Kind:           lsp.SymbolKindInterface,
			Range:          r,
			SelectionRange: nameRange(loc, s.Name),
		}

	case *ast.VarDeclStmt:
		if s.Name == "" {
			return nil
		}
		kind := varDeclSymbolKind(s)
		loc := s.GetLocation()
		r := astLocToRange(loc)
		return &lsp.DocumentSymbol{
			Name:           s.Name,
			Kind:           kind,
			Range:          r,
			SelectionRange: nameRange(loc, s.Name),
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

// astLocToRange converts a 1-based ast.Location to a 0-based lsp.Range.
func astLocToRange(loc ast.Location) lsp.Range {
	return lsp.Range{
		Start: lsp.Position{Line: lspPos(loc.StartLine), Character: lspPos(loc.StartCol)},
		End:   lsp.Position{Line: lspPos(loc.EndLine), Character: lspPos(loc.EndCol)},
	}
}

// nameRange returns a single-line range covering just the symbol name, starting
// at the declaration's StartLine/StartCol. SelectionRange should highlight only
// the name token, not the whole declaration.
func nameRange(loc ast.Location, name string) lsp.Range {
	start := lsp.Position{Line: lspPos(loc.StartLine), Character: lspPos(loc.StartCol)}
	end := lsp.Position{Line: lspPos(loc.StartLine), Character: lspPos(loc.StartCol) + len(name)}
	return lsp.Range{Start: start, End: end}
}
