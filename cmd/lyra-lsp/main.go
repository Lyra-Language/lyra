package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"runtime/debug"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/owenrumney/go-lsp/lsp"
	"github.com/owenrumney/go-lsp/server"

	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/ast/symbols"
	diag "github.com/Lyra-Language/lyra/pkg/diagnostic"
	"github.com/Lyra-Language/lyra/pkg/driver"
	"github.com/Lyra-Language/lyra/pkg/modules"
	"github.com/Lyra-Language/lyra/pkg/typetable"
)

// docAnalysis holds the full analysis result for one open document.
//
// The tables span the document's whole import graph — that is the point of resolving
// units (see units.go), and it is what lets a name declared in the prelude or another
// module resolve at all. `program`, by contrast, holds only *this* document's top-level
// statements, since every handler that walks it does so by source position.
type docAnalysis struct {
	program     *ast.Program
	symTable    *symbols.SymbolTable
	scopeTable  *symbols.ScopeTable
	typeTable   *typetable.TypeTable
	file        string         // filesystem path, "" for a buffer with no file
	moduleScope *symbols.Scope // the scope of the module this file declares
}

// fileScope is the scope a top-level position in this document resolves in. A file
// declaring `module util.math` puts its declarations in that module's scope, not the
// unnamed entry one, so asking the entry scope would find none of the file's own names.
func (a *docAnalysis) fileScope() *symbols.Scope {
	if a.moduleScope != nil {
		return a.moduleScope
	}
	return a.symTable.EntryScope()
}

// declLocation is a bare location in this document, for the symbol-table lookups that
// resolve a name *as the asking file sees it* (LookupTypeFrom, UFCSCallable, …). Which
// declaration a name means, and which module a file may reach, are both answered from
// `Location.File`, so a request originating in this document has to carry its path.
//
// Passing the zero Location was right only while the server analysed one file at a time
// with nothing stamped on it. Now that it resolves the whole import graph, an empty file
// names no module, and every such lookup would answer for the entry module instead of
// this one.
func (a *docAnalysis) declLocation() ast.Location {
	return ast.Location{File: a.file}
}

type Handler struct {
	client        *server.Client
	mu            sync.Mutex
	docStore      map[string]string       // URI → current full document text
	analysisStore map[string]*docAnalysis // URI → last successful analysis

	// parseCache survives across requests, which is the whole point: every keystroke
	// re-resolves the document's import graph and so re-parses every unit in it, and for a
	// small file with the standard prelude that is 12 files of which 11 cannot have
	// changed. It is keyed on file contents, so an edit invalidates exactly the file
	// edited and nothing else. Its own locking makes it safe to share across concurrent
	// requests.
	parseCache *modules.ParseCache

	// collectCache reuses the *collection* of every unit but the one being edited, which
	// is the other 75% of a keystroke: the parse cache stops at the syntax tree, and
	// collection folds all 12 units into one Program and SymbolTable every time. Together
	// they take a keystroke on a small file from 20.1 ms to 5.7 ms.
	collectCache *driver.CollectCache

	// rootPath is the workspace the client opened, from `initialize`. It is the search
	// root for a module's **importers**, which resolution cannot find: the graph runs
	// downward only (see importers.go). Empty when the client sent none, in which case
	// the document's own directory stands in — the same thing modules.DefaultRoots uses.
	rootPath string
}

func newHandler() *Handler {
	return &Handler{
		docStore:      make(map[string]string),
		analysisStore: make(map[string]*docAnalysis),
		parseCache:    modules.NewParseCache(),
		collectCache:  driver.NewCollectCache(),
	}
}

// initializeRoot is the workspace the client opened, preferring the first workspace
// folder over the deprecated rootUri and falling back to rootPath. Any of them may be
// absent — a client is entitled to open a single file with no workspace at all — and the
// empty answer is handled by falling back to the document's directory.
func initializeRoot(params *lsp.InitializeParams) string {
	if params == nil {
		return ""
	}
	if len(params.WorkspaceFolders) > 0 {
		if p := uriToPath(string(params.WorkspaceFolders[0].URI)); p != "" {
			return p
		}
	}
	if params.RootURI != nil {
		if p := uriToPath(string(*params.RootURI)); p != "" {
			return p
		}
	}
	if params.RootPath != nil && *params.RootPath != "" {
		return *params.RootPath
	}
	return ""
}

// SetClient is called by the server after the connection is established.
func (h *Handler) SetClient(c *server.Client) {
	h.client = c
}

// Initialize returns server capabilities. We use SyncIncremental so the client
// only sends changed ranges on each edit; the server applies them to its own
// document store and keeps the full text in memory.
func (h *Handler) Initialize(_ context.Context, params *lsp.InitializeParams) (*lsp.InitializeResult, error) {
	if root := initializeRoot(params); root != "" {
		h.mu.Lock()
		h.rootPath = root
		h.mu.Unlock()
		log.Printf("initialize: workspace root %s", root)
	}
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

// analyze runs the shared front-end pipeline over the document's whole import graph
// (analyzeDocument), persists the typed result for other requests, and publishes the
// diagnostics belonging to this document.
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

	// The whole import graph, not just this buffer — see units.go for why, and for
	// what has to be filtered back out afterwards.
	res, file := h.analyzeDocument(uri, source)

	own := diagnosticsFor(res.Diagnostics, file)
	diags := make([]lsp.Diagnostic, 0, len(own))
	for i := range own {
		diags = append(diags, diagToLSP(uri, source, own[i]))
	}

	// Persist the analysis for hover/definition/etc. Program is nil only on a
	// fatal parse failure; there is nothing to store then.
	if res.Program != nil {
		h.mu.Lock()
		h.analysisStore[string(uri)] = &docAnalysis{
			program:     docProgram(res.Program, file),
			symTable:    res.SymbolTable,
			scopeTable:  res.ScopeTable,
			typeTable:   res.TypeTable,
			file:        file,
			moduleScope: moduleScopeOf(res.SymbolTable, file),
		}
		h.mu.Unlock()
	}

	log.Printf("analyze: publishing %d diagnostics", len(diags))
	return h.client.PublishDiagnostics(ctx, &lsp.PublishDiagnosticsParams{
		URI:         uri,
		Diagnostics: diags,
	})
}

// applyEdit applies a single incremental LSP text change to src and returns
// the updated document. An LSP Range is a pair of 0-based (line, character)
// positions; posToOffset resolves each to a byte offset so the edit is a plain
// string splice.
func applyEdit(src string, r lsp.Range, newText string) string {
	start := posToOffset(src, r.Start.Line, r.Start.Character)
	end := posToOffset(src, r.End.Line, r.End.Character)
	if end < start {
		// A malformed range (end before start) would panic the slice; treat it
		// as an empty range at start rather than crashing the server.
		end = start
	}
	return src[:start] + newText + src[end:]
}

// posToOffset converts a 0-based LSP (line, character) position to a byte
// offset within text.
//
// LSP measures "character" in UTF-16 code units, not bytes: a BMP rune (é, 世)
// is one code unit but two or three UTF-8 bytes, and an astral rune (most
// emoji) is a surrogate pair — two code units — and four bytes. Advancing by
// bytes therefore corrupts any edit on a line containing non-ASCII text, so we
// walk runes and count UTF-16 units. A character past its line's end clamps to
// the line end (the LSP-documented behavior), and a line past the end of the
// text clamps to len(text).
func posToOffset(text string, line, char int) int {
	// Walk to the first byte of the target line.
	offset := 0
	for l := 0; l < line; l++ {
		nl := strings.IndexByte(text[offset:], '\n')
		if nl < 0 {
			return len(text) // fewer lines than requested
		}
		offset += nl + 1
	}
	// Consume `char` UTF-16 code units within the line, stopping at its end.
	for units := 0; units < char && offset < len(text) && text[offset] != '\n'; {
		r, size := utf8.DecodeRuneInString(text[offset:])
		units += utf16Len(r)
		offset += size
	}
	return offset
}

// utf16Len reports how many UTF-16 code units encode r: two for an astral-plane
// rune (a surrogate pair), one otherwise. The invalid-encoding rune (size 1) is
// treated as a single unit so a malformed byte still makes forward progress.
func utf16Len(r rune) int {
	if r > 0xFFFF {
		return 2
	}
	return 1
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
// source is the document text, needed to convert byte columns to UTF-16.
func diagToLSP(uri lsp.DocumentURI, source string, d diag.Diagnostic) lsp.Diagnostic {
	sev := lsp.DiagnosticSeverity(0)
	switch d.Severity {
	case diag.SeverityWarning:
		sev = lsp.SeverityWarning
	case diag.SeverityInfo:
		sev = lsp.SeverityInformation
	default:
		sev = lsp.SeverityError
	}
	return lsp.Diagnostic{
		Range:              locToRange(source, d.Location),
		Severity:           &sev,
		Code:               codeToLSP(d.Code),
		Source:             "lyra",
		Message:            d.Message,
		Tags:               tagsToLSP(d.Tags),
		RelatedInformation: toLSPRelatedInfo(uri, source, d.RelatedInformation),
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

func toLSPRelatedInfo(uri lsp.DocumentURI, source string, related []diag.RelatedInformation) []lsp.DiagnosticRelatedInformation {
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
				URI:   uri,
				Range: locToRange(source, r.Location),
			},
			Message: r.Message,
		})
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// Hover returns type information for the symbol under the cursor.
// The go-lsp library registers textDocument/hover automatically when this
// method is present on the handler.
func (h *Handler) Hover(_ context.Context, params *lsp.HoverParams) (result *lsp.Hover, retErr error) {
	defer recoverHandler("hover", &result, &retErr)

	c, ok := h.cursorAt(string(params.TextDocument.URI), params.Position)
	if !ok {
		return nil, nil
	}
	analysis, line, col := c.analysis, c.line, c.col

	// **A written type name is checked first here, and last in `Definition`**, and the
	// asymmetry has a reason rather than being an oversight. A type in a type position is
	// not an expression, so neither handler reaches it through `findExprAtPos` — but the
	// *enclosing* node is one: the cursor on `Point` in `(p: Point) -> i64` sits inside
	// the whole `LambdaExpr`, which has a recorded type, so hover would answer with the
	// function's signature and never fall through. `resolveDefinition` has no case for a
	// LambdaExpr and returns nil, so there the fallback is reached on its own.
	//
	// Checking first is safe because a type reference's span is a type *position*: a
	// struct literal's name is collected as an expression and never recorded here, so the
	// two sets do not overlap and nothing that already answered changes.
	if hov := hoverTypeReference(analysis, line, col); hov != nil {
		return hov, nil
	}

	// A **pattern**, checked first for the same reason: a constructor in a `match` arm is
	// not an expression, and the arm around it is — so falling through would answer with
	// whatever encloses the pattern rather than with the pattern.
	if hov := hoverPatternReference(analysis, line, col); hov != nil {
		return hov, nil
	}

	expr := findExprAtPos(analysis.program, line, col)
	if expr == nil {
		return nil, nil
	}

	doc := resolveDoc(expr, line, col, analysis)

	// A typeless expression may still be documented, and the case that matters is the
	// **method name of a UFCS call**. `desugarUFCSCall` rewrites `s.trim()` into
	// `trim(s)` with a synthesized callee at the method name's own location, and that
	// synthesized node never gets a recorded type — so bailing on the type alone made
	// hovering a method name answer nothing at all. That is the spelling the standard
	// library is written for, so it is the spelling whose documentation must show.
	//
	// Returning the doc without a signature is strictly better than the nil this
	// replaces, and cannot regress anything: every position that resolved a type before
	// still takes the branch below.
	typ, ok := analysis.typeTable.Get(expr)
	if !ok {
		if doc == nil {
			return nil, nil
		}
		return &lsp.Hover{
			Contents: lsp.MarkupContent{Kind: lsp.Markdown, Value: doc.Text},
		}, nil
	}

	content := renderHover(hoverContent(expr, typ), doc)
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
