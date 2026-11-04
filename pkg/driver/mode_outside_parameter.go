package driver

import (
	"regexp"
	"strings"

	sitter "github.com/tree-sitter/go-tree-sitter"

	"github.com/Lyra-Language/lyra/pkg/cst"
)

// modeOutsideParameterMessage is what a syntax error says when it is `ref`/`mut`/`own` written
// on a local, a field or an element (lyra-E093). The grammar has no slot for one there, and
// giving it one would only be to refuse it in the collector, so the error already sits where
// the mode is and only needs telling apart from any other token that failed to shift.
func modeOutsideParameterMessage(node *sitter.Node, source []byte) (string, bool) {
	mode := misplacedMode(node, source)
	switch mode {
	case "own":
		return "`own` goes on a function's parameter: a local, a field or an element owns its value already. Drop it", true
	case "ref":
		return "`ref` goes on a function's parameter: a borrow lasts for one call, so a local, a field or an element can't hold one. Drop it; a field that refers to a single value rather than holding a copy is `shared`", true
	case "mut":
		return "`mut` goes on a function's parameter: a borrow lasts for one call, so a local, a field or an element can't hold one. Drop it; a `var` is a mutable binding of its own, and to write into a place, pass it to a `mut` parameter", true
	}
	return "", false
}

// misplacedMode names the mode an error node stands for, by the shapes recovery leaves. It
// looks no further than these, because an error can swallow a whole function, and a `mut`
// parameter inside one says nothing about what went wrong.
func misplacedMode(node *sitter.Node, source []byte) string {
	text := strings.TrimSpace(node.Utf8Text(source))
	// `x: mut i64`, `[]ref t`, `Maybe<own t>`, `^ref t`: the keyword alone, a type after it.
	if isBorrowMode(text) {
		if rest := strings.TrimLeft(string(source[node.EndByte():]), " \t"); rest != "" && startsType(rest[0]) {
			return text
		}
		return ""
	}
	prev, parent := node.PrevSibling(), node.Parent()
	// `let ref x = 1`: the keyword read as the binding's name, and the real name as the error.
	if prev != nil && parent != nil && parent.Kind() == "declaration" && prev.Kind() == "identifier" {
		if name := prev.Utf8Text(source); isBorrowMode(name) {
			return name
		}
	}
	// `(ref string, i64)` reads as the start of a function type, and fails where no `->` comes.
	// In a parameter, recovery gets as far as that type and the error is what follows it.
	if text == ")" && parent != nil && parent.Kind() == "lambda_type" {
		return firstMode(cst.Field(parent, "parameter_types"), source)
	}
	// Elsewhere the error takes the whole list: from the name or the colon in a field or a local,
	// from the arrow in a return type, and the payload in a constructor. `(x: mut i64) {`, a
	// function missing its `=>`, is none of these, which is why the start of the error is checked.
	afterConstructor := prev != nil && prev.Kind() == "data_type_constructor" && strings.HasPrefix(text, "(")
	if !afterConstructor && !annotatedList.MatchString(text) {
		return ""
	}
	return firstMode(node, source)
}

// annotatedList is an error's text opening with an annotation on a parenthesized list:
// `: (ref t, u)`, `p: (ref t, u)`, `-> (ref t, u)`.
var annotatedList = regexp.MustCompile(`^(([a-z_][a-zA-Z0-9_]*\s*)?:|->)\s*\(`)

// firstMode finds the first `type_modifier` under a node, by source order.
func firstMode(node *sitter.Node, source []byte) string {
	if node == nil {
		return ""
	}
	if node.Kind() == "type_modifier" {
		return node.Utf8Text(source)
	}
	for i := uint(0); i < node.ChildCount(); i++ {
		if mode := firstMode(node.Child(i), source); mode != "" {
			return mode
		}
	}
	return ""
}

func isBorrowMode(text string) bool {
	return text == "ref" || text == "mut" || text == "own"
}

// startsType reports whether a byte can begin a type: a name, `[]t`, `(t, u)`, `^t`, `{ a: t }`.
func startsType(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c == '[' || c == '(' || c == '^' || c == '{'
}
