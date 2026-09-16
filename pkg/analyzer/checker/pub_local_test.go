package checker_test

import (
	"strings"
	"testing"

	"github.com/Lyra-Language/lyra/pkg/analyzer/checker"
	"github.com/Lyra-Language/lyra/pkg/analyzer/collector"
	diag "github.com/Lyra-Language/lyra/pkg/diagnostic"
	"github.com/Lyra-Language/lyra/pkg/parser"
)

// The pass needs no types, so this is the collector and the pass and nothing else — the
// same shape continuation_test.go uses.
func pubLocalErrors(t *testing.T, source string) []diag.Diagnostic {
	t.Helper()
	tree, err := parser.Parse(source)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	c := collector.NewCollector([]byte(source))
	program, _, _, _ := c.Collect(tree.RootNode())
	out := checker.CheckPubOnLocalBindings(program)
	for _, d := range out {
		if d.Code != diag.CodePubOnLocalBinding {
			t.Fatalf("unexpected code %q", d.Code)
		}
		if d.Severity != diag.SeverityError {
			t.Fatalf("lyra-E081 must be an error, got %v", d.Severity)
		}
	}
	return out
}

// `pub` on a binding inside a function is refused (lyra-E081), in every spelling.
//
// It used to be accepted and ignored: `pub` exports a name from its module, a local binding
// has none to export, and nothing said so. An error rather than a warning because no program
// wants it and none changes meaning by deleting it.
func TestCheck_PubOnALocalBindingIsRefused(t *testing.T) {
	for _, src := range []string{
		"let main = () -> void => {\n  pub let n = 3\n  println(\"${n}\")\n}",
		"let main = () -> void => {\n  pub var m = 4\n  m += 1\n  println(\"${m}\")\n}",
		"let main = () -> void => {\n  pub const N = 3\n  println(\"${N}\")\n}",
		// Any depth, not just a function's own block.
		"let main = () -> void => {\n  for i in 0..<2 {\n    pub let k = i\n    println(\"${k}\")\n  }\n}",
	} {
		if got := pubLocalErrors(t, src); len(got) == 0 {
			t.Errorf("expected lyra-E081 for:\n%s", src)
		}
	}
}

// The top level is where `pub` means something, and it is decided by identity rather than by
// depth — so a module's own declarations are never touched.
func TestCheck_PubAtTheTopLevelIsUntouched(t *testing.T) {
	src := "pub const N = 3\npub let f = pure () -> i64 => N\nlet main = () -> void => { println(\"${f()}\") }"
	if got := pubLocalErrors(t, src); len(got) != 0 {
		t.Errorf("a top-level `pub` is the point of the modifier; got %v", got)
	}
}

// The message names the spelling the author used, since the fix is to delete one word and
// the three forms read differently.
func TestCheck_PubOnALocalBindingNamesTheBinding(t *testing.T) {
	got := pubLocalErrors(t, "let main = () -> void => {\n  pub const LIMIT = 3\n  println(\"${LIMIT}\")\n}")
	if len(got) != 1 {
		t.Fatalf("want one diagnostic, got %v", got)
	}
	msg := got[0].Message
	if !strings.Contains(msg, "`pub const LIMIT`") || !strings.Contains(msg, "drop the `pub`") {
		t.Errorf("message should name the binding and the fix; got %q", msg)
	}
}
