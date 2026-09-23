package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Lyra-Language/lyra/pkg/analyzer/collector"
	"github.com/Lyra-Language/lyra/pkg/parser"
	"github.com/Lyra-Language/lyra/pkg/printer"
)

// **The Go collector as the oracle directly**, rather than through its stored goldens.
//
// The golden files are a curated set, and a slice can add a construct none of them
// exercises in isolation: slice 5's calls and blocks appear in plenty of goldens and in no
// *matching* one, because every golden using them also uses something further out. Pinning
// them against hand-written expectations would rebuild the weakness slice 4 deleted — a
// second construction of the same tree, free to drift.
//
// Running both collectors on the same source removes the curation. The Go AST goes through
// `pkg/printer` exactly as the goldens were produced, so agreement here is agreement with
// the file on disk, for any source either can be given.
func TestCollector_AgreesWithTheGoCollector(t *testing.T) {
	bin := buildCollector(t)
	for _, c := range []struct{ name, source string }{
		// Slice 5's two, in the shapes no matching golden covers.
		{"a call with no arguments", "let a = foo()"},
		{"a call with arguments", "let b = bar(1, x)"},
		{"a call whose callee is a member", "let c = person.greet()"},
		{"a non-empty block as a body", "let d = () => {\n  let inner = 1\n}"},
		{"a block holding several statements", "let e = () => {\n  let x = 1\n  let y = 2\n}"},
		{"a call inside a block", "let f = () => {\n  let g = h(1)\n}"},
		{"nested calls", "let i = outer(inner(2))"},
		// And the postfix family beside them, since they compose.
		{"indexing a call's result", "let j = rows()[0]"},
		{"a member of an index", "let k = grid[1].name"},
		{"a tuple index of a member", "let l = pair.first.0"},
	} {
		t.Run(c.name, func(t *testing.T) {
			source := filepath.Join(t.TempDir(), "in.lyra")
			if err := os.WriteFile(source, []byte(c.source), 0o644); err != nil {
				t.Fatal(err)
			}
			got, err := exec.Command(bin, source).Output()
			if err != nil {
				t.Fatalf("the Lyra collector exited non-zero on %q: %v", c.source, err)
			}
			if want := goCollectorAST(t, c.source); string(got) != want {
				t.Errorf("the two collectors disagree on %q\n Lyra:\n%s\n Go:\n%s",
					c.source, got, want)
			}
		})
	}
}

// goCollectorAST is the Go collector's answer for source, printed the way the goldens are.
func goCollectorAST(t *testing.T, source string) string {
	t.Helper()
	tree, err := parser.Parse(source)
	if err != nil {
		t.Fatalf("parsing %q: %v", source, err)
	}
	program, _, _, errs := collector.NewCollector([]byte(source)).Collect(tree.RootNode())
	if len(errs) > 0 {
		t.Fatalf("the Go collector rejected %q: %v", source, errs)
	}
	out := printer.PrintAST(program)
	if !strings.HasSuffix(out, "\n") {
		out += "\n"
	}
	return out
}
