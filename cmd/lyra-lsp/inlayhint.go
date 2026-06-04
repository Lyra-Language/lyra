package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"runtime/debug"

	"github.com/owenrumney/go-lsp/lsp"

	"github.com/Lyra-Language/lyra/pkg/ast"
)

// InlayHint implements textDocument/inlayHint, returning type hints for
// unannotated let/var/const bindings within the requested range.
func (h *Handler) InlayHint(_ context.Context, params *lsp.InlayHintParams) (result []lsp.InlayHint, retErr error) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("inlayHint panic: %v\n%s", r, debug.Stack())
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

	var hints []lsp.InlayHint
	for _, node := range analysis.program.Statements {
		walkNodeForHints(node, params.Range, analysis, &hints)
	}
	return hints, nil
}

func walkNodeForHints(node ast.AstNode, r lsp.Range, analysis *docAnalysis, out *[]lsp.InlayHint) {
	if node == nil {
		return
	}
	switch s := node.(type) {
	case *ast.VarDeclStmt:
		if s.Type == nil && inRange(s.GetLocation(), r) {
			if hint := varDeclHint(s, analysis); hint != nil {
				*out = append(*out, *hint)
			}
		}
		walkExprForHints(s.Value, r, analysis, out)
	case *ast.ExpressionStmt:
		walkExprForHints(s.Expression, r, analysis, out)
	case *ast.ReturnStmt:
		walkExprForHints(s.Value, r, analysis, out)
	}
}

func walkExprForHints(expr ast.Expression, r lsp.Range, analysis *docAnalysis, out *[]lsp.InlayHint) {
	if expr == nil {
		return
	}
	switch e := expr.(type) {
	case *ast.BlockExpr:
		for _, stmt := range e.Statements {
			walkNodeForHints(stmt, r, analysis, out)
		}
	case *ast.IfExpr:
		walkExprForHints(e.Then, r, analysis, out)
		walkExprForHints(e.Else, r, analysis, out)
	case *ast.MatchExpr:
		for _, arm := range e.MatchArms {
			walkExprForHints(arm.Body, r, analysis, out)
		}
	case *ast.LambdaExpr:
		walkExprForHints(e.Body, r, analysis, out)
	}
}

func varDeclHint(stmt *ast.VarDeclStmt, analysis *docAnalysis) *lsp.InlayHint {
	typ, ok := analysis.typeTable.Get(stmt.Value)
	if !ok {
		return nil
	}
	loc := stmt.GetLocation()
	// Position immediately after the variable name: keyword + space + name
	col := lspPos(loc.StartCol) + len(stmt.BindingKind.String()) + 1 + len(stmt.Name)
	kind := lsp.InlayHintKindType
	label, _ := json.Marshal(fmt.Sprintf(": %s", typ))
	return &lsp.InlayHint{
		Position: lsp.Position{Line: lspPos(loc.StartLine), Character: col},
		Label:    label,
		Kind:     &kind,
	}
}

// inRange reports whether the declaration's start line falls within the LSP range.
func inRange(loc ast.Location, r lsp.Range) bool {
	line := lspPos(loc.StartLine)
	return line >= r.Start.Line && line <= r.End.Line
}
