package main

import (
	"log"
	"runtime/debug"

	lsp "github.com/owenrumney/go-lsp/lsp"
)

// The prologue every request handler opens with.
//
// Thirteen handlers spelled the same twenty lines: a deferred recover that logs and
// answers empty, a locked lookup of the document's analysis and source, and — for the
// nine that act on a cursor — the conversion of an LSP position into ast.Location's
// terms. Each copy was an opportunity to hold the mutex a line too long, or to forget
// that a missing document is an empty answer rather than an error.

// recoverHandler is the panic guard, deferred as a single line:
//
//	defer recoverHandler("definition", &result, &retErr)
//
// **An LSP server must not die on one bad request.** The editor would lose every other
// feature with it, and the user sees a language server that "stopped working" with nothing
// to report — so a handler that panics answers with its zero value and a nil error, which
// the client reads as "nothing here", and the stack goes to the log.
//
// Deferring it directly is what makes it work: recover() reports a panic only when called
// by the deferred function itself, and here that is this function rather than a closure
// wrapped around it. The named result and error are taken by pointer for the same reason
// the closures took them by capture — the zero value has to replace whatever the handler
// was midway through returning.
func recoverHandler[T any](name string, result *T, retErr *error) {
	if r := recover(); r != nil {
		log.Printf("%s panic: %v\n%s", name, r, debug.Stack())
		var zero T
		*result, *retErr = zero, nil
	}
}

// docFor returns a document's analysis and source text, and whether it has been analyzed
// at all — a document the server has not seen yet is an empty answer, never an error.
//
// The lock covers both maps together: they are written as a pair by the analysis pass, and
// a reader that took them separately could see an analysis from one edit beside the source
// of the next.
func (h *Handler) docFor(uri string) (*docAnalysis, string, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	analysis, ok := h.analysisStore[uri]
	return analysis, h.docStore[uri], ok
}

// docCursor is what a position-based handler works from: the document, and the cursor
// already in the compiler's coordinates.
type docCursor struct {
	uri      string
	analysis *docAnalysis
	source   string
	// line and col are ast.Location's convention — 1-based, and col counts bytes.
	line int
	col  int
}

// cursorAt looks up the document and converts an LSP position into it.
//
// The conversion is the reason this exists rather than each handler doing its own: LSP
// positions are 0-based with UTF-16 "characters", ast.Location is 1-based with byte
// columns, and the two disagree on every line containing a non-ASCII character. See
// position.go.
func (h *Handler) cursorAt(uri string, pos lsp.Position) (docCursor, bool) {
	analysis, source, ok := h.docFor(uri)
	if !ok {
		return docCursor{}, false
	}
	return docCursor{
		uri:      uri,
		analysis: analysis,
		source:   source,
		line:     pos.Line + 1,
		col:      byteColumn(source, pos.Line, pos.Character),
	}, true
}
