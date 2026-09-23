package main

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/owenrumney/go-lsp/lsp"
	"github.com/owenrumney/go-lsp/servertest"
)

// **A related location is published against its own file**, which for the diagnostic that
// has one is almost always a different file from the open document.
//
// `lyra-W001` carries "previously declared here", and the shadowed name is usually the
// prelude's: `let index = …` shadows `std/prelude/strings.lyra`'s `index`. Until 09/23 every
// related entry was published under the *current* document's URI, with its range converted
// against the current document's text — so the link opened this file at the prelude's line
// number, which an editor clamps to the end of a shorter file. It went somewhere, which is
// why it read as a jump to nowhere rather than as a missing feature.
//
// The fix reaches for `sourceOf`, the resolver `locationIn` already uses for
// go-to-definition: "turn an ast.Location into an LSP Location" had one answer, and this
// was a second copy of it that dropped the file.
func TestDiagnostics_RelatedInformationPointsAtTheShadowedFile(t *testing.T) {
	t.Setenv("LYRA_STD", stdRootDir(t))
	h := servertest.New(t, newHandler())
	dir := t.TempDir()
	// `index` is the prelude's own — a shadow of another file's declaration, and short
	// enough that the prelude's line number is past the end of this file, which is the
	// shape that made the old behaviour visible.
	src := "let f = pure (n: i64) -> i64 => {\n  let index = n + 1\n  index\n}\n"
	uri := openFileAndWait(t, h, dir, "app.lyra", src)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	diags, err := h.WaitForDiagnostics(ctx, uri)
	if err != nil {
		t.Fatalf("WaitForDiagnostics: %v", err)
	}
	var related []lsp.DiagnosticRelatedInformation
	for _, d := range diags {
		if strings.Contains(d.Message, "shadows a variable declared in an outer scope") {
			related = d.RelatedInformation
		}
	}
	if len(related) == 0 {
		t.Fatalf("want a shadowing warning carrying its prior declaration; got %v", diags)
	}
	target := uriToPath(string(related[0].Location.URI))
	if filepath.Base(target) != "strings.lyra" {
		t.Errorf("the link points at %s; want the prelude file declaring `index`", target)
	}
	if target == filepath.Join(dir, "app.lyra") {
		t.Errorf("the link points back at the open document, which is the bug itself")
	}
	// And the range is that file's, not this one's: `app.lyra` is four lines long, so a
	// line number from the prelude is proof the range was converted against the right text.
	if line := related[0].Location.Range.Start.Line; line < 10 {
		t.Errorf("the related range starts at line %d, which is inside the open document; "+
			"want the prelude's own line", line)
	}
}
