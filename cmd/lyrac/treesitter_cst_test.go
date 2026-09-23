package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The CST accessors a **collector** needs, which are not the ones a formatter needs.
//
// `lyrafmt` walks to the leaves by index and rebuilds text from their bytes; it never asks
// what a child *is*. A collector asks almost nothing else — `cst.Field(node, "name")` is
// the most-used call in the Go one — so `bindings/treesitter` gaining field access, named
// children and a byte-correct text read is the first thing the bootstrap stands on
// (todo.md, 09/22).
//
// Two of these are shaped deliberately differently from the Go binding they mirror:
//
//   - **`field` answers a `Maybe`.** In C an absent field is a *null node*, indistinguishable
//     from a real one until you use it. That is the compiler's hazard 2: Go's
//     `ChildByFieldName` returns a real nil whose `ChildCount` **hangs inside the CGO
//     binding** rather than panicking, so the mistake presents as a wedged editor rather
//     than a stack trace. Here there is no Node to call anything on, and forgetting the
//     case does not compile.
//   - **`text` takes the source's bytes, not the string.** Every offset tree-sitter reports
//     counts bytes; a Lyra `string` slices by rune. Passing the string would read the wrong
//     text for any file that is not ASCII, and read it silently.
func TestTreeSitter_CollectorAccessors(t *testing.T) {
	bin := buildProbe(t, `
import bindings.treesitter.{
  lyra_language, new_parser, parser_delete, parse, tree_delete,
  root, kind, field, named_child, named_child_count, text,
}
import std.ffi.{ cstring }

// Eight multi-byte runes sit above the declaration under test, so every offset in it is
// past the point where counting runes and counting bytes diverge.
let sample = "// café üß — a comment\nlet add = (a: i64, b: i64) -> i64 => a + b\n"

let main = () -> u8 => {
  let Some(parser) = new_parser(lyra_language()) else { return 1 }
  let Some(tree) = parse(parser, sample) else {
    parser_delete(parser)
    return 1
  }
  let bytes = sample.cstring()
  let decl = named_child(root(tree), 1)
  println("kind=${kind(decl)}")
  println("named=${named_child_count(decl)}")
  match field(decl, "name") {
    Some(n) => println("name=${text(n, bytes)}"),
    None => println("name=<absent>"),
  }
  match field(decl, "value") {
    Some(v) => println("value=${kind(v)}:${text(v, bytes)}"),
    None => println("value=<absent>"),
  }
  // A field this rule does not have. In C this is the null node; here it is None, and the
  // program that forgets to handle it is the program that does not build.
  match field(decl, "condition") {
    Some(_) => println("condition=<present>"),
    None => println("condition=<absent>"),
  }
  tree_delete(tree)
  parser_delete(parser)
  0
}
`)
	out, err := exec.Command(bin).CombinedOutput()
	if err != nil {
		t.Fatalf("the probe exited non-zero: %v\n%s", err, out)
	}
	// `add` and `(a: i64, b: i64) -> i64 => a + b` are both read from byte offsets that a
	// rune-indexed slice would shift by eight — the text is the assertion, not the span.
	for _, want := range []string{
		"kind=declaration",
		"named=2",
		"name=add",
		"value=lambda_expr:(a: i64, b: i64) -> i64 => a + b",
		"condition=<absent>",
	} {
		if !strings.Contains(string(out), want) {
			t.Errorf("want %q in the probe's output:\n%s", want, out)
		}
	}
}

// buildProbe writes source to a temp file and builds it against the tree-sitter runtime,
// skipping on the same three conditions the formatter's own build skips on.
func buildProbe(t *testing.T, source string) string {
	t.Helper()
	treeSitterEnv(t) // the grammar archive and the paths, or a skip
	path := filepath.Join(t.TempDir(), "probe.lyra")
	if err := os.WriteFile(path, []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	return buildLinkedToTreeSitter(t, path)
}
