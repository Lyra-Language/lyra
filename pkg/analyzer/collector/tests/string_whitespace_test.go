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
