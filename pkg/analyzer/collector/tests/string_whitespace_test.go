package collector_test

import (
	"testing"

	"github.com/Lyra-Language/lyra/pkg/ast"
)

// These assert *exact* string values, which the golden tests cannot: the golden
// comparator normalizes whitespace (trims lines, collapses space runs), so it's
// blind to leading/internal whitespace differences — exactly what the collector
// bug corrupted. tree-sitter strips a `string_content` chunk's leading whitespace
// as token padding, so the collector reconstructs literal chunks from the raw
// source between interpolations instead of from the (padding-stripped) nodes.

func plainStringValue(t *testing.T, src string) string {
	t.Helper()
	program, _, _, _ := parseAndCollect(t, src)
	decl := program.Statements[0].(*ast.VarDeclStmt)
	lit, ok := decl.Value.(*ast.StringLiteralExpr)
	if !ok {
		t.Fatalf("expected a plain StringLiteralExpr, got %T", decl.Value)
	}
	return lit.Value
}

func interpSegmentValues(t *testing.T, src string) []string {
	t.Helper()
	program, _, _, _ := parseAndCollect(t, src)
	decl := program.Statements[0].(*ast.VarDeclStmt)
	interp, ok := decl.Value.(*ast.InterpolatedStringExpr)
	if !ok {
		t.Fatalf("expected an InterpolatedStringExpr, got %T", decl.Value)
	}
	var out []string
	for _, seg := range interp.Segments {
		switch s := seg.(type) {
		case *ast.StringLiteralExpr:
			out = append(out, "lit:"+s.Value)
		case *ast.IdentifierExpr:
			out = append(out, "id:"+s.Name)
		default:
			out = append(out, "?")
		}
	}
	return out
}

// A plain string literal keeps its leading whitespace (this was dropped before —
// tree-sitter reported the content node starting after the spaces).
func TestCollect_PlainString_LeadingWhitespace(t *testing.T) {
	if got := plainStringValue(t, `let x = "  leading"`); got != "  leading" {
		t.Errorf("plain string value = %q, want %q", got, "  leading")
	}
}

// A single-space literal is a space, not empty (the sharpest form of the bug).
func TestCollect_PlainString_SingleSpace(t *testing.T) {
	if got := plainStringValue(t, `let x = " "`); got != " " {
		t.Errorf("single-space string value = %q, want %q", got, " ")
	}
}

// Whitespace on every side of an interpolation survives: the leading space of a
// chunk after a `}`, and a plain leading space after the opening quote.
func TestCollect_Interpolation_WhitespacePreserved(t *testing.T) {
	got := interpSegmentValues(t, `let x = " a ${n} b ${n} c "`)
	want := []string{"lit: a ", "id:n", "lit: b ", "id:n", "lit: c "}
	if len(got) != len(want) {
		t.Fatalf("segments = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("segment[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

// Adjacent interpolations produce no spurious empty literal chunk between them.
func TestCollect_Interpolation_NoEmptyChunks(t *testing.T) {
	got := interpSegmentValues(t, `let x = "${a}${b}"`)
	want := []string{"id:a", "id:b"}
	if len(got) != len(want) {
		t.Fatalf("segments = %v, want %v (no empty chunks expected)", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("segment[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

// ── comment delimiters are string content ─────────────────────────────────────
//
// The external scanner used to attempt a block-comment scan at every string
// content-chunk boundary (it ran before the in-string branch, and skipped
// leading whitespace as padding on the way). A string whose content began with
// `/*` therefore lexed as a *comment* running to the next `*/` anywhere later in
// the file — swallowing following declarations whole, with no diagnostic from
// any pass. Scanning is now gated on not being inside a string.

func TestCollect_String_BlockCommentOpenerIsContent(t *testing.T) {
	if got := plainStringValue(t, `let x = "/*"`); got != "/*" {
		t.Errorf("string value = %q, want %q", got, "/*")
	}
}

func TestCollect_String_BlockCommentCloserIsContent(t *testing.T) {
	if got := plainStringValue(t, `let x = "*/"`); got != "*/" {
		t.Errorf("string value = %q, want %q", got, "*/")
	}
}

// The whitespace-skipping path: a leading space then a comment opener. Both the
// space and the delimiters must survive.
func TestCollect_String_LeadingSpaceThenBlockComment(t *testing.T) {
	if got := plainStringValue(t, `let x = " /* note */ price"`); got != " /* note */ price" {
		t.Errorf("string value = %q, want %q", got, " /* note */ price")
	}
}

func TestCollect_String_LineCommentOpenerIsContent(t *testing.T) {
	if got := plainStringValue(t, `let x = "// not a comment"`); got != "// not a comment" {
		t.Errorf("string value = %q, want %q", got, "// not a comment")
	}
}

// A `/*` mid-content never started a chunk, so it always worked — pinned so the
// common shape (a glob, a path) can't regress either.
func TestCollect_String_CommentDelimitersMidContent(t *testing.T) {
	if got := plainStringValue(t, `let x = "path/*.txt"`); got != "path/*.txt" {
		t.Errorf("string value = %q, want %q", got, "path/*.txt")
	}
}

// The chunk right after an interpolation is the other boundary the scan fired at.
func TestCollect_Interpolation_CommentOpenersAreContent(t *testing.T) {
	got := interpSegmentValues(t, `let x = "${a} /* hi */ y"`)
	want := []string{"id:a", "lit: /* hi */ y"}
	if len(got) != len(want) {
		t.Fatalf("segments = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("segment[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

// The declaration *after* a string containing `/*` must still exist: the sharpest
// form of the bug swallowed it (and everything up to the next `*/`) as a comment.
func TestCollect_String_CommentOpenerDoesNotSwallowFollowingCode(t *testing.T) {
	program, _, _, _ := parseAndCollect(t, "let open = \"/*\"\nlet close = \"*/\"\nlet after = 1")
	if len(program.Statements) != 3 {
		t.Fatalf("expected 3 declarations, got %d (a string's `/*` swallowed following code)", len(program.Statements))
	}
	for i, want := range []string{"open", "close", "after"} {
		decl, ok := program.Statements[i].(*ast.VarDeclStmt)
		if !ok {
			t.Fatalf("statement %d: expected VarDeclStmt, got %T", i, program.Statements[i])
		}
		if decl.Name != want {
			t.Errorf("statement %d name = %q, want %q", i, decl.Name, want)
		}
	}
}
