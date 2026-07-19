package main

import (
	"context"
	"log"
	"runtime/debug"
	"strings"

	"github.com/owenrumney/go-lsp/lsp"

	"github.com/Lyra-Language/lyra/pkg/ast"
)

// WorkspaceSymbol implements workspace/symbol, returning SymbolInformation for
// every type declaration and function in all open documents whose name
// fuzzy-matches the query. An empty query returns all symbols.
func (h *Handler) WorkspaceSymbol(_ context.Context, params *lsp.WorkspaceSymbolParams) (result []lsp.SymbolInformation, retErr error) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("workspaceSymbol panic: %v\n%s", r, debug.Stack())
			result, retErr = nil, nil
		}
	}()

	h.mu.Lock()
	snapshot := make(map[string]*docAnalysis, len(h.analysisStore))
	sources := make(map[string]string, len(h.analysisStore))
	for uri, a := range h.analysisStore {
		snapshot[uri] = a
		sources[uri] = h.docStore[uri]
	}
	h.mu.Unlock()

	query := strings.ToLower(params.Query)
	var out []lsp.SymbolInformation
	for uri, analysis := range snapshot {
		for _, node := range analysis.program.Statements {
			info := stmtToSymbolInfo(uri, sources[uri], node)
			if info == nil {
				continue
			}
			if fuzzyMatch(query, strings.ToLower(info.Name)) {
				out = append(out, *info)
			}
		}
	}
	return out, nil
}

// stmtToSymbolInfo converts a top-level statement to a SymbolInformation, or
// returns nil for statements that don't contribute to the symbol index.
func stmtToSymbolInfo(uri string, source string, node ast.AstNode) *lsp.SymbolInformation {
	switch s := node.(type) {
	case *ast.TypeDeclStmt:
		if s.Name == "" {
			return nil
		}
		return &lsp.SymbolInformation{
			Name:     s.Name,
			Kind:     typeDeclKind(s.Type),
			Location: astLocToLSPLocation(uri, source, s.GetLocation()),
		}

	case *ast.TraitDeclStmt:
		if s.Name == "" {
			return nil
		}
		return &lsp.SymbolInformation{
			Name:     s.Name,
			Kind:     lsp.SymbolKindInterface,
			Location: astLocToLSPLocation(uri, source, s.GetLocation()),
		}

	case *ast.VarDeclStmt:
		if s.Name == "" {
			return nil
		}
		kind := varDeclSymbolKind(s)
		if kind != lsp.SymbolKindFunction && kind != lsp.SymbolKindConstant {
			// Only index functions and constants; plain variables are too noisy.
			return nil
		}
		return &lsp.SymbolInformation{
			Name:     s.Name,
			Kind:     kind,
			Location: astLocToLSPLocation(uri, source, s.GetLocation()),
		}
	}
	return nil
}

// fuzzyMatch reports whether every character in pattern appears in text in
// order (subsequence match). An empty pattern matches everything.
func fuzzyMatch(pattern, text string) bool {
	if pattern == "" {
		return true
	}
	pi := 0
	for ti := 0; ti < len(text) && pi < len(pattern); ti++ {
		if text[ti] == pattern[pi] {
			pi++
		}
	}
	return pi == len(pattern)
}
