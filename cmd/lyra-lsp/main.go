package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"runtime/debug"
	"sync"

	"github.com/owenrumney/go-lsp/lsp"
	"github.com/owenrumney/go-lsp/server"
	sitter "github.com/tree-sitter/go-tree-sitter"

	"github.com/Lyra-Language/lyra/pkg/analyzer/checker"
	"github.com/Lyra-Language/lyra/pkg/analyzer/collector"
	"github.com/Lyra-Language/lyra/pkg/analyzer/typechecker"
	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/ast/symbols"
	diag "github.com/Lyra-Language/lyra/pkg/diagnostic"
	"github.com/Lyra-Language/lyra/pkg/parser"
	"github.com/Lyra-Language/lyra/pkg/typetable"
)

// docAnalysis holds the full analysis result for one open document.
type docAnalysis struct {
	program    *ast.Program
	symTable   *symbols.SymbolTable
	scopeTable *symbols.ScopeTable
	typeTable  *typetable.TypeTable
}

type Handler struct {
	client        *server.Client
	mu            sync.Mutex
	docStore      map[string]string      // URI → current full document text
	analysisStore map[string]*docAnalysis // URI → last successful analysis
}

func newHandler() *Handler {
	return &Handler{
		docStore:      make(map[string]string),
		analysisStore: make(map[string]*docAnalysis),
	}
}

// SetClient is called by the server after the connection is established.
func (h *Handler) SetClient(c *server.Client) {
	h.client = c
}

// Initialize returns server capabilities. We use SyncIncremental so the client
// only sends changed ranges on each edit; the server applies them to its own
// document store and keeps the full text in memory.
func (h *Handler) Initialize(_ context.Context, _ *lsp.InitializeParams) (*lsp.InitializeResult, error) {
	openClose := true
	enabled := true
	return &lsp.InitializeResult{
		ServerInfo: &lsp.ServerInfo{Name: "lyra-lsp", Version: "0.1.0"},
		Capabilities: lsp.ServerCapabilities{
			TextDocumentSync: &lsp.TextDocumentSyncOptions{
				OpenClose: &openClose,
				Change:    lsp.SyncIncremental,
			},
			DocumentSymbolProvider: &enabled,
			// `.` triggers member completion as soon as it is typed; other
			// completions are still offered on the usual identifier characters.
			CompletionProvider: &lsp.CompletionOptions{
				TriggerCharacters: []string{"."},
			},
			// `(` triggers signature help as soon as the call is opened; `,` re-
			// triggers it when the user moves to the next argument. The handler
			// implements SignatureHelpHandler so the capability auto-registers,
			// but we set it explicitly here to carry the trigger characters.
			SignatureHelpProvider: &lsp.SignatureHelpOptions{
				TriggerCharacters:   []string{"("},
				RetriggerCharacters: []string{","},
			},
			// Semantic tokens augment the TextMate grammar with classifications
			// it can't make (constant vs mutable variable, function, type, …).
			// The handler implements SemanticTokensFullHandler, so the provider
			// is set explicitly here only to carry the legend; mergeCapabilities
			// keeps this over the empty auto-built one.
			SemanticTokensProvider: &lsp.SemanticTokensOptions{
				Legend: lsp.SemanticTokensLegend{
					TokenTypes:     semanticTokenTypes,
					TokenModifiers: semanticTokenModifiers,
				},
				Full: &lsp.SemanticTokensFull{},
			},
		},
	}, nil
}

func (h *Handler) Shutdown(_ context.Context) error { return nil }

// DidOpen stores the initial document text and runs the first analysis.
func (h *Handler) DidOpen(ctx context.Context, params *lsp.DidOpenTextDocumentParams) error {
	h.mu.Lock()
	h.docStore[string(params.TextDocument.URI)] = params.TextDocument.Text
	h.mu.Unlock()
	return h.analyze(ctx, params.TextDocument.URI, params.TextDocument.Text)
}

// DidChange applies incremental edits to the document store, then re-analyzes.
func (h *Handler) DidChange(ctx context.Context, params *lsp.DidChangeTextDocumentParams) error {
	if len(params.ContentChanges) == 0 {
		return nil
	}

	h.mu.Lock()
	uri := string(params.TextDocument.URI)
	text := h.docStore[uri]
	for _, change := range params.ContentChanges {
		if change.Range == nil {
			// Full replacement (client sent a non-incremental change).
			text = change.Text
		} else {
			text = applyEdit(text, *change.Range, change.Text)
		}
	}
	h.docStore[uri] = text
	h.mu.Unlock()

	return h.analyze(ctx, params.TextDocument.URI, text)
}

// DidSave re-analyzes using the already-current document store. We do not
// request IncludeText on save since the store is kept up-to-date by DidChange.
func (h *Handler) DidSave(ctx context.Context, params *lsp.DidSaveTextDocumentParams) error {
	h.mu.Lock()
	text := h.docStore[string(params.TextDocument.URI)]
	h.mu.Unlock()
	return h.analyze(ctx, params.TextDocument.URI, text)
}

// DidClose removes the document from the store and clears its diagnostics.
func (h *Handler) DidClose(ctx context.Context, params *lsp.DidCloseTextDocumentParams) error {
	h.mu.Lock()
	delete(h.docStore, string(params.TextDocument.URI))
	delete(h.analysisStore, string(params.TextDocument.URI))
	h.mu.Unlock()
	return h.client.PublishDiagnostics(ctx, &lsp.PublishDiagnosticsParams{
		URI:         params.TextDocument.URI,
		Diagnostics: []lsp.Diagnostic{},
	})
}

// analyze runs parser → collector → typechecker and pushes diagnostics.
func (h *Handler) analyze(ctx context.Context, uri lsp.DocumentURI, source string) (retErr error) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("analyze panic: %v\n%s", r, debug.Stack())
			sev := lsp.SeverityError
			_ = h.client.PublishDiagnostics(ctx, &lsp.PublishDiagnosticsParams{
				URI: uri,
				Diagnostics: []lsp.Diagnostic{{
					Range:    lsp.Range{},
					Severity: &sev,
					Source:   "lyra",
					Message:  fmt.Sprintf("internal error: %v", r),
				}},
			})
			retErr = fmt.Errorf("analyze panicked: %v", r)
		}
	}()

	diags := []lsp.Diagnostic{}

	log.Printf("analyze: parsing %s", uri)
	tree, err := parser.Parse(source)
	if tree == nil && err == nil {
		err = fmt.Errorf("parser returned nil tree")
	}
	if err != nil {
		sev := lsp.SeverityError
		diags = append(diags, lsp.Diagnostic{
			Range:    lsp.Range{},
			Severity: &sev,
			Source:   "lyra",
			Message:  fmt.Sprintf("parse error: %v", err),
		})
		return h.client.PublishDiagnostics(ctx, &lsp.PublishDiagnosticsParams{
			URI:         uri,
			Diagnostics: diags,
		})
	}

	diags = append(diags, collectParseErrors(tree.RootNode(), []byte(source))...)

	log.Printf("analyze: collecting")
	c := collector.NewCollector([]byte(source))
	program, symTable, scopeTable, collectorErrors := c.Collect(tree.RootNode())
	log.Printf("analyze: collect done (%d errors)", len(collectorErrors))

	for _, rawErr := range collectorErrors {
		ce, ok := rawErr.(diag.Diagnostic)
		if !ok {
			sev := lsp.SeverityError
			diags = append(diags, lsp.Diagnostic{
				Range:    lsp.Range{},
				Severity: &sev,
				Source:   "lyra",
				Message:  rawErr.Error(),
			})
			continue
		}

		sev := severityFromCollector(ce.Severity)
		loc := ce.Location
		diags = append(diags, lsp.Diagnostic{
			Range: lsp.Range{
				Start: lsp.Position{Line: lspPos(loc.StartLine), Character: lspPos(loc.StartCol)},
				End:   lsp.Position{Line: lspPos(loc.EndLine), Character: lspPos(loc.EndCol)},
			},
			Severity:           &sev,
			Code:               codeToLSP(ce.Code),
			Source:             "lyra",
			Message:            ce.Message,
			Tags:               tagsToLSP(ce.Tags),
			RelatedInformation: toLSPRelatedInfo(uri, ce.RelatedInformation),
		})
	}

	log.Printf("analyze: checking use-before-declaration")
	for _, ube := range checker.CheckUseBeforeDeclaration(program) {
		sev := lsp.SeverityError
		loc := ube.Location
		diags = append(diags, lsp.Diagnostic{
			Range: lsp.Range{
				Start: lsp.Position{Line: lspPos(loc.StartLine), Character: lspPos(loc.StartCol)},
				End:   lsp.Position{Line: lspPos(loc.EndLine), Character: lspPos(loc.EndCol)},
			},
			Severity: &sev,
			Code:     codeToLSP(ube.Code),
			Source:   "lyra",
			Message:  ube.Message,
		})
	}

	log.Printf("analyze: checking return outside function")
	for _, re := range checker.CheckReturnOutsideFunction(program) {
		sev := lsp.SeverityError
		loc := re.Location
		diags = append(diags, lsp.Diagnostic{
			Range: lsp.Range{
				Start: lsp.Position{Line: lspPos(loc.StartLine), Character: lspPos(loc.StartCol)},
				End:   lsp.Position{Line: lspPos(loc.EndLine), Character: lspPos(loc.EndCol)},
			},
			Severity: &sev,
			Code:     codeToLSP(re.Code),
			Source:   "lyra",
			Message:  re.Message,
		})
	}

	log.Printf("analyze: checking break/continue outside loop")
	for _, be := range checker.CheckBreakContinueOutsideLoop(program) {
		sev := lsp.SeverityError
		loc := be.Location
		diags = append(diags, lsp.Diagnostic{
			Range: lsp.Range{
				Start: lsp.Position{Line: lspPos(loc.StartLine), Character: lspPos(loc.StartCol)},
				End:   lsp.Position{Line: lspPos(loc.EndLine), Character: lspPos(loc.EndCol)},
			},
			Severity: &sev,
			Code:     codeToLSP(be.Code),
			Source:   "lyra",
			Message:  be.Message,
		})
	}

	log.Printf("analyze: checking await outside async")
	for _, ae := range checker.CheckAwaitOutsideAsync(program) {
		sev := lsp.SeverityError
		loc := ae.Location
		diags = append(diags, lsp.Diagnostic{
			Range: lsp.Range{
				Start: lsp.Position{Line: lspPos(loc.StartLine), Character: lspPos(loc.StartCol)},
				End:   lsp.Position{Line: lspPos(loc.EndLine), Character: lspPos(loc.EndCol)},
			},
			Severity: &sev,
			Code:     codeToLSP(ae.Code),
			Source:   "lyra",
			Message:  ae.Message,
		})
	}

	log.Printf("analyze: checking try outside result")
	for _, te := range checker.CheckTryOutsideResult(program, symTable) {
		sev := lsp.SeverityError
		loc := te.Location
		diags = append(diags, lsp.Diagnostic{
			Range: lsp.Range{
				Start: lsp.Position{Line: lspPos(loc.StartLine), Character: lspPos(loc.StartCol)},
				End:   lsp.Position{Line: lspPos(loc.EndLine), Character: lspPos(loc.EndCol)},
			},
			Severity: &sev,
			Code:     codeToLSP(te.Code),
			Source:   "lyra",
			Message:  te.Message,
		})
	}

	log.Printf("analyze: checking yield outside generator")
	for _, ye := range checker.CheckYieldOutsideGenerator(program) {
		sev := lsp.SeverityError
		loc := ye.Location
		diags = append(diags, lsp.Diagnostic{
			Range: lsp.Range{
				Start: lsp.Position{Line: lspPos(loc.StartLine), Character: lspPos(loc.StartCol)},
				End:   lsp.Position{Line: lspPos(loc.EndLine), Character: lspPos(loc.EndCol)},
			},
			Severity: &sev,
			Code:     codeToLSP(ye.Code),
			Source:   "lyra",
			Message:  ye.Message,
		})
	}

	log.Printf("analyze: checking unsafe outside unsafe")
	for _, ue := range checker.CheckUnsafeOutsideUnsafe(program) {
		sev := lsp.SeverityError
		loc := ue.Location
		diags = append(diags, lsp.Diagnostic{
			Range: lsp.Range{
				Start: lsp.Position{Line: lspPos(loc.StartLine), Character: lspPos(loc.StartCol)},
				End:   lsp.Position{Line: lspPos(loc.EndLine), Character: lspPos(loc.EndCol)},
			},
			Severity: &sev,
			Code:     codeToLSP(ue.Code),
			Source:   "lyra",
			Message:  ue.Message,
		})
	}

	log.Printf("analyze: checking recursive types")
	for _, re := range checker.CheckRecursiveTypes(program) {
		sev := lsp.SeverityError
		loc := re.Location
		diags = append(diags, lsp.Diagnostic{
			Range: lsp.Range{
				Start: lsp.Position{Line: lspPos(loc.StartLine), Character: lspPos(loc.StartCol)},
				End:   lsp.Position{Line: lspPos(loc.EndLine), Character: lspPos(loc.EndCol)},
			},
			Severity: &sev,
			Code:     codeToLSP(re.Code),
			Source:   "lyra",
			Message:  re.Message,
		})
	}

	log.Printf("analyze: checking effect bounds")
	for _, eb := range checker.CheckEffectBounds(program) {
		sev := lsp.SeverityError
		loc := eb.Location
		diags = append(diags, lsp.Diagnostic{
			Range: lsp.Range{
				Start: lsp.Position{Line: lspPos(loc.StartLine), Character: lspPos(loc.StartCol)},
				End:   lsp.Position{Line: lspPos(loc.EndLine), Character: lspPos(loc.EndCol)},
			},
			Severity: &sev,
			Code:     codeToLSP(eb.Code),
			Source:   "lyra",
			Message:  eb.Message,
		})
	}

	log.Printf("analyze: typechecking")
	tt := typetable.New()
	tc := typechecker.New(symTable, scopeTable, tt)
	typeErrors := tc.Check(program)

	log.Printf("analyze: checking purity")
	for _, pe := range checker.CheckPurity(program, scopeTable, tc.MethodTable()) {
		sev := lsp.SeverityError
		loc := pe.Location
		diags = append(diags, lsp.Diagnostic{
			Range: lsp.Range{
				Start: lsp.Position{Line: lspPos(loc.StartLine), Character: lspPos(loc.StartCol)},
				End:   lsp.Position{Line: lspPos(loc.EndLine), Character: lspPos(loc.EndCol)},
			},
			Severity: &sev,
			Code:     codeToLSP(pe.Code),
			Source:   "lyra",
			Message:  pe.Message,
		})
	}

	log.Printf("analyze: checking unreachable code")
	for _, d := range checker.CheckUnreachableCode(program) {
		diags = append(diags, diagToLSP(uri, d))
	}

	log.Printf("analyze: checking unused variables")
	for _, d := range checker.CheckUnusedVariables(program) {
		diags = append(diags, diagToLSP(uri, d))
	}

	log.Printf("analyze: checking unused imports")
	for _, d := range checker.CheckUnusedImports(program) {
		diags = append(diags, diagToLSP(uri, d))
	}

	log.Printf("analyze: checking unused parameters")
	for _, d := range checker.CheckUnusedParameters(program) {
		diags = append(diags, diagToLSP(uri, d))
	}

	log.Printf("analyze: checking shadowing")
	for _, sw := range checker.CheckShadowing(program) {
		sev := lsp.SeverityWarning
		loc := sw.Location
		var relatedInfo []lsp.DiagnosticRelatedInformation
		if sw.OriginalLocation.StartLine > 0 {
			relatedInfo = []lsp.DiagnosticRelatedInformation{{
				Location: lsp.Location{
					URI: uri,
					Range: lsp.Range{
						Start: lsp.Position{Line: lspPos(sw.OriginalLocation.StartLine), Character: lspPos(sw.OriginalLocation.StartCol)},
						End:   lsp.Position{Line: lspPos(sw.OriginalLocation.EndLine), Character: lspPos(sw.OriginalLocation.EndCol)},
					},
				},
				Message: "previously declared here",
			}}
		}
		diags = append(diags, lsp.Diagnostic{
			Range: lsp.Range{
				Start: lsp.Position{Line: lspPos(loc.StartLine), Character: lspPos(loc.StartCol)},
				End:   lsp.Position{Line: lspPos(loc.EndLine), Character: lspPos(loc.EndCol)},
			},
			Severity:           &sev,
			Code:               codeToLSP(sw.Code),
			Source:             "lyra",
			Message:            sw.Message,
			RelatedInformation: relatedInfo,
		})
	}

	for _, te := range typeErrors {
		sev := severityFromTypechecker(te.Severity)
		loc := te.Location
		diags = append(diags, lsp.Diagnostic{
			Range: lsp.Range{
				Start: lsp.Position{Line: lspPos(loc.StartLine), Character: lspPos(loc.StartCol)},
				End:   lsp.Position{Line: lspPos(loc.EndLine), Character: lspPos(loc.EndCol)},
			},
			Severity:           &sev,
			Code:               codeToLSP(te.Code),
			Source:             "lyra",
			Message:            te.Message,
			Tags:               tagsToLSP(te.Tags),
			RelatedInformation: toLSPRelatedInfo(uri, te.RelatedInformation),
		})
	}

	h.mu.Lock()
	h.analysisStore[string(uri)] = &docAnalysis{
		program:    program,
		symTable:   symTable,
		scopeTable: scopeTable,
		typeTable:  tt,
	}
	h.mu.Unlock()

	log.Printf("analyze: publishing %d diagnostics", len(diags))
	return h.client.PublishDiagnostics(ctx, &lsp.PublishDiagnosticsParams{
		URI:         uri,
		Diagnostics: diags,
	})
}

// applyEdit applies a single incremental LSP text change to src and returns
// the updated document. LSP positions are 0-based line/character pairs where
// "character" counts UTF-16 code units; for ASCII Lyra source this equals the
// byte offset within the line, which is what we use here.
func applyEdit(src string, r lsp.Range, newText string) string {
	start := posToOffset(src, r.Start.Line, r.Start.Character)
	end := posToOffset(src, r.End.Line, r.End.Character)
	return src[:start] + newText + src[end:]
}

// posToOffset converts a 0-based LSP (line, character) position to a byte
// offset within text. It walks the string once, counting newlines to reach the
// target line and then advancing 'char' bytes within that line.
func posToOffset(text string, line, char int) int {
	curLine := 0
	for i := 0; i < len(text); i++ {
		if curLine == line {
			return min(i+char, len(text))
		}
		if text[i] == '\n' {
			curLine++
		}
	}
	return len(text)
}

// lspPos converts a 1-based ast.Location line/column to a 0-based LSP position,
// clamping at zero to guard against uninitialized (zero-value) locations.
func lspPos(oneBased int) int {
	if oneBased <= 0 {
		return 0
	}
	return oneBased - 1
}

// diagToLSP converts a diag.Diagnostic to an lsp.Diagnostic for publishing.
func diagToLSP(uri lsp.DocumentURI, d diag.Diagnostic) lsp.Diagnostic {
	sev := lsp.DiagnosticSeverity(0)
	switch d.Severity {
	case diag.SeverityWarning:
		sev = lsp.SeverityWarning
	case diag.SeverityInfo:
		sev = lsp.SeverityInformation
	default:
		sev = lsp.SeverityError
	}
	loc := d.Location
	return lsp.Diagnostic{
		Range: lsp.Range{
			Start: lsp.Position{Line: lspPos(loc.StartLine), Character: lspPos(loc.StartCol)},
			End:   lsp.Position{Line: lspPos(loc.EndLine), Character: lspPos(loc.EndCol)},
		},
		Severity:           &sev,
		Code:               codeToLSP(d.Code),
		Source:             "lyra",
		Message:            d.Message,
		Tags:               tagsToLSP(d.Tags),
		RelatedInformation: toLSPRelatedInfo(uri, d.RelatedInformation),
	}
}

func codeToLSP(code string) json.RawMessage {
	if code == "" {
		return nil
	}
	b, _ := json.Marshal(code)
	return b
}

func tagsToLSP(tags []diag.Tag) []lsp.DiagnosticTag {
	if len(tags) == 0 {
		return nil
	}
	out := make([]lsp.DiagnosticTag, len(tags))
	for i, t := range tags {
		out[i] = lsp.DiagnosticTag(t)
	}
	return out
}

func toLSPRelatedInfo(uri lsp.DocumentURI, related []diag.RelatedInformation) []lsp.DiagnosticRelatedInformation {
	if len(related) == 0 {
		return nil
	}
	out := make([]lsp.DiagnosticRelatedInformation, 0, len(related))
	for _, r := range related {
		if r.Location.StartLine == 0 {
			continue
		}
		out = append(out, lsp.DiagnosticRelatedInformation{
			Location: lsp.Location{
				URI: uri,
				Range: lsp.Range{
					Start: lsp.Position{Line: lspPos(r.Location.StartLine), Character: lspPos(r.Location.StartCol)},
					End:   lsp.Position{Line: lspPos(r.Location.EndLine), Character: lspPos(r.Location.EndCol)},
				},
			},
			Message: r.Message,
		})
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func severityFromCollector(s diag.Severity) lsp.DiagnosticSeverity {
	switch s {
	case collector.CollectorErrorSeverityWarning:
		return lsp.SeverityWarning
	case collector.CollectorErrorSeverityInfo:
		return lsp.SeverityInformation
	default:
		return lsp.SeverityError
	}
}

func severityFromTypechecker(s typechecker.Severity) lsp.DiagnosticSeverity {
	if s == typechecker.SeverityWarning {
		return lsp.SeverityWarning
	}
	return lsp.SeverityError
}

// collectParseErrors walks the tree-sitter CST and returns diagnostics for
// ERROR and MISSING nodes. Tree-sitter embeds parse errors as named nodes in
// the tree rather than returning a parse failure, so this is the only way to
// surface syntax errors with accurate source ranges.
func collectParseErrors(root *sitter.Node, source []byte) []lsp.Diagnostic {
	if !root.HasError() {
		return nil
	}
	var diags []lsp.Diagnostic
	var walk func(*sitter.Node)
	walk = func(node *sitter.Node) {
		if node.IsMissing() {
			sev := lsp.SeverityError
			start := node.StartPosition()
			end := node.EndPosition()
			diags = append(diags, lsp.Diagnostic{
				Range: lsp.Range{
					Start: lsp.Position{Line: int(start.Row), Character: int(start.Column)},
					End:   lsp.Position{Line: int(end.Row), Character: int(end.Column)},
				},
				Severity: &sev,
				Source:   "lyra",
				Message:  fmt.Sprintf("missing %s", node.Kind()),
			})
			return
		}
		if node.IsError() {
			sev := lsp.SeverityError
			start := node.StartPosition()
			end := node.EndPosition()
			text := node.Utf8Text(source)
			msg := "syntax error"
			if len(text) > 0 && len(text) <= 40 {
				msg = fmt.Sprintf("syntax error: unexpected %q", text)
			}
			diags = append(diags, lsp.Diagnostic{
				Range: lsp.Range{
					Start: lsp.Position{Line: int(start.Row), Character: int(start.Column)},
					End:   lsp.Position{Line: int(end.Row), Character: int(end.Column)},
				},
				Severity: &sev,
				Source:   "lyra",
				Message:  msg,
			})
			return
		}
		for i := uint(0); i < node.ChildCount(); i++ {
			if child := node.Child(i); child != nil && child.HasError() {
				walk(child)
			}
		}
	}
	walk(root)
	return diags
}

// Hover returns type information for the symbol under the cursor.
// The go-lsp library registers textDocument/hover automatically when this
// method is present on the handler.
func (h *Handler) Hover(_ context.Context, params *lsp.HoverParams) (result *lsp.Hover, retErr error) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("hover panic: %v\n%s", r, debug.Stack())
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

	// LSP positions are 0-based; ast.Location is 1-based.
	line := params.Position.Line + 1
	col := params.Position.Character + 1

	expr := findExprAtPos(analysis.program, line, col)
	if expr == nil {
		return nil, nil
	}

	typ, ok := analysis.typeTable.Get(expr)
	if !ok {
		return nil, nil
	}

	content := hoverContent(expr, typ)
	return &lsp.Hover{
		Contents: lsp.MarkupContent{Kind: lsp.Markdown, Value: content},
	}, nil
}

func main() {
	logFile, err := os.OpenFile("/tmp/lyra-lsp.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err == nil {
		log.SetOutput(logFile)
		defer logFile.Close()
	}
	log.Println("lyra-lsp started")

	srv := server.NewServer(newHandler())
	if err := srv.Run(context.Background(), server.RunStdio()); err != nil {
		log.Printf("server exited: %v", err)
	}
}
