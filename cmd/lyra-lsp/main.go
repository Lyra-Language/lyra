package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"runtime/debug"

	"github.com/owenrumney/go-lsp/lsp"
	"github.com/owenrumney/go-lsp/server"

	"github.com/Lyra-Language/lyra/pkg/analyzer/collector"
	"github.com/Lyra-Language/lyra/pkg/analyzer/typechecker"
	"github.com/Lyra-Language/lyra/pkg/parser"
	"github.com/Lyra-Language/lyra/pkg/typetable"
)

type Handler struct {
	client *server.Client
}

// SetClient is called by the server after the connection is established.
func (h *Handler) SetClient(c *server.Client) {
	h.client = c
}

// Initialize returns server capabilities. We explicitly advertise full-document
// sync (SyncFull) so that every textDocument/didChange notification carries the
// complete file text. The library default is SyncIncremental, which would send
// only diffs — our analysis pipeline needs the whole source every time.
func (h *Handler) Initialize(_ context.Context, _ *lsp.InitializeParams) (*lsp.InitializeResult, error) {
	openClose := true
	includeText := true
	return &lsp.InitializeResult{
		ServerInfo: &lsp.ServerInfo{Name: "lyra-lsp", Version: "0.1.0"},
		Capabilities: lsp.ServerCapabilities{
			TextDocumentSync: &lsp.TextDocumentSyncOptions{
				OpenClose: &openClose,
				Change:    lsp.SyncFull,
				Save:      &lsp.SaveOptions{IncludeText: &includeText},
			},
		},
	}, nil
}

func (h *Handler) Shutdown(_ context.Context) error { return nil }

// DidOpen runs the analysis pipeline when a .lyra file is opened.
func (h *Handler) DidOpen(ctx context.Context, params *lsp.DidOpenTextDocumentParams) error {
	return h.analyze(ctx, params.TextDocument.URI, params.TextDocument.Text)
}

// DidChange re-runs the analysis pipeline on every edit (full-sync mode).
func (h *Handler) DidChange(ctx context.Context, params *lsp.DidChangeTextDocumentParams) error {
	if len(params.ContentChanges) == 0 {
		return nil
	}
	// Full sync: the last (only) change carries the complete document text.
	return h.analyze(ctx, params.TextDocument.URI, params.ContentChanges[len(params.ContentChanges)-1].Text)
}

// DidSave re-runs analysis when the file is saved. With SyncFull the
// didChange notifications already carry the full text on every keystroke, but
// DidSave is a useful belt-and-suspenders for editors that batch changes.
func (h *Handler) DidSave(ctx context.Context, params *lsp.DidSaveTextDocumentParams) error {
	if params.Text != nil {
		return h.analyze(ctx, params.TextDocument.URI, *params.Text)
	}
	return nil
}

// DidClose clears diagnostics when the file is closed.
func (h *Handler) DidClose(ctx context.Context, params *lsp.DidCloseTextDocumentParams) error {
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

	log.Printf("analyze: collecting")
	c := collector.NewCollector([]byte(source))
	program, symTable, collectorErrors := c.Collect(tree.RootNode())
	log.Printf("analyze: collect done (%d errors)", len(collectorErrors))

	for _, rawErr := range collectorErrors {
		ce, ok := rawErr.(collector.CollectorError)
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
			Severity: &sev,
			Source:   "lyra",
			Message:  ce.Message,
		})
	}

	log.Printf("analyze: typechecking")
	tt := typetable.New()
	tc := typechecker.New(symTable, tt)
	for _, te := range tc.Check(program) {
		sev := severityFromTypechecker(te.Severity)
		loc := te.Location
		diags = append(diags, lsp.Diagnostic{
			Range: lsp.Range{
				Start: lsp.Position{Line: lspPos(loc.StartLine), Character: lspPos(loc.StartCol)},
				End:   lsp.Position{Line: lspPos(loc.EndLine), Character: lspPos(loc.EndCol)},
			},
			Severity: &sev,
			Source:   "lyra",
			Message:  te.Message,
		})
	}

	log.Printf("analyze: publishing %d diagnostics", len(diags))
	return h.client.PublishDiagnostics(ctx, &lsp.PublishDiagnosticsParams{
		URI:         uri,
		Diagnostics: diags,
	})
}

// lspPos converts a 1-based ast.Location line/column to a 0-based LSP position,
// clamping at zero to guard against uninitialized (zero-value) locations.
func lspPos(oneBased int) int {
	if oneBased <= 0 {
		return 0
	}
	return oneBased - 1
}

func severityFromCollector(s collector.CollectorErrorSeverity) lsp.DiagnosticSeverity {
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

func main() {
	logFile, err := os.OpenFile("/tmp/lyra-lsp.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err == nil {
		log.SetOutput(logFile)
		defer logFile.Close()
	}
	log.Println("lyra-lsp started")

	srv := server.NewServer(&Handler{})
	if err := srv.Run(context.Background(), server.RunStdio()); err != nil {
		log.Printf("server exited: %v", err)
	}
}
