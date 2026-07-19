package main

import (
	"testing"

	"github.com/owenrumney/go-lsp/lsp"
	"github.com/owenrumney/go-lsp/servertest"

	"github.com/Lyra-Language/lyra/pkg/ast"
)

// These tests pin the byte<->UTF-16 column conversions that reconcile
// ast.Location (1-based byte columns) with LSP positions (0-based UTF-16 code
// units). For ASCII text the two are identical; the interesting cases are lines
// containing multi-byte runes (é = 2 bytes / 1 unit; 😀 = 4 bytes / 2 units).

func TestLineStartByte(t *testing.T) {
	const src = "ab\ncdé\nef" // bytes: a0 b1 \n2 c3 d4 é5-6 \n7 e8 f9
	cases := []struct{ line, want int }{
		{0, 0},
		{1, 3},
		{2, 8},
		{5, len(src)}, // past EOF clamps to len
	}
	for _, c := range cases {
		if got := lineStartByte(src, c.line); got != c.want {
			t.Errorf("lineStartByte(%q, %d) = %d, want %d", src, c.line, got, c.want)
		}
	}
}

func TestUtf16Column(t *testing.T) {
	cases := []struct {
		name          string
		src           string
		line, byteCol int
		want          int
	}{
		{"ascii", "héllo", 0, 0, 0},
		{"after h", "héllo", 0, 1, 1},
		{"after é (2 bytes)", "héllo", 0, 3, 2},
		{"after é+l", "héllo", 0, 4, 3},
		{"whole line", "héllo", 0, 6, 5}, // 5 runes, all BMP
		{"past line clamps", "héllo", 0, 100, 5},
		{"second line after é", "ab\ncdé", 1, 4, 3}, // c,d,é → 3 units
		{"emoji two units", "a😀b", 0, 5, 3},         // a(1) + 😀(2) = 3 units at byte 5
	}
	for _, c := range cases {
		if got := utf16Column(c.src, c.line, c.byteCol); got != c.want {
			t.Errorf("%s: utf16Column(%q, %d, %d) = %d, want %d", c.name, c.src, c.line, c.byteCol, got, c.want)
		}
	}
}

func TestByteColumn(t *testing.T) {
	// byteColumn returns a 1-based byte column (what ast.Location uses).
	cases := []struct {
		name            string
		src             string
		line, utf16Char int
		want            int
	}{
		{"ascii start", "héllo", 0, 0, 1},
		{"after h", "héllo", 0, 1, 2},
		{"after é", "héllo", 0, 2, 4}, // é is 2 bytes: byte offset 3, 1-based col 4
		{"after é+l", "héllo", 0, 3, 5},
		{"emoji", "a😀b", 0, 3, 6}, // a + 😀(2 units) → byte offset 5, col 6
	}
	for _, c := range cases {
		if got := byteColumn(c.src, c.line, c.utf16Char); got != c.want {
			t.Errorf("%s: byteColumn(%q, %d, %d) = %d, want %d", c.name, c.src, c.line, c.utf16Char, got, c.want)
		}
	}
}

// byteColumn and utf16Column are inverses on rune boundaries: converting an LSP
// character to a byte column and back yields the original character. (Char 7
// here would land inside the 😀 surrogate pair — not a valid boundary — so it is
// deliberately excluded; such a position clamps to the whole rune.)
func TestByteColumnUtf16ColumnRoundTrip(t *testing.T) {
	const src = "héllo 😀 world" // boundaries: h0 é1 l2 l3 o4 sp5 😀6 sp8 w9 o10 r11 l12 d13
	for _, char := range []int{0, 1, 2, 3, 4, 5, 6, 8, 9, 13} {
		byteCol1 := byteColumn(src, 0, char)    // 1-based byte column
		back := utf16Column(src, 0, byteCol1-1) // 0-based byte column -> UTF-16
		if back != char {
			t.Errorf("round trip char %d: byteColumn=%d, utf16Column=%d", char, byteCol1, back)
		}
	}
}

func TestByteOffsetAt(t *testing.T) {
	const src = "ab\ncdé" // a0 b1 \n2 c3 d4 é5-6
	cases := []struct{ line, byteCol, want int }{
		{0, 0, 0},
		{0, 2, 2},
		{1, 0, 3},
		{1, 2, 5},
		{1, 100, len(src)}, // clamp to len
	}
	for _, c := range cases {
		if got := byteOffsetAt(src, c.line, c.byteCol); got != c.want {
			t.Errorf("byteOffsetAt(%q, %d, %d) = %d, want %d", src, c.line, c.byteCol, got, c.want)
		}
	}
}

func TestUtf16Len16(t *testing.T) {
	cases := []struct {
		s    string
		want int
	}{
		{"", 0},
		{"abc", 3},
		{"héllo", 5},
		{"a😀b", 4}, // 😀 is a surrogate pair
	}
	for _, c := range cases {
		if got := utf16Len16(c.s); got != c.want {
			t.Errorf("utf16Len16(%q) = %d, want %d", c.s, got, c.want)
		}
	}
}

func TestLocToRange(t *testing.T) {
	// The binary expression `"é" == 5` in this source has byte location
	// 1:9-1:18. é is 2 bytes but 1 UTF-16 unit, so the end column must be 16
	// (the UTF-16 length), not 17 (the byte length).
	const src = `let x = "é" == 5`
	loc := ast.Location{StartLine: 1, StartCol: 9, EndLine: 1, EndCol: 18}
	got := locToRange(src, loc)
	want := lsp.Range{
		Start: lsp.Position{Line: 0, Character: 8},
		End:   lsp.Position{Line: 0, Character: 16},
	}
	if got != want {
		t.Errorf("locToRange = %+v, want %+v", got, want)
	}
}

// TestDiagnostic_NonASCIIColumn is the end-to-end proof for the outgoing path:
// a diagnostic whose range spans a multi-byte character must be published with
// UTF-16 columns, not byte columns.
func TestDiagnostic_NonASCIIColumn(t *testing.T) {
	h := servertest.New(t, newHandler())
	// `"é" == 5` is a type error; its range covers the é.
	openAndWait(t, h, `let x = "é" == 5`)

	diags := h.Diagnostics(testURI)
	if len(diags) == 0 {
		t.Fatal("expected a diagnostic")
	}
	r := diags[0].Range
	// Byte-based conversion would give End.Character 17; UTF-16 gives 16.
	if r.Start.Character != 8 || r.End.Character != 16 {
		t.Errorf("diagnostic range = %+v, want Start.Character=8 End.Character=16", r)
	}
}

// TestHover_NonASCIIPosition is the end-to-end proof for the incoming path: a
// hover request whose UTF-16 character sits after a multi-byte rune must be
// mapped to the right AST node.
func TestHover_NonASCIIPosition(t *testing.T) {
	h := servertest.New(t, newHandler())
	// `sé` can't be an identifier (non-ASCII), so put the multi-byte rune in a
	// string earlier on the line and hover the identifier after it.
	src := `let a = 1` + "\n" + `let b = "é" == a`
	openAndWait(t, h, src)

	// On line 1, the identifier `a` is the last character. In UTF-16 columns:
	// `let b = "é" == a` → l e t sp b sp = sp " é " sp = = sp a → `a` at index 15.
	hover, err := h.Hover(testURI, 1, 15)
	if err != nil {
		t.Fatalf("Hover: %v", err)
	}
	if hover == nil {
		t.Fatal("expected hover on `a`, got nil (incoming UTF-16 position was mis-mapped)")
	}
}
