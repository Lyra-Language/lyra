package main

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/owenrumney/go-lsp/servertest"
)

// The end-to-end path for the shipped documentation: prelude source on disk → collector →
// symbol table → hover in an editor. The other hover tests declare their subject in the
// buffer, so this is the only one that would notice the prelude's docs being collected
// but never reaching a reader.
//
// It sets LYRA_STD itself. The server resolves the standard library beside its own
// executable, and a `go test` binary lives in a temp directory — so without this the
// prelude is simply absent and the test passes vacuously on a program that does not
// compile.
func TestHover_PreludeDocReachesTheEditor(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("resolving the std root: %v", err)
	}
	// The root is the directory *containing* `std/`, not `std/` itself.
	t.Setenv("LYRA_STD", root)

	h := servertest.New(t, newHandler())
	src := "let main = () -> void => {\n" +
		"  let n = \"42\".parse_i64().unwrap_or(0);\n" +
		"  println(n)\n" +
		"}\n"
	openAndWait(t, h, src)

	if diags := h.Diagnostics(testURI); len(diags) > 0 {
		t.Fatalf("the program should compile clean; got %d diagnostics, first: %s",
			len(diags), diags[0].Message)
	}

	// `unwrap_or` in the chain, 0-based line 1, col 28.
	hover, err := h.Hover(testURI, 1, 28)
	if err != nil {
		t.Fatalf("Hover: %v", err)
	}
	if hover == nil {
		t.Fatal("expected a hover result on a prelude function")
	}
	got := hover.Contents.Value
	if !strings.Contains(got, "fallback") {
		t.Errorf("the prelude's `unwrap_or` doc did not reach hover; got: %q", got)
	}
	// The method name of a UFCS call has no recorded type — the callee is synthesized by
	// the desugaring — so this position renders the documentation alone, with no
	// signature block above it. Hovering the receiver still shows the type.
	if strings.Contains(got, "```") {
		t.Errorf("expected documentation without a signature block; got: %q", got)
	}
}
