package parser_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Lyra-Language/lyra/pkg/parser"
)

// A parse always terminates, including on a program cut off mid-edit — the state the language
// server parses on nearly every keystroke.
//
// It did not (fixed 09/14). `comment` was a grammar rule, a choice over two line-comment tokens
// and the scanner's block-comment token, which makes it a non-terminal extra, and
// go-tree-sitter's runtime loops forever recovering from an error at end of input with one in
// play: re-entering `recover_to_previous` and reducing an empty `comment` each round. The
// tree-sitter CLI's newer runtime parsed the same input in a millisecond, which is why no
// corpus test could see it. Comments are now one external token (scan_comment in
// tree-sitter-lyra's scanner.c). Against the old grammar the sweep below hung on its first
// five truncations of the examples, all ordinary mid-edit states — `Rect { x: x, y: y, w:
// SIDE, ` among them.
//
// A hang cannot be interrupted inside CGO, so a timed-out parse is reported and its goroutine
// abandoned; the test binary still exits.
func TestParse_TerminatesOnTruncatedInput(t *testing.T) {
	t.Parallel()
	for _, src := range []string{
		"let main = () -> void => {\n  println(x ++ ",
		"let main = () -> void => {\n  println(\"[\" ++ ",
		"let main = () -> void => {\n  let p = Rect { x: x, y: y, w: SIDE, ",
	} {
		parseWithin(t, src, src)
	}

	root := filepath.Join("..", "..")
	var files []string
	for _, dir := range []string{"std", "examples", "bindings"} {
		filepath.Walk(filepath.Join(root, dir), func(p string, info os.FileInfo, err error) error {
			if err == nil && !info.IsDir() && strings.HasSuffix(p, ".lyra") {
				files = append(files, p)
			}
			return nil
		})
	}
	if len(files) == 0 {
		t.Fatal("found no .lyra sources to truncate")
	}
	for _, f := range files {
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		step := max(len(src)/60, 1)
		for cut := 1; cut < len(src); cut += step {
			if !parseWithin(t, string(src[:cut]), f) {
				return // one hang is enough to report; the rest would each leak a spinning goroutine
			}
		}
	}
}

func parseWithin(t *testing.T, src, where string) bool {
	t.Helper()
	done := make(chan struct{})
	go func() {
		if tree, err := parser.Parse(src); err == nil && tree != nil {
			tree.Close()
		}
		close(done)
	}()
	select {
	case <-done:
		return true
	case <-time.After(5 * time.Second):
		tail := src
		if len(tail) > 80 {
			tail = tail[len(tail)-80:]
		}
		t.Errorf("parse of %s did not terminate; input ends …%q", where, tail)
		return false
	}
}
