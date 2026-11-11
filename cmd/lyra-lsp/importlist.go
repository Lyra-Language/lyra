package main

import (
	"strings"
	"unicode/utf8"

	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/owenrumney/go-lsp/lsp"
)

// The member list of one `import m.{ … }`, read from the source as byte offsets, for the
// two quick fixes that edit one: adding a name (addImportActions) and taking an unused one
// out (removeUnusedImportAction).
//
// **Both edits are made against the text, never by re-printing the statement**, so what
// the reader wrote — a comment beside a name, an alias, the list's layout, a trailing
// comma — survives the edit. The rule each follows is the layout lyrafmt would keep: a
// list on one line stays on one line until it passes lyrafmt's width (then it is exploded
// as lyrafmt explodes it), and a list already a name per line gets a line of its own in
// the same indentation, with the trailing comma the list already has or lacks. A new name
// is appended, not sorted in: lyrafmt does not sort import lists and this repository's are
// in the order they were written.
//
// The edit is always one TextEdit. Two edits at one position are ordered by the array, a
// rule clients have been seen to get wrong; one edit covering both changes cannot be.
type importList struct {
	source string
	stmt   int // offset of `import`
	open   int // offset just past `.{`
	close  int // offset of the closing `}`
	// The members the program wrote. A list tree-sitter recovered (`import m.{}`, which the
	// grammar refuses) carries a MISSING member with no name; it is not one of these.
	members []listMember
}

type listMember struct {
	start, end int
	name       string
	alias      string
	loc        ast.Location
}

// fmtWidth is lyrafmt's line budget (`WIDTH` in examples/lyrafmt/lyrafmt.lyra): a comma
// list on a longer line is exploded a member per line.
const fmtWidth = 90

// fmtIndent is lyrafmt's indentation step, which an exploded list's members take.
const fmtIndent = "  "

// readImportList finds the braces and members of a member import in source. Not ok when
// the statement's text is not the shape its node says it is — a recovered parse ending
// somewhere other than its `}` — since an edit placed by a guess corrupts the buffer.
func readImportList(source string, imp *ast.ImportStmt) (importList, bool) {
	loc := imp.GetLocation()
	stmt := byteOffsetAt(source, lspPos(loc.StartLine), lspPos(loc.StartCol))
	// ast.Location's end column is one past the last character, 1-based: the `}` is at
	// 0-based byte column EndCol-2 (beforeClosingBrace).
	closing := byteOffsetAt(source, lspPos(loc.EndLine), loc.EndCol-2)
	if closing <= stmt || closing >= len(source) || source[closing] != '}' {
		return importList{}, false
	}
	at := strings.Index(source[stmt:closing], ".{")
	if at < 0 {
		return importList{}, false
	}
	l := importList{source: source, stmt: stmt, open: stmt + at + 2, close: closing}
	for _, m := range imp.Members {
		if m.Name == "" {
			continue
		}
		start := byteOffsetAt(source, lspPos(m.Location.StartLine), lspPos(m.Location.StartCol))
		end := byteOffsetAt(source, lspPos(m.Location.EndLine), lspPos(m.Location.EndCol))
		if start < l.open || end > l.close || start >= end {
			return importList{}, false
		}
		l.members = append(l.members, listMember{
			start: start, end: end, name: m.Name, alias: m.Alias, loc: m.Location,
		})
	}
	return l, true
}

// addEdit is the edit putting name at the end of the list.
func (l importList) addEdit(name string) lsp.TextEdit {
	if len(l.members) == 0 {
		// `import m.{}` (or `{` and `}` on two lines): the list is written whole.
		return l.replace(l.open, l.close, " "+name+" ")
	}
	last := l.members[len(l.members)-1]
	if l.spansLines(last.end, l.close) {
		if edit, ok := l.addOnItsOwnLine(last, name); ok {
			return edit
		}
	}
	lineStart, lineEnd := l.lineAround(last.end)
	width := utf8.RuneCountInString(l.source[lineStart:lineEnd]) + len(", ") + len(name)
	if width > fmtWidth && l.onlyMembers() {
		return l.explode(name)
	}
	return l.replace(last.end, last.end, ", "+name)
}

// addOnItsOwnLine adds name on a line after last's, the layout of a list lyrafmt exploded
// (or one written a name per line). Not ok when last's line ends inside a block comment,
// where a line break would land in the comment.
func (l importList) addOnItsOwnLine(last listMember, name string) (lsp.TextEdit, bool) {
	_, lineEnd := l.lineAround(last.end)
	rest := l.source[last.end:lineEnd]
	if open := strings.LastIndex(rest, "/*"); open >= 0 && !strings.Contains(rest[open:], "*/") {
		return lsp.TextEdit{}, false
	}
	newline := "\n"
	if strings.HasSuffix(rest, "\r") {
		rest, lineEnd, newline = rest[:len(rest)-1], lineEnd-1, "\r\n"
	}
	line := newline + l.memberIndent(last) + name
	if l.commaAfter(last) >= 0 {
		// The list ends in a comma, so the new last member does too.
		return l.replace(lineEnd, lineEnd, line+","), true
	}
	// No trailing comma: the old last member takes one, its comment (`half // halves`)
	// staying after it, and the new one is the list's end.
	return l.replace(last.end, lineEnd, ","+rest+line), true
}

// explode rewrites a one-line list that would pass lyrafmt's width a member per line, as
// lyrafmt writes one: each member indented a step from the statement, the trailing comma
// kept if the list had one, `}` on a line of its own.
func (l importList) explode(name string) lsp.TextEdit {
	indent := l.lineIndent(l.stmt) + fmtIndent
	var b strings.Builder
	for _, m := range l.members {
		b.WriteString("\n" + indent + l.source[m.start:m.end] + ",")
	}
	b.WriteString("\n" + indent + name)
	if l.commaAfter(l.members[len(l.members)-1]) >= 0 {
		b.WriteString(",")
	}
	b.WriteString("\n" + l.lineIndent(l.stmt))
	return l.replace(l.open, l.close, b.String())
}

// removeEdit is the edit taking members[i] out of a list of two or more, leaving the
// others' layout, commas and comments as they were.
func (l importList) removeEdit(i int) lsp.TextEdit {
	m := l.members[i]
	isLast := i == len(l.members)-1
	if start, end, ok := l.ownLines(m, isLast); ok {
		if isLast && l.commaAfter(m) < 0 {
			// The member going was the list's end and had no comma; the one before it
			// becomes the end, so its comma goes — and the text after that comma (a
			// comment) stays.
			if comma := l.commaAfter(l.members[i-1]); comma >= 0 {
				return l.replace(comma, end, l.source[comma+1:start])
			}
		}
		return l.replace(start, end, "")
	}
	if !isLast {
		// `x, ` up to the next member.
		return l.replace(m.start, l.members[i+1].start, "")
	}
	// `, y` back to the member before.
	return l.replace(l.members[i-1].end, m.end, "")
}

// ownLines reports the lines a member stands on alone — nothing before it on its first
// line, and after it no more than its comma and a line comment — as the range deleting
// them, newline included. A member that is not the list's last must have its comma there,
// or deleting its lines would leave the comma that follows it leading the next line.
func (l importList) ownLines(m listMember, isLast bool) (int, int, bool) {
	lineStart, _ := l.lineAround(m.start)
	if lineStart <= l.open || strings.TrimLeft(l.source[lineStart:m.start], " \t") != "" {
		return 0, 0, false
	}
	_, lineEnd := l.lineAround(m.end)
	if lineEnd >= l.close || lineEnd >= len(l.source) {
		return 0, 0, false
	}
	rest := strings.TrimLeft(l.source[m.end:lineEnd], " \t")
	hasComma := strings.HasPrefix(rest, ",")
	rest = strings.TrimLeft(strings.TrimPrefix(rest, ","), " \t\r")
	if rest != "" && !strings.HasPrefix(rest, "//") {
		return 0, 0, false
	}
	if !isLast && !hasComma {
		return 0, 0, false
	}
	return lineStart, lineEnd + 1, true
}

// commaAfter is the offset of the comma directly after m (spaces and tabs between), or -1.
func (l importList) commaAfter(m listMember) int {
	for i := m.end; i < l.close; i++ {
		switch l.source[i] {
		case ' ', '\t':
			continue
		case ',':
			return i
		}
		return -1
	}
	return -1
}

// onlyMembers reports whether the braces hold nothing but the members, commas and
// whitespace — no comment, which exploding the list would have to place by a guess.
func (l importList) onlyMembers() bool {
	at := l.open
	var between strings.Builder
	for _, m := range l.members {
		between.WriteString(l.source[at:m.start])
		at = m.end
	}
	between.WriteString(l.source[at:l.close])
	return strings.Trim(between.String(), " \t\r\n,") == "" &&
		!strings.ContainsAny(between.String(), "/")
}

// memberIndent is the indentation of the line m is on — or, for a member on the `.{` line,
// the statement's plus lyrafmt's step.
func (l importList) memberIndent(m listMember) string {
	if lineStart, _ := l.lineAround(m.start); lineStart > l.open {
		return l.lineIndent(m.start)
	}
	return l.lineIndent(l.stmt) + fmtIndent
}

// lineIndent is the leading whitespace of the line holding offset.
func (l importList) lineIndent(offset int) string {
	lineStart, lineEnd := l.lineAround(offset)
	line := l.source[lineStart:lineEnd]
	return line[:len(line)-len(strings.TrimLeft(line, " \t"))]
}

// lineAround is the line holding offset: its first byte and the offset of its newline (or
// the end of the source).
func (l importList) lineAround(offset int) (int, int) {
	start := strings.LastIndexByte(l.source[:offset], '\n') + 1
	end := strings.IndexByte(l.source[offset:], '\n')
	if end < 0 {
		return start, len(l.source)
	}
	return start, offset + end
}

func (l importList) spansLines(from, to int) bool {
	return strings.IndexByte(l.source[from:to], '\n') >= 0
}

func (l importList) replace(from, to int, text string) lsp.TextEdit {
	return lsp.TextEdit{
		Range:   lsp.Range{Start: offsetPosition(l.source, from), End: offsetPosition(l.source, to)},
		NewText: text,
	}
}

// offsetPosition converts a byte offset in source to an LSP position (0-based line, UTF-16
// column).
func offsetPosition(source string, offset int) lsp.Position {
	line := strings.Count(source[:offset], "\n")
	return lsp.Position{
		Line:      line,
		Character: utf16Column(source, line, offset-lineStartByte(source, line)),
	}
}

// mentionedOutside reports whether any of names appears as a whole word in source outside
// [from, to). Deliberately coarse — a comment or string counts — because it decides
// whether deleting an import could break the program, and the cost of a false "yes" is
// only a weaker edit.
func mentionedOutside(source string, from, to int, names ...string) bool {
	for _, name := range names {
		if name == "" {
			continue
		}
		for at := 0; ; {
			i := strings.Index(source[at:], name)
			if i < 0 {
				break
			}
			i += at
			at = i + len(name)
			if i >= from && i < to {
				continue
			}
			before := i == 0 || !isNameByte(source[i-1])
			after := at >= len(source) || !isNameByte(source[at])
			if before && after {
				return true
			}
		}
	}
	return false
}

func isNameByte(b byte) bool {
	return b == '_' || b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= '0' && b <= '9'
}
