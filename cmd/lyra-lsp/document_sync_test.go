package main

import (
	"context"
	"testing"
	"time"

	"github.com/owenrumney/go-lsp/lsp"
	"github.com/owenrumney/go-lsp/servertest"
)

// --- posToOffset / applyEdit unit tests -------------------------------------
//
// LSP "character" is a count of UTF-16 code units, not bytes. These tests pin
// that behavior, including the non-ASCII cases where a byte-based conversion
// (the previous implementation) would land at the wrong offset and corrupt the
// edit.

func TestPosToOffset_ASCII(t *testing.T) {
	const text = "ab\ncd\nef" // lines "ab", "cd", "ef"
	cases := []struct {
		line, char int
		want       int
	}{
		{0, 0, 0}, // start of file
		{0, 1, 1}, // within first line
		{0, 2, 2}, // end of first line (before '\n')
		{0, 9, 2}, // char past line end clamps to line end, not into next line
		{1, 0, 3}, // start of second line (after first '\n')
		{1, 2, 5}, // end of second line
		{2, 1, 7}, // within third line
		{2, 9, 8}, // past last line end clamps to len(text)
		{9, 0, 8}, // line past EOF clamps to len(text)
	}
	for _, c := range cases {
		if got := posToOffset(text, c.line, c.char); got != c.want {
			t.Errorf("posToOffset(%q, %d, %d) = %d, want %d", text, c.line, c.char, got, c.want)
		}
	}
}

func TestPosToOffset_MultiByte(t *testing.T) {
	// "hé" — é (U+00E9) is one UTF-16 code unit but two UTF-8 bytes.
	// "a😀b" — 😀 (U+1F600) is a surrogate pair (two UTF-16 units) and four bytes.
	cases := []struct {
		name       string
		text       string
		char, want int
	}{
		{"before accent", "héllo", 1, 1},      // after 'h'
		{"after accent", "héllo", 2, 3},       // after 'é' (2 bytes): byte offset 3
		{"after accent+l", "héllo", 3, 4},     // h(1)+é(2)+l(1)
		{"before emoji", "a😀b", 1, 1},         // after 'a'
		{"after emoji", "a😀b", 3, 5},          // a(1 unit) + 😀(2 units) → byte 5
		{"mid surrogate clamps", "a😀b", 2, 5}, // char inside the pair consumes the whole rune
	}
	for _, c := range cases {
		if got := posToOffset(c.text, 0, c.char); got != c.want {
			t.Errorf("%s: posToOffset(%q, 0, %d) = %d, want %d", c.name, c.text, c.char, got, c.want)
		}
	}
}

func TestApplyEdit(t *testing.T) {
	cases := []struct {
		name           string
		src            string
		sl, sc, el, ec int
		text, want     string
	}{
		{"insert", "ab", 0, 1, 0, 1, "X", "aXb"},
		{"replace within line", "abc", 0, 0, 0, 2, "XY", "XYc"},
		{"delete within line", "abc", 0, 1, 0, 2, "", "ac"},
		{"replace across lines", "ab\ncd", 0, 1, 1, 1, "X", "aXd"},
		{"append past line end", "ab\ncd", 0, 2, 0, 2, "Z", "abZ\ncd"},
		// The regression: replacing "world" in a line that begins with a
		// multi-byte character. With byte-based offsets é's extra byte shifts
		// every following column, so the splice lands one byte early and
		// mangles the text ("héllothered"); UTF-16 offsets get it right.
		{"non-ascii line replace", "héllo world", 0, 6, 0, 11, "there", "héllo there"},
	}
	for _, c := range cases {
		r := lsp.Range{
			Start: lsp.Position{Line: c.sl, Character: c.sc},
			End:   lsp.Position{Line: c.el, Character: c.ec},
		}
		if got := applyEdit(c.src, r, c.text); got != c.want {
			t.Errorf("%s: applyEdit(%q, %v, %q) = %q, want %q", c.name, c.src, r, c.text, got, c.want)
		}
	}
}

func TestApplyEdit_MalformedRangeDoesNotPanic(t *testing.T) {
	// End before start would panic a naive slice; applyEdit degrades to an
	// empty edit at start instead of crashing the server.
	r := lsp.Range{
		Start: lsp.Position{Line: 0, Character: 3},
		End:   lsp.Position{Line: 0, Character: 1},
	}
	if got := applyEdit("abcdef", r, "X"); got != "abcXdef" {
		t.Errorf("applyEdit with reversed range = %q, want %q", got, "abcXdef")
	}
}

// --- document-sync lifecycle integration tests ------------------------------

// newSyncHarness returns a running harness plus the concrete handler behind it,
// so tests can inspect the internal document store directly.
func newSyncHarness(t *testing.T) (*servertest.Harness, *Handler) {
	t.Helper()
	handler := newHandler()
	return servertest.New(t, handler), handler
}

// docText reads the current stored text for a URI under the handler's lock.
func docText(t *testing.T, h *Handler, uri string) (string, bool) {
	t.Helper()
	h.mu.Lock()
	defer h.mu.Unlock()
	s, ok := h.docStore[uri]
	return s, ok
}

// notifyAndWait sends a notification and blocks until the resulting analysis
// publishes diagnostics, so the document store is guaranteed updated. Clearing
// first makes WaitForDiagnostics block for the *next* publish rather than
// returning a stale one.
func notifyAndWait(t *testing.T, h *servertest.Harness, method string, params any) {
	t.Helper()
	h.ClearDiagnostics()
	if err := h.Notify(method, params); err != nil {
		t.Fatalf("Notify %s: %v", method, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := h.WaitForDiagnostics(ctx, testURI); err != nil {
		t.Fatalf("WaitForDiagnostics after %s: %v", method, err)
	}
}

func rangedChange(sl, sc, el, ec int, text string) *lsp.DidChangeTextDocumentParams {
	return &lsp.DidChangeTextDocumentParams{
		TextDocument: lsp.VersionedTextDocumentIdentifier{
			TextDocumentIdentifier: lsp.TextDocumentIdentifier{URI: testURI},
			Version:                2,
		},
		ContentChanges: []lsp.TextDocumentContentChangeEvent{{
			Range: &lsp.Range{
				Start: lsp.Position{Line: sl, Character: sc},
				End:   lsp.Position{Line: el, Character: ec},
			},
			Text: text,
		}},
	}
}

func TestDidChange_IncrementalEdit(t *testing.T) {
	h, handler := newSyncHarness(t)
	openAndWait(t, h, "let x = 1")

	// Replace the "1" (line 0, chars 8..9) with "42".
	notifyAndWait(t, h, "textDocument/didChange", rangedChange(0, 8, 0, 9, "42"))

	got, ok := docText(t, handler, testURI)
	if !ok {
		t.Fatal("document missing from store after change")
	}
	if want := "let x = 42"; got != want {
		t.Errorf("docStore = %q, want %q", got, want)
	}
}

func TestDidChange_FullReplacement(t *testing.T) {
	h, handler := newSyncHarness(t)
	openAndWait(t, h, "let x = 1")

	// A change with no Range is a whole-document replacement.
	full := &lsp.DidChangeTextDocumentParams{
		TextDocument: lsp.VersionedTextDocumentIdentifier{
			TextDocumentIdentifier: lsp.TextDocumentIdentifier{URI: testURI},
			Version:                2,
		},
		ContentChanges: []lsp.TextDocumentContentChangeEvent{{Text: "let y = 2"}},
	}
	notifyAndWait(t, h, "textDocument/didChange", full)

	got, _ := docText(t, handler, testURI)
	if want := "let y = 2"; got != want {
		t.Errorf("docStore = %q, want %q", got, want)
	}
}

func TestDidChange_NonASCIILine(t *testing.T) {
	// End-to-end version of the UTF-16 regression: an edit after a multi-byte
	// character on the same line must splice at the right place.
	h, handler := newSyncHarness(t)
	openAndWait(t, h, "héllo world")

	notifyAndWait(t, h, "textDocument/didChange", rangedChange(0, 6, 0, 11, "there"))

	got, _ := docText(t, handler, testURI)
	if want := "héllo there"; got != want {
		t.Errorf("docStore = %q, want %q", got, want)
	}
}

func TestDidChange_EmptyChangesIsNoOp(t *testing.T) {
	h, handler := newSyncHarness(t)
	openAndWait(t, h, "let x = 1")

	// A didChange with no content changes returns early (no store write, no
	// analysis). Sending a didSave afterward gives us a publish to wait on,
	// and notifications are processed in order, so once it lands the empty
	// change has already been handled.
	empty := &lsp.DidChangeTextDocumentParams{
		TextDocument: lsp.VersionedTextDocumentIdentifier{
			TextDocumentIdentifier: lsp.TextDocumentIdentifier{URI: testURI},
			Version:                2,
		},
	}
	if err := h.Notify("textDocument/didChange", empty); err != nil {
		t.Fatalf("Notify empty didChange: %v", err)
	}
	notifyAndWait(t, h, "textDocument/didSave", &lsp.DidSaveTextDocumentParams{
		TextDocument: lsp.TextDocumentIdentifier{URI: testURI},
	})

	got, _ := docText(t, handler, testURI)
	if want := "let x = 1"; got != want {
		t.Errorf("docStore = %q, want %q (empty change should not mutate)", got, want)
	}
}

func TestDidSave_ReanalyzesAndKeepsDocument(t *testing.T) {
	h, handler := newSyncHarness(t)
	openAndWait(t, h, "let x = 1")

	notifyAndWait(t, h, "textDocument/didSave", &lsp.DidSaveTextDocumentParams{
		TextDocument: lsp.TextDocumentIdentifier{URI: testURI},
	})

	if _, ok := docText(t, handler, testURI); !ok {
		t.Error("document should remain in store after save")
	}
}

func TestDidClose_RemovesDocumentAndClearsDiagnostics(t *testing.T) {
	// Use a source with a diagnostic so we can observe it being cleared.
	h, handler := newSyncHarness(t)
	openAndWait(t, h, "let x: i32 = \"nope\"")

	notifyAndWait(t, h, "textDocument/didClose", &lsp.DidCloseTextDocumentParams{
		TextDocument: lsp.TextDocumentIdentifier{URI: testURI},
	})

	if _, ok := docText(t, handler, testURI); ok {
		t.Error("document should be removed from store after close")
	}
	if diags := h.Diagnostics(testURI); len(diags) != 0 {
		t.Errorf("close should clear diagnostics, got %d", len(diags))
	}
}
