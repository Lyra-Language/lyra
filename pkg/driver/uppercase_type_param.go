package driver

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"
)

// uppercaseTypeParamMessage explains a parse error inside a declaration's generic list
// that names a type parameter in uppercase — `let f<R>`, `struct Box<T>`, `<t, U>` — and
// gives the list as it must be written. ok=false for any other parse error.
//
// **The grammar refuses `<T>` on purpose** (tree-sitter-lyra's generic_type.js): a
// lowercase name is a type variable and an Uppercase one a concrete type, and admitting
// both in the list would collide `T` with a const_identifier in the lexer. So the parse
// fails, and the bare "unexpected \"<R> where R\"" it left — followed by "unknown type
// \"Renderer\"" for a trait plainly declared — named neither the rule nor the fix (10/26).
// The error is recognised here, from the source around it, rather than by a grammar rule
// that accepts the spelling only to refuse it.
func uppercaseTypeParamMessage(source []byte, start, end uint) (string, bool) {
	lineStart := strings.LastIndexByte(string(source[:start]), '\n') + 1
	// The list's `<`: the error's own first byte, or the last `<` before it on the line
	// with nothing between that would end a generic list.
	open := -1
	for i := int(start); i >= lineStart; i-- {
		c := source[i]
		if c == '<' {
			open = i
			break
		}
		if i < int(start) && strings.ContainsRune(">(){}=;", rune(c)) {
			break
		}
	}
	if open < 0 || !declarationHead.Match(source[lineStart:open]) {
		return "", false
	}
	// The error starts inside the list (it may run past it: `<R> where R`).
	closeAt := strings.IndexByte(string(source[open:]), '>')
	if closeAt < 0 || int(start) > open+closeAt {
		return "", false
	}
	inner := string(source[open+1 : open+closeAt])
	if strings.ContainsAny(inner, "\n(){}=") {
		return "", false
	}
	var written, fixed []string
	var named []string
	for _, part := range strings.Split(inner, ",") {
		part = strings.TrimSpace(part)
		written = append(written, part)
		name := leadingName.FindString(part)
		// `const N: i64` is a value parameter, uppercase by rule.
		if name == "" || name == "const" || !unicode.IsUpper(rune(name[0])) {
			fixed = append(fixed, part)
			continue
		}
		named = append(named, name)
		fixed = append(fixed, strings.ToLower(name)+part[len(name):])
	}
	if len(named) == 0 {
		return "", false
	}
	what := "would be a concrete type, not a variable"
	if len(named) > 1 {
		what = "would be concrete types, not variables"
	}
	return fmt.Sprintf(
		"a type parameter is written in lowercase — %s %s — so `<%s>` is `<%s>`, and so is every use in the declaration",
		quoteAll(named), what, strings.Join(written, ", "), strings.Join(fixed, ", ")), true
}

// declarationHead is a line up to a declared name that a generic list may follow: `let f`,
// `pub let pure f`, `struct Box`, `data Opt`, `trait Conv` and the other type forms.
var declarationHead = regexp.MustCompile(
	`(?:^|\s)(?:pub\s+)?(?:let|var|struct|data|trait|type|alias|union|tuple)\s+(?:(?:mut|pure|det|noalloc)\s+)*[A-Za-z_][A-Za-z0-9_]*\s*$`)

var leadingName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*`)

func quoteAll(names []string) string {
	quoted := make([]string, len(names))
	for i, n := range names {
		quoted[i] = fmt.Sprintf("%q", n)
	}
	return strings.Join(quoted, ", ")
}
