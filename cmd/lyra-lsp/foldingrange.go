package main

import (
	"context"
	"log"
	"runtime/debug"

	"github.com/owenrumney/go-lsp/lsp"

	"github.com/Lyra-Language/lyra/pkg/ast"
)

// FoldingRange implements textDocument/foldingRange, returning fold regions for
// match expressions, data/struct/trait declarations, and block expressions.
// The go-lsp library registers the capability automatically when this method is present.
func (h *Handler) FoldingRange(_ context.Context, params *lsp.FoldingRangeParams) (result []lsp.FoldingRange, retErr error) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("foldingRange panic: %v\n%s", r, debug.Stack())
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

	var ranges []lsp.FoldingRange

	addFold := func(loc ast.Location) {
		startLine := lspPos(loc.StartLine)
		endLine := lspPos(loc.EndLine)
		if startLine >= endLine {
			return
		}
		ranges = append(ranges, lsp.FoldingRange{
			StartLine: startLine,
			EndLine:   endLine,
		})
	}

	for _, node := range analysis.program.Statements {
		// Emit a fold for each top-level multi-line declaration (struct, data, trait).
		switch s := node.(type) {
		case *ast.TypeDeclStmt:
			addFold(s.GetLocation())
		case *ast.TraitDeclStmt:
			addFold(s.GetLocation())
		}

		// Walk expressions within the statement for match and block folds.
		if stmt, ok := node.(ast.Statement); ok {
			ast.WalkStmt(stmt, nil, func(expr ast.Expression) bool {
				switch e := expr.(type) {
				case *ast.MatchExpr:
					addFold(e.GetLocation())
				case *ast.BlockExpr:
					addFold(e.GetLocation())
				}
				return true
			})
		}
	}

	return ranges, nil
}
