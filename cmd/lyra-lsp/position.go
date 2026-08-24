package main

import (
	"strings"
	"sync"
	"unicode/utf8"
	"unsafe"

	"github.com/owenrumney/go-lsp/lsp"

	"github.com/Lyra-Language/lyra/pkg/ast"
)

// This file is the single place that reconciles the two position conventions in
// play:
//
//   - ast.Location uses 1-based line/column where the column is a BYTE offset
//     within the line (tree-sitter reports byte columns).
//   - LSP uses 0-based line/character where "character" counts UTF-16 code
//     units.
//
// For ASCII source the two column systems are identical, so these helpers are
// no-ops there; they only differ on lines containing multi-byte runes (é, 世,
// emoji). Every conversion between an ast.Location and an lsp position/range —
// in both directions — goes through here.

// lineStartByte returns the byte offset at which 0-based line `line` begins. A
// line past the end of the text clamps to len(source).
func lineStartByte(source string, line int) int {
	starts := lineStarts(source)
	if line < len(starts) {
		return starts[line]
	}
	return len(source)
}

// lineStarts is the byte offset of every 0-based line in source, memoized.
//
// **The scan it replaces made position conversion quadratic.** Every conversion walked from
// the top of the file counting newlines, and a Range costs two of them — so a request that
// converts N positions over an L-line file did O(N·L) work. Measured over a synthetic file:
// 1.5 ms at 500 lines / 200 conversions, 10 ms at 2000 / 500, and **93 ms at 5000 / 2000**,
// which is a visible stall on document symbols or diagnostics in a large file. With the
// index it is one pass to build and a slice index per lookup.
//
// A two-entry cache rather than a map: a request converts many positions over *one* source,
// and two slots cover an editor moving between a pair of files without letting the cache
// grow with everything ever opened.
//
// **The key is the string's identity — its data pointer and length — not its contents.**
// Comparing `source == entry.source` looks equivalent and is not: Go's string equality falls
// through to `runtime.memequal` over the whole text, so on a 5000-line file the lookup cost
// more than the scan it replaced. It was 94% of the profile, and the version of this comment
// that claimed Go short-circuits on the pointer was simply wrong.
//
// Equal pointer *and* equal length means the same bytes, so a hit is sound. Two distinct
// strings with equal contents miss and pay one rebuild — a slower path, never a wrong
// answer, and not a case the LSP produces anyway since every conversion in a request reads
// the source the handler fetched once.
func lineStarts(source string) []int {
	key := unsafe.StringData(source)
	lineCache.mu.Lock()
	defer lineCache.mu.Unlock()
	for i := range lineCache.entries {
		if lineCache.entries[i].starts != nil &&
			lineCache.entries[i].key == key && lineCache.entries[i].length == len(source) {
			return lineCache.entries[i].starts
		}
	}
	starts := make([]int, 1, 1+strings.Count(source, "\n"))
	for i := 0; i < len(source); i++ {
		if source[i] == '\n' {
			starts = append(starts, i+1)
		}
	}
	lineCache.entries[lineCache.next] = lineEntry{key: key, length: len(source), starts: starts}
	lineCache.next = (lineCache.next + 1) % len(lineCache.entries)
	return starts
}

// key is the source's data pointer, never dereferenced — only compared, which is what makes
// the lookup O(1) instead of O(len(source)).
type lineEntry struct {
	key    *byte
	length int
	starts []int
}

// Guarded because an LSP server answers requests concurrently. The critical section is a
// pointer comparison on a hit, which is far cheaper than the scan it replaces.
var lineCache struct {
	mu      sync.Mutex
	entries [2]lineEntry
	next    int
}

// utf16Column converts a 0-based byte column on 0-based `line` to the 0-based
// UTF-16 code-unit column LSP expects. A byte column past the line's end clamps
// to the line end.
func utf16Column(source string, line, byteCol int) int {
	start := lineStartByte(source, line)
	end := start + byteCol
	if end > len(source) {
		end = len(source)
	}
	units := 0
	for i := start; i < end; {
		r, size := utf8.DecodeRuneInString(source[i:])
		if r == '\n' {
			break
		}
		units += utf16Len(r)
		i += size
	}
	return units
}

// byteColumn is the inverse used on incoming requests: it converts a 0-based
// LSP (line, utf16 character) to the 1-based byte column that ast.Location uses,
// so a cursor position can be matched against node locations.
func byteColumn(source string, line, utf16Char int) int {
	return posToOffset(source, line, utf16Char) - lineStartByte(source, line) + 1
}

// byteOffsetAt returns the absolute byte offset in source for 0-based `line`
// and 0-based byte column `byteCol`, clamped to source bounds. Used when a
// helper needs to scan the raw bytes at an ast.Location (whose columns are
// already bytes) rather than convert to an LSP position.
func byteOffsetAt(source string, line, byteCol int) int {
	off := lineStartByte(source, line) + byteCol
	if off < 0 {
		return 0
	}
	if off > len(source) {
		return len(source)
	}
	return off
}

// locToRange converts a 1-based, byte-based ast.Location to a 0-based, UTF-16
// LSP range against the document source.
func locToRange(source string, loc ast.Location) lsp.Range {
	return lsp.Range{
		Start: lsp.Position{
			Line:      lspPos(loc.StartLine),
			Character: utf16Column(source, lspPos(loc.StartLine), lspPos(loc.StartCol)),
		},
		End: lsp.Position{
			Line:      lspPos(loc.EndLine),
			Character: utf16Column(source, lspPos(loc.EndLine), lspPos(loc.EndCol)),
		},
	}
}

// utf16Len16 returns the number of UTF-16 code units in s (the string form of
// utf16Len). Token lengths reported to LSP must be in these units.
func utf16Len16(s string) int {
	n := 0
	for _, r := range s {
		n += utf16Len(r)
	}
	return n
}
