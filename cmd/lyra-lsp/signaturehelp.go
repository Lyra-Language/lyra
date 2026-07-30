package main

import (
	"context"
	"fmt"
	"log"
	"runtime/debug"
	"strings"

	"github.com/owenrumney/go-lsp/lsp"

	"github.com/Lyra-Language/lyra/pkg/ast"
)

// SignatureHelp implements textDocument/signatureHelp. When the cursor is inside
// a function call's argument list, it returns the lambda's parameter list and
// highlights the parameter corresponding to the cursor's comma position.
//
// The go-lsp library auto-registers the capability via the SignatureHelpHandler
// interface; Initialize also sets an explicit SignatureHelpProvider to advertise
// `(` and `,` as trigger characters.
func (h *Handler) SignatureHelp(_ context.Context, params *lsp.SignatureHelpParams) (result *lsp.SignatureHelp, retErr error) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("signatureHelp panic: %v\n%s", r, debug.Stack())
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

	// LSP positions are 0-based UTF-16; ast.Location is 1-based bytes.
	line := params.Position.Line + 1
	col := byteColumn(source, params.Position.Line, params.Position.Character)

	// Scan the source prefix up to the cursor to find the enclosing call site.
	prefix := source[:posToOffset(source, params.Position.Line, params.Position.Character)]
	callee, activeParam, found := parseCallContext(prefix)
	if !found {
		return nil, nil
	}

	// Resolve the callee name to a LambdaExpr via the lexical scope.
	scope := findScopeAtPos(analysis.program, analysis.scopeTable, analysis.symTable.EntryScope(), line, col)
	named, ok := scope.Lookup(callee)
	if !ok {
		return nil, nil
	}
	decl, ok := named.(*ast.VarDeclStmt)
	if !ok {
		return nil, nil
	}
	lambda, ok := decl.Value.(*ast.LambdaExpr)
	if !ok {
		return nil, nil
	}

	sig := buildSignatureInfo(callee, lambda)
	zero := 0
	ap := activeParam
	if ap >= len(lambda.Parameters) {
		ap = len(lambda.Parameters) - 1
	}
	if ap < 0 {
		ap = 0
	}
	return &lsp.SignatureHelp{
		Signatures:      []lsp.SignatureInformation{sig},
		ActiveSignature: &zero,
		ActiveParameter: &ap,
	}, nil
}

// parseCallContext scans the source prefix backwards to find the innermost
// unclosed `(`. Returns the callee identifier before the `(` and the number of
// top-level commas between the `(` and the cursor (= the zero-based active
// parameter index). Bracket pairs `[]` and `{}` are also tracked so inner
// commas (e.g. array literals) don't count toward the parameter index.
func parseCallContext(prefix string) (callee string, activeParam int, ok bool) {
	depth := 0
	commas := 0
	for i := len(prefix) - 1; i >= 0; i-- {
		switch prefix[i] {
		case ')', ']', '}':
			depth++
		case '(':
			if depth > 0 {
				depth--
			} else {
				callee = identBefore(prefix, i)
				if callee == "" {
					return "", 0, false
				}
				return callee, commas, true
			}
		case '[', '{':
			if depth > 0 {
				depth--
			}
		case ',':
			if depth == 0 {
				commas++
			}
		}
	}
	return "", 0, false
}

// identBefore returns the identifier that immediately precedes position i in s,
// skipping any trailing whitespace before i.
func identBefore(s string, i int) string {
	j := i - 1
	for j >= 0 && (s[j] == ' ' || s[j] == '\t') {
		j--
	}
	end := j + 1
	for j >= 0 && isIdentChar(s[j]) {
		j--
	}
	return s[j+1 : end]
}

// buildSignatureInfo formats the signature label for a lambda and builds
// ParameterInformation with byte-offset label ranges for active-parameter
// highlighting. Label form: `callee(param1: T1, param2: T2) -> ReturnType`.
func buildSignatureInfo(callee string, lambda *ast.LambdaExpr) lsp.SignatureInformation {
	var sb strings.Builder
	sb.WriteString(callee)
	sb.WriteByte('(')

	paramInfos := make([]lsp.ParameterInformation, 0, len(lambda.Parameters))
	for i, p := range lambda.Parameters {
		if i > 0 {
			sb.WriteString(", ")
		}
		start := sb.Len()
		sb.WriteString(formatParamLabel(p))
		end := sb.Len()
		paramInfos = append(paramInfos, lsp.ParameterInformation{
			Label: []int{start, end},
		})
	}

	sb.WriteByte(')')

	if lambda.ReturnType.Type != nil {
		sb.WriteString(" -> ")
		sb.WriteString(lambda.ReturnType.String())
	}

	return lsp.SignatureInformation{
		Label:      sb.String(),
		Parameters: paramInfos,
	}
}

// formatParamLabel renders a single parameter as "name: Type" (or just "name"
// when the type is unannotated).
func formatParamLabel(p ast.Parameter) string {
	name := p.Pattern.GetName()
	if p.Type != nil {
		return fmt.Sprintf("%s: %s", name, p.Type.String())
	}
	return name
}
