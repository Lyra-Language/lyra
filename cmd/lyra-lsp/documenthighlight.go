package main

import (
	"context"
	"log"
	"runtime/debug"

	"github.com/owenrumney/go-lsp/lsp"

	"github.com/Lyra-Language/lyra/pkg/ast"
)

// DocumentHighlight implements textDocument/documentHighlight, returning all
// occurrences of the identifier under the cursor in the same file. The handler
// always includes the declaration site. The go-lsp library registers this
// capability automatically via the DocumentHighlightHandler interface.
func (h *Handler) DocumentHighlight(_ context.Context, params *lsp.DocumentHighlightParams) (result []lsp.DocumentHighlight, retErr error) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("documentHighlight panic: %v\n%s", r, debug.Stack())
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

	line := params.Position.Line + 1
	col := byteColumn(source, params.Position.Line, params.Position.Character)

	ident, ok := findExprAtPos(analysis.program, line, col).(*ast.IdentifierExpr)
	if !ok {
		return nil, nil
	}

	declLoc, ok := resolveDeclLocation(ident.Name, line, col, analysis)
	if !ok {
		return nil, nil
	}

	kindText := lsp.DocumentHighlightKindText
	seen := map[ast.Location]bool{}
	add := func(loc ast.Location) {
		if seen[loc] {
			return
		}
		seen[loc] = true
		result = append(result, lsp.DocumentHighlight{
			Range: locToRange(source, loc),
			Kind:  &kindText,
		})
	}

	// Always include the declaration.
	add(declLoc)

	walkExprs(analysis.program, func(e ast.Expression) {
		name, loc, ok := referenceOccurrence(e)
		if !ok || name != ident.Name {
			return
		}
		if dl, ok := resolveDeclLocation(name, loc.StartLine, loc.StartCol, analysis); ok && dl == declLoc {
			add(loc)
		}
	})

	log.Printf("documentHighlight: %q → %d highlight(s)", ident.Name, len(result))
	return result, nil
}
