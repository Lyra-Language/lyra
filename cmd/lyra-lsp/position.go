package main

import (
	"strings"
	"unicode/utf8"

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
