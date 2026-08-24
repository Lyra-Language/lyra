package main

import (
	"strings"
	"testing"

	"github.com/Lyra-Language/lyra/pkg/ast"
)

// scanLineStartByte is the implementation lineStartByte had before the memoized index:
// walk from the top counting newlines. Kept here as the oracle, because the index is only
// worth having if it answers identically.
func scanLineStartByte(source string, line int) int {
	off := 0
	for l := 0; l < line; l++ {
		nl := strings.IndexByte(source[off:], '\n')
		if nl < 0 {
			return len(source)
		}
		off += nl + 1
	}
	return off
}

// The index must agree with the scan for every line of every shape of source, including the
// out-of-range lines callers reach through a stale or synthesized Location.
//
// The scan was O(line) and a Range costs two of them, so a request converting N positions
// over an L-line file did O(N·L) work — 93 ms at 5000 lines and 2000 conversions, a visible
// stall on document symbols. The index makes that one pass plus a slice read.
//
// The edge cases here are the ones a line table gets wrong: whether a trailing newline
// creates a final empty line (it does — the byte after it is a line start), an empty source,
// and CRLF, where the \r belongs to the *previous* line's bytes and only \n starts a line.
func TestLineStartByte_MatchesTheScanItReplaced(t *testing.T) {
	for _, c := range []struct{ name, src string }{
		{"empty", ""},
		{"no trailing newline", "one\ntwo\nthree"},
		{"trailing newline", "one\ntwo\nthree\n"},
		{"leading empty line", "\none\ntwo"},
		{"consecutive empty lines", "one\n\n\n\ntwo"},
		{"only newlines", "\n\n\n"},
		{"crlf", "one\r\ntwo\r\nthree\r\n"},
		{"multi-byte runes", "héllo\n世界とても長い行\n🎉🎉🎉\nplain"},
		{"no newline at all", "single line only"},
	} {
		t.Run(c.name, func(t *testing.T) {
			// Past the end deliberately: a caller with a stale Location asks for lines that
			// do not exist, and both must clamp the same way.
			for line := 0; line <= strings.Count(c.src, "\n")+3; line++ {
				want := scanLineStartByte(c.src, line)
				if got := lineStartByte(c.src, line); got != want {
					t.Errorf("lineStartByte(%q, %d) = %d; scan says %d", c.src, line, got, want)
				}
			}
		})
	}
}

// The cache is keyed on the source's **identity** (data pointer and length), not its
// contents — comparing contents costs O(len(source)) per lookup, which on a large file was
// more than the scan it replaced and showed up as 94% of the profile in runtime.memequal.
//
// The risk that swap introduces is a stale hit: two different sources sharing a key. They
// cannot, since equal pointer and equal length means the same bytes. What they *can* do is
// miss when contents are equal but storage differs, which costs a rebuild and nothing else.
// This pins both halves — the same text through distinct allocations must still answer
// correctly, and interleaving sources must not let one's index answer for the other.
func TestLineStarts_DistinctSourcesDoNotShareAnIndex(t *testing.T) {
	a := "aaa\nbbb\nccc\n"
	b := "x\ny\nz\nw\nv\n"
	// A separate allocation with the same contents as a: must answer as a does.
	aCopy := string([]byte(a))

	for i := 0; i < 4; i++ {
		for _, src := range []string{a, b, aCopy} {
			for line := 0; line <= strings.Count(src, "\n")+1; line++ {
				if got, want := lineStartByte(src, line), scanLineStartByte(src, line); got != want {
					t.Fatalf("round %d: lineStartByte(%q, %d) = %d; want %d", i, src, line, got, want)
				}
			}
		}
	}
}

// The whole point of the conversion: a UTF-16 column, which is what the index feeds. A
// regression in the index shows up here as a column off by the length of a previous line.
func TestLocToRange_ColumnsAreCorrectWithMultiByteRunes(t *testing.T) {
	// Line 2 (1-based) starts with two 3-byte runes, so byte column 7 is UTF-16 column 2.
	src := "let a = 1\n世界 = 2\nlet c = 3\n"
	r := locToRange(src, ast.Location{StartLine: 2, StartCol: 7, EndLine: 2, EndCol: 8})
	if r.Start.Line != 1 || r.Start.Character != 2 {
		t.Errorf("start = %d:%d; want 1:2 — two 3-byte runes are two UTF-16 units",
			r.Start.Line, r.Start.Character)
	}
}
