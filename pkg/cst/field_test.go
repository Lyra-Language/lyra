package cst_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Lyra-Language/lyra/pkg/cst"
	"github.com/Lyra-Language/lyra/pkg/parser"
	sitter "github.com/tree-sitter/go-tree-sitter"
)

// The field names a collector asks for at nearly every node, which is the shape of
// the real access pattern.
var fieldNames = []string{"name", "value", "type", "body", "parameters", "condition"}

// preludeRoots parses every file of the shipped prelude. Real source rather than a
// fixture, because what is being measured is the *shape* of a collector's field
// accesses, and the whole prelude is the nearest thing to a representative program.
// All of it, not one file: the prelude became a directory on 08/07, and benchmarking
// whichever file sorts first would quietly shrink the workload.
func preludeRoots(tb testing.TB) []*sitter.Node {
	tb.Helper()
	dir := filepath.Join("..", "..", "std", "prelude")
	entries, err := os.ReadDir(dir)
	if err != nil {
		tb.Skipf("no std/prelude/ to parse: %v", err)
	}
	var roots []*sitter.Node
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".lyra" {
			continue
		}
		src, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			tb.Fatal(err)
		}
		tree, err := parser.Parse(string(src))
		if err != nil {
			tb.Fatal(err)
		}
		roots = append(roots, tree.RootNode())
	}
	if len(roots) == 0 {
		tb.Skip("std/prelude/ holds no .lyra files")
	}
	return roots
}

func walkAsking(n *sitter.Node, ask func(*sitter.Node, string) *sitter.Node) int {
	found := 0
	for _, f := range fieldNames {
		if ask(n, f) != nil {
			found++
		}
	}
	for i := uint(0); i < n.ChildCount(); i++ {
		found += walkAsking(n.Child(i), ask)
	}
	return found
}

// Field must answer exactly what ChildByFieldName does — including for a name the
// grammar does not define, which must stay nil rather than becoming a wrong node.
// The speed is only worth having if the answers are identical.
func TestFieldMatchesChildByFieldName(t *testing.T) {
	var check func(n *sitter.Node)
	check = func(n *sitter.Node) {
		for _, f := range append(fieldNames, "definitely_not_a_field") {
			byName, byID := n.ChildByFieldName(f), cst.Field(n, f)
			if (byName == nil) != (byID == nil) {
				t.Fatalf("field %q: ChildByFieldName nil=%v but Field nil=%v", f, byName == nil, byID == nil)
			}
			if byName != nil && byName.Id() != byID.Id() {
				t.Fatalf("field %q: the two resolved to different nodes", f)
			}
		}
		for i := uint(0); i < n.ChildCount(); i++ {
			check(n.Child(i))
		}
	}
	for _, root := range preludeRoots(t) {
		check(root)
	}
}

// The pair this replaced. ChildByFieldName allocates a C string from the Go name,
// calls into C and frees it on every lookup; Field resolves the name to an id once
// and only makes the call. Run with -bench Field to see the gap (~3.7x when this
// landed) — it is why the collector, which asks at nearly every node, got ~25%
// faster end to end.
func BenchmarkField_ByName(b *testing.B) {
	roots := preludeRoots(b)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, root := range roots {
			walkAsking(root, (*sitter.Node).ChildByFieldName)
		}
	}
}

func BenchmarkField_ByCachedId(b *testing.B) {
	roots := preludeRoots(b)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, root := range roots {
			walkAsking(root, cst.Field)
		}
	}
}
