package driver_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/Lyra-Language/lyra/pkg/driver"
)

// Malformed input must not crash the compiler.
//
// The hazard is hazard 3's: a collector that hits an unrecoverable value error and returns
// a nil *concrete* node hands the caller a non-nil interface holding a nil pointer, and the
// first `GetLocation` on it segfaults. Most of those paths guard a *required* grammar field
// and so cannot fire on a program that parsed — but tree-sitter recovers from a syntax
// error by building partial nodes, and a partial node is exactly a node with a field
// missing. That is the door, and these are the shapes that knock on it.
//
// A rune literal with an illegal escape crashed the compiler this way on 08/30, and only
// because the grammar had been loosened that day; the path had been dead and wrong for as
// long as the literal existed. This test is the standing check that the rest of them stay
// unreachable — or, when one becomes reachable, that it is found here rather than by a user
// typing a half-finished line.
func TestAnalyzeDoesNotPanicOnMalformedInput(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"extern with no signature":     "extern foo:",
		"extern with an open paren":    "extern foo: (",
		"extern with a junk signature": "extern puts: 12345",
		"impl with no type":            "impl Show for",
		"impl with an open brace":      "impl Show for {",
		"impl with no trait":           "impl for Cat { }",
		"with, no body":                "let main = () -> void => { with arena }",
		"with, non-block body":         "let main = () -> void => { with arena 5 }",
		"for-in, no body":              "let main = () -> void => { for i in 0..<3 }",
		"for-in, non-block body":       "let main = () -> void => { for i in 0..<3 5 }",
		"for-in, no loop variable":     "let main = () -> void => { for in xs { } }",
		"for-in, no iterable":          "let main = () -> void => { for i in { } }",
		"for, bare":                    "let main = () -> void => { for }",
		"for, non-block body":          "let main = () -> void => { for ; ; 5 }",
		"deref assign, no value":       "let main = () -> void => { unsafe { p^ = } }",
		"deref assign, no target":      "let main = () -> void => { unsafe { ^ = 5 } }",
		"lvalue assign, no value":      "let main = () -> void => { xs[0] = }",
		"var reassign, no value":       "let main = () -> void => { var x = 1; x = }",
		"import, bare":                 "import",
		"import, empty members":        "import .{ }",
		"module, bare":                 "module",
		"type, bare":                   "type",
		"type, no right-hand side":     "type Foo =",
		"trait, no name":               "trait { }",
		"sizeof, no type":              `let main = () -> void => { println("${sizeof()}") }`,
		"unsafe, no body":              "let main = () -> void => { unsafe }",
		"data pattern, unclosed":       "let main = () -> void => { match m { Some( => 1, _ => 2 } }",
		"if-let, no then block":        "let main = () -> void => { if let Some(x) = m }",
		"if-let, no else block":        "let main = () -> void => { if let Some(x) = m { } else }",
		"lambda, no body":              "let main = () -> void =>",
		"rune, illegal escape":         `let c: rune = '\q'`,
		"rune, dangling backslash":     `let c: rune = '\`,
		"rune, empty":                  "let c: rune = ''",
		"string, illegal escape":       `let s = "\q"`,
	}
	for name, src := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("Analyze panicked on %s (%q): %v", name, src, r)
				}
			}()
			res := driver.Analyze([]byte(src))
			// Reaching the typechecker is the point — a typed nil crashes there, not in the
			// collector that made it. Touch every diagnostic so nothing is lazily unevaluated.
			var sb strings.Builder
			for _, d := range res.Diagnostics {
				fmt.Fprintf(&sb, "%v", d)
			}
			_ = sb.String()
		})
	}
}
