package main

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/owenrumney/go-lsp/lsp"
	"github.com/owenrumney/go-lsp/servertest"
)

// **A diagnostic outlives the server that published it.** The client holds the last set it
// was sent and has no reason to drop it when the connection ends, so a server exiting
// without clearing leaves the editor marking errors from a program that no longer exists —
// which reads as a compiler bug, since the editor and the compiler then disagree and the
// editor is what is being looked at.
//
// Asserted through the client, not by inspecting the handler: what matters is that a clearing
// notification is actually *sent*, and a test that checked internal state would pass on a
// server that computed the right answer and never delivered it.
func TestShutdown_ClearsPublishedDiagnostics(t *testing.T) {
	handler := newHandler()
	h := servertest.New(t, handler)
	// A program with a real error, so there is something to clear.
	src := `
let main = () -> void => println(undefinedName())`
	if err := h.DidOpen(testURI, "lyra", src); err != nil {
		t.Fatalf("DidOpen: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	first, err := h.WaitForDiagnostics(ctx, testURI)
	if err != nil {
		t.Fatalf("WaitForDiagnostics: %v", err)
	}
	if len(first) == 0 {
		t.Fatal("the fixture must produce a diagnostic, or this test proves nothing")
	}

	if err := handler.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}

	// The clearing notification travels over the connection, so poll for what the client
	// last received rather than assuming it has arrived.
	var after []lsp.Diagnostic
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		after = h.Diagnostics(testURI)
		if len(after) == 0 {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	var msgs []string
	for _, d := range after {
		msgs = append(msgs, d.Message)
	}
	t.Errorf("shutdown left %d diagnostic(s) on screen: %s — the client holds the last set "+
		"it was sent, so a server that exits without clearing leaves the editor marking a "+
		"program that no longer exists", len(after), strings.Join(msgs, "; "))
}
