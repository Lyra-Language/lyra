package main

import (
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/owenrumney/go-lsp/lsp"

	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/ast/symbols"
	diag "github.com/Lyra-Language/lyra/pkg/diagnostic"
	"github.com/Lyra-Language/lyra/pkg/driver"
	"github.com/Lyra-Language/lyra/pkg/modules"
)

// This file gives the language server the same view of a program the compiler has:
// the document's whole import graph, prelude included, rather than the single buffer.
//
// Analyzing one unit in isolation is not a smaller version of the real thing — it is a
// different program. There is no prelude in it, so `Maybe`, `Some`, `Ok` and every
// other name the standard library provides is undefined, and the editor reported them
// as errors (`undefined tuple type "Some"`) on files `lyrac check` compiled cleanly.
// An unresolved import did the same for a program's own modules.
//
// Two things separate this from what lyrac does, and both are properties of an editor
// rather than of the pipeline:
//
//   - **The buffer is not the file.** Every open document is passed as an overlay, so
//     analysis sees what the user is looking at — including a file that has never been
//     saved, which has no on-disk content at all.
//   - **Only this document's half of the result may be published.** The program now
//     spans several files; diagnostics and the statement list are filtered back down to
//     the one being served, or the prelude's own warnings would land on the user's
//     lines (see docProgram / diagnosticsFor).

// analyzeDocument resolves uri's import graph and runs the front-end over every unit.
// It returns the result and the document's filesystem path (empty when the URI is not
// a file, e.g. an unsaved "untitled" buffer, in which case the single-unit pipeline is
// used and there is nothing to filter against).
func (h *Handler) analyzeDocument(uri lsp.DocumentURI, source string) (*driver.Result, string) {
	path := uriToPath(string(uri))
	if path == "" {
		return driver.Analyze([]byte(source)), ""
	}

	opts := modules.DefaultOptions()
	opts.Overlay = h.overlay(uri, source)
	units, diags := modules.Resolve(path, modules.DefaultRoots(path), opts)
	if len(units) == 0 {
		// The entry file itself could not be loaded or parsed. Fall back to the
		// single-unit pipeline so the buffer still gets whatever analysis it can
		// support: a server that returns nothing here goes silent exactly when the
		// user most needs a diagnostic.
		res := driver.Analyze([]byte(source))
		res.Diagnostics = append(diags, res.Diagnostics...)
		return res, path
	}

	res := driver.AnalyzeUnits(units)
	// Resolver diagnostics come first: an unreadable import explains the errors that
	// follow from the names it failed to provide.
	res.Diagnostics = append(diags, res.Diagnostics...)
	return res, path
}

// overlay is every open document keyed by filesystem path, with uri's text taken from
// the caller rather than the store — analyze runs on the text it was handed, which
// during a didChange is newer than what the store held when the lock was last taken.
func (h *Handler) overlay(uri lsp.DocumentURI, source string) map[string][]byte {
	h.mu.Lock()
	defer h.mu.Unlock()

	out := make(map[string][]byte, len(h.docStore)+1)
	for docURI, text := range h.docStore {
		if path := uriToPath(docURI); path != "" {
			out[path] = []byte(text)
		}
	}
	if path := uriToPath(string(uri)); path != "" {
		out[path] = []byte(source)
	}
	return out
}

// docProgram narrows a whole-program AST to the statements that came from file. Every
// position-based handler (hover, definition, document symbols, semantic tokens, …)
// walks this, and they must not see another unit's nodes: a line and column alone do
// not say which file they belong to, so the prelude's line 40 would answer a request
// about the user's line 40.
//
// A unit with no file — the single-unit fallback above — is kept, since there is
// nothing else it could be.
func docProgram(program *ast.Program, file string) *ast.Program {
	if program == nil || file == "" {
		return program
	}
	out := &ast.Program{AstBase: program.AstBase}
	for _, stmt := range program.Statements {
		if stmt != nil && sameFile(stmt.GetLocation().File, file) {
			out.Statements = append(out.Statements, stmt)
		}
	}
	return out
}

// moduleScopeOf returns the scope of the module file declares, or nil when it declares
// none (the unnamed entry module, whose scope docAnalysis.fileScope falls back to). The
// scope is looked up rather than created: asking for one that does not exist would
// conjure an empty scope in which nothing resolves.
func moduleScopeOf(symTable *symbols.SymbolTable, file string) *symbols.Scope {
	if symTable == nil || file == "" {
		return nil
	}
	module := symTable.ModuleOfFile[file]
	if module == "" {
		return nil
	}
	return symTable.ModuleScopes[module]
}

// diagnosticsFor selects the diagnostics belonging to file, for the same reason
// docProgram narrows the AST: they are published against one document's URI, so
// another unit's would appear on this document's lines.
//
// A diagnostic naming no file is kept. Those are program-level (a failed resolve, an
// internal error) and the document being served is the only place to show them.
func diagnosticsFor(diags []diag.Diagnostic, file string) []diag.Diagnostic {
	if file == "" {
		return diags
	}
	out := make([]diag.Diagnostic, 0, len(diags))
	for _, d := range diags {
		// Diagnostic.File is stamped by the driver for parse errors and by the
		// resolver for import failures; everything derived from an AST node carries
		// its file on the location instead.
		in := d.File
		if in == "" {
			in = d.Location.File
		}
		if in == "" || sameFile(in, file) {
			out = append(out, d)
		}
	}
	return out
}

// sameFile compares two filesystem paths as the resolver produces them. Both sides come
// from the same construction (an entry path, or a root joined with a module's relative
// path), so cleaning is enough — no symlink resolution, which would touch the disk on
// every diagnostic.
func sameFile(a, b string) bool {
	return a == b || filepath.Clean(a) == filepath.Clean(b)
}

// uriToPath converts a `file://` URI to a filesystem path, returning "" for anything
// else. A non-file URI is not an error: an editor gives an unsaved buffer an
// `untitled:` URI, which has no path to resolve modules against and is analyzed on its
// own.
func uriToPath(uri string) string {
	u, err := url.Parse(uri)
	if err != nil || u.Scheme != "file" {
		return ""
	}
	path, err := url.PathUnescape(u.Path)
	if err != nil {
		return ""
	}
	// A Windows URI is `file:///C:/…`; url.Parse leaves the leading slash on, and it
	// has to come off before the path is usable.
	if len(path) > 2 && path[0] == '/' && path[2] == ':' {
		path = path[1:]
	}
	return filepath.Clean(path)
}

// pathToURI is the inverse, for a definition that resolves into a file the client has
// not opened (a prelude declaration, another module of the program).
func pathToURI(path string) string {
	u := url.URL{Scheme: "file", Path: filepath.ToSlash(path)}
	if !strings.HasPrefix(u.Path, "/") {
		u.Path = "/" + u.Path
	}
	return u.String()
}

// sourceOf returns the text of an arbitrary file for position conversion: the open
// buffer if the client has one, otherwise what is on disk. The second result is false
// when neither is available, in which case a location in that file cannot be mapped to
// LSP coordinates and is better dropped than reported at the wrong place.
func (h *Handler) sourceOf(file string) (uri string, source string, ok bool) {
	uri = pathToURI(file)
	h.mu.Lock()
	text, open := h.docStore[uri]
	h.mu.Unlock()
	if open {
		return uri, text, true
	}
	b, err := os.ReadFile(file)
	if err != nil {
		return "", "", false
	}
	return uri, string(b), true
}
