package main

import (
	"context"
	"strings"

	"github.com/owenrumney/go-lsp/lsp"

	"github.com/Lyra-Language/lyra/pkg/ast"
)

// WorkspaceSymbol implements workspace/symbol, returning SymbolInformation for
// every type declaration and function in all open documents whose name
// fuzzy-matches the query. An empty query returns all symbols.
func (h *Handler) WorkspaceSymbol(_ context.Context, params *lsp.WorkspaceSymbolParams) (result []lsp.SymbolInformation, retErr error) {
	defer recoverHandler("workspaceSymbol", &result, &retErr)

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

// stmtToSymbolInfo is one entry in the workspace symbol index: the declaration located at
// its **name**, so jumping to a search hit lands on the thing that matched.
//
// It indexes less than the outline does. A plain `var` or unannotated `let` is a symbol in
// its own file and noise across a workspace, so only functions and constants are kept —
// which is this consumer's policy, not something symbolOf should know.
func stmtToSymbolInfo(uri string, source string, node ast.AstNode) *lsp.SymbolInformation {
	d, ok := symbolOf(node)
	if !ok {
		return nil
	}
	if _, isVar := node.(*ast.VarDeclStmt); isVar &&
		d.kind != lsp.SymbolKindFunction && d.kind != lsp.SymbolKindConstant {
		return nil
	}
	return &lsp.SymbolInformation{
		Name:     d.name,
		Kind:     d.kind,
		Location: astLocToLSPLocation(uri, source, d.nameLoc),
	}
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
