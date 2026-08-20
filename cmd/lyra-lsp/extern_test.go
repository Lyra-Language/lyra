package main

import (
	"testing"

	"github.com/owenrumney/go-lsp/lsp"
	"github.com/owenrumney/go-lsp/servertest"
)

// The editor's view of an `extern`.
//
// Every one of these was missing before 08/20, and for one reason: `ExternDeclStmt` was
// added to the AST on 08/18 and each of this server's switches over top-level declaration
// kinds was written before it existed. Nothing surfaced them because they are all
// editor-facing — an extern was simply absent from the outline, the symbol index and the
// highlighting, which reads as "the editor does not know about that yet" rather than as a
// bug. See the hazard-8 note in CLAUDE.md.
//
// `externSrc` puts an extern between two ordinary declarations, so a test asserting the
// extern is present also fails if it displaced a neighbour.
const externSrc = `
let before = () -> i64 => 1
@link("m")
unsafe extern pure sqrt: (f64) -> f64
let after = () -> i64 => 2`

func TestDocumentSymbol_Extern(t *testing.T) {
	h := servertest.New(t, newHandler())
	openAndWait(t, h, externSrc)
	syms, err := h.DocumentSymbol(testURI)
	if err != nil {
		t.Fatalf("DocumentSymbol: %v", err)
	}
	var found *lsp.DocumentSymbol
	for i := range syms {
		if syms[i].Name == "sqrt" {
			found = &syms[i]
		}
	}
	if found == nil {
		t.Fatalf("no `sqrt` in the outline; got %v", symbolNames(syms))
	}
	if found.Kind != lsp.SymbolKindFunction {
		t.Errorf("kind = %v; want Function", found.Kind)
	}
	// **The selection range is the name, not the declaration's start.** An extern's
	// span begins at its `@link` line, so measuring len("sqrt") from there would
	// highlight `@lin` — which is what writing this case like its three neighbours
	// produces, and why NameLocation is used instead.
	if found.SelectionRange.Start.Line != 3 {
		t.Errorf("selection on line %d; want 3 (the `unsafe extern` line, not `@link`)",
			found.SelectionRange.Start.Line)
	}
	if got := found.SelectionRange.End.Character - found.SelectionRange.Start.Character; got != len("sqrt") {
		t.Errorf("selection spans %d characters; want %d", got, len("sqrt"))
	}
}

func TestWorkspaceSymbol_Extern(t *testing.T) {
	h := servertest.New(t, newHandler())
	openAndWait(t, h, externSrc)
	syms, err := h.WorkspaceSymbol("sqrt")
	if err != nil {
		t.Fatalf("WorkspaceSymbol: %v", err)
	}
	if len(syms) == 0 {
		t.Fatal("workspace symbol search found no `sqrt`")
	}
	if syms[0].Kind != lsp.SymbolKindFunction {
		t.Errorf("kind = %v; want Function", syms[0].Kind)
	}
	if syms[0].Location.Range.Start.Line != 3 {
		t.Errorf("located on line %d; want 3 (the name, not the `@link` line)",
			syms[0].Location.Range.Start.Line)
	}
}

// Go-to-definition on a *usage* lands on the extern's name. It worked before only in the
// sense that it returned something: the declaration's own start is the `@link` line, so
// the jump landed above the symbol. `namedNameLoc` is the one answer to "where is this
// declaration's name", and definition now asks it rather than keeping a second copy.
func TestDefinition_Extern(t *testing.T) {
	h := servertest.New(t, newHandler())
	src := `
@link("m")
unsafe extern pure sqrt: (f64) -> f64
let use = () -> f64 => unsafe { sqrt(4.0) }`
	openAndWait(t, h, src)
	// Inside the `unsafe` block, which is the only place a call to an extern can be:
	// navigation was blind in there until the same day (hover.go's findExprInExpr).
	locs, err := h.Definition(testURI, 3, 32) // on `sqrt` in the call
	if err != nil {
		t.Fatalf("Definition: %v", err)
	}
	if len(locs) != 1 {
		t.Fatalf("expected 1 location, got %d", len(locs))
	}
	if locs[0].Range.Start.Line != 2 {
		t.Errorf("jumped to line %d; want 2 (`unsafe extern pure sqrt`, not `@link`)",
			locs[0].Range.Start.Line)
	}
}

// **Renaming an extern is declined**, and that is the design rather than a gap: its name
// *is* the C symbol, so the other half of the declaration lives in a library this
// compiler did not build. Renaming the Lyra side would emit `declare @newName` for a
// symbol nobody defines — a link error naming something the source no longer contains.
//
// Before 08/20 it was worse than declined. `namedNameLoc` had no extern case, so the
// anchor fell back to the declaration's *start*, and renaming a usage spliced the new
// name over the first characters of the `@link` line.
func TestRename_ExternIsDeclined(t *testing.T) {
	h := servertest.New(t, newHandler())
	src := `
@link("m")
unsafe extern pure sqrt: (f64) -> f64
let use = () -> f64 => unsafe { sqrt(4.0) }`
	openAndWait(t, h, src)

	for _, at := range []struct {
		what       string
		line, char int
	}{
		{"the declaration", 2, 19},
		{"a usage", 3, 33},
	} {
		edit, err := h.Rename(testURI, at.line, at.char, "squareRoot")
		if err == nil && edit != nil && len(edit.Changes) > 0 {
			t.Errorf("rename at %s produced edits; an extern's name is the C symbol and must decline", at.what)
		}
		prep, err := h.PrepareRename(testURI, at.line, at.char)
		if err == nil && prep != nil {
			t.Errorf("prepareRename at %s offered a rename; want it declined", at.what)
		}
	}
}

// An extern's name is highlighted as a function, at its declaration *and* at every
// reference — two switches in semantictokens.go, and both needed the case.
func TestSemanticTokens_Extern(t *testing.T) {
	h := servertest.New(t, newHandler())
	src := `
@link("m")
unsafe extern pure sqrt: (f64) -> f64
let use = () -> f64 => unsafe { sqrt(4.0) }`
	openAndWait(t, h, src)
	toks, err := h.SemanticTokensFull(testURI)
	if err != nil {
		t.Fatalf("SemanticTokensFull: %v", err)
	}
	// The encoding is [deltaLine, deltaStart, length, type, modifiers] per token; a
	// token of length 4 typed as a function is `sqrt`, and there must be two of them.
	n := 0
	for i := 0; i+4 < len(toks.Data); i += 5 {
		if toks.Data[i+2] == len("sqrt") && toks.Data[i+3] == semTypeFunction {
			n++
		}
	}
	if n != 2 {
		t.Errorf("got %d function-typed `sqrt` tokens; want 2 (the declaration and the call)", n)
	}
}

// **Navigation works inside an `unsafe` block.** `findExprAtPos` — which hover,
// definition, rename and document-highlight all start from — had no case for
// UnsafeBlockExpr, so every one of them returned nothing in there. That is the whole of a
// program's FFI and raw-pointer code, since both require the block.
//
// Not an extern bug, but found by one: a call to an extern needs an `unsafe` context, so
// there is no way to test go-to-definition on one without going through this.
func TestNavigation_InsideAnUnsafeBlock(t *testing.T) {
	h := servertest.New(t, newHandler())
	src := `
let helper = pure (n: i64) -> i64 => n
let main = () -> void => {
  var x: i64 = 5
  unsafe {
    let p = &mut x
    p^ = helper(7)
  }
}`
	openAndWait(t, h, src)

	// `helper` in the call on line 6, inside two levels of block.
	locs, err := h.Definition(testURI, 6, 10)
	if err != nil {
		t.Fatalf("Definition: %v", err)
	}
	if len(locs) != 1 || locs[0].Range.Start.Line != 1 {
		t.Errorf("definition inside `unsafe` = %v; want one location on line 1", locs)
	}

	// And a binding declared *inside* the block resolves there too, which is the
	// scope half of the same question (definition.go's scopeInExpr).
	locs, err = h.Definition(testURI, 6, 4) // `p` in `p^ = …`
	if err != nil {
		t.Fatalf("Definition: %v", err)
	}
	if len(locs) != 1 || locs[0].Range.Start.Line != 5 {
		t.Errorf("definition of an unsafe-block binding = %v; want one location on line 5", locs)
	}
}
