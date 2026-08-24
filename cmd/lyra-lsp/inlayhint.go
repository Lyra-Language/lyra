package main

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/owenrumney/go-lsp/lsp"

	"github.com/Lyra-Language/lyra/pkg/ast"
)

// InlayHint implements textDocument/inlayHint, returning type hints for
// unannotated let/var/const bindings within the requested range.
func (h *Handler) InlayHint(_ context.Context, params *lsp.InlayHintParams) (result []lsp.InlayHint, retErr error) {
	defer recoverHandler("inlayHint", &result, &retErr)

	uri := string(params.TextDocument.URI)
	analysis, source, ok := h.docFor(uri)
	if !ok {
		return nil, nil
	}

	var hints []lsp.InlayHint
	for _, node := range analysis.program.Statements {
		walkNodeForHints(source, node, params.Range, analysis, &hints)
	}
	return hints, nil
}

func walkNodeForHints(source string, node ast.AstNode, r lsp.Range, analysis *docAnalysis, out *[]lsp.InlayHint) {
	if node == nil {
		return
	}
	switch s := node.(type) {
	case *ast.VarDeclStmt:
		if s.Type == nil && inRange(s.GetLocation(), r) {
			if hint := varDeclHint(source, s, analysis); hint != nil {
				*out = append(*out, *hint)
			}
		}
		walkExprForHints(source, s.Value, r, analysis, out)
	case *ast.ExpressionStmt:
		walkExprForHints(source, s.Expression, r, analysis, out)
	case *ast.ReturnStmt:
		walkExprForHints(source, s.Value, r, analysis, out)
	}
}

func walkExprForHints(source string, expr ast.Expression, r lsp.Range, analysis *docAnalysis, out *[]lsp.InlayHint) {
	if expr == nil {
		return
	}
	switch e := expr.(type) {
	case *ast.BlockExpr:
		for _, stmt := range e.Statements {
			walkNodeForHints(source, stmt, r, analysis, out)
		}
	case *ast.IfExpr:
		walkExprForHints(source, e.Then, r, analysis, out)
		walkExprForHints(source, e.Else, r, analysis, out)
	case *ast.MatchExpr:
		for _, arm := range e.MatchArms {
			walkExprForHints(source, arm.Body, r, analysis, out)
		}
	case *ast.LambdaExpr:
		walkExprForHints(source, e.Body, r, analysis, out)
	}
}

func varDeclHint(source string, stmt *ast.VarDeclStmt, analysis *docAnalysis) *lsp.InlayHint {
	typ, ok := analysis.typeTable.Get(stmt.Value)
	if !ok {
		return nil
	}
	loc := stmt.GetLocation()
	// Byte column immediately after the variable name (keyword + space + name),
	// converted to a UTF-16 column for the hint position.
	byteCol := lspPos(loc.StartCol) + len(stmt.BindingKind.String()) + 1 + len(stmt.Name)
	kind := lsp.InlayHintKindType
	label, _ := json.Marshal(fmt.Sprintf(": %s", typ))
	return &lsp.InlayHint{
		Position: lsp.Position{
			Line:      lspPos(loc.StartLine),
			Character: utf16Column(source, lspPos(loc.StartLine), byteCol),
		},
		Label: label,
		Kind:  &kind,
	}
}

// inRange reports whether the declaration's start line falls within the LSP range.
func inRange(loc ast.Location, r lsp.Range) bool {
	line := lspPos(loc.StartLine)
	return line >= r.Start.Line && line <= r.End.Line
}
