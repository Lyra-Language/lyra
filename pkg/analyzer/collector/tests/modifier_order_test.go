package collector_test

import (
	"strings"
	"testing"

	"github.com/Lyra-Language/lyra/pkg/analyzer/collector"
	"github.com/Lyra-Language/lyra/pkg/parser"
)

// Modifier order and repetition are collector rules, not grammar rules.
//
// The grammar enforced both until `lambda_expr`'s seven ordered `optional()`s were collapsed
// into one repeated choice — the shape that owned 91% of the parser's states and made
// `src/parser.c` 116 MB. `async pure (…)` and `pure pure (…)` now parse, and are rejected
// here instead, with a message naming the modifier and the canonical order rather than
// pointing at whichever token failed to shift.

// collectDiagnostics collects source and returns the diagnostics, without the t.Fatalf on
// any error that parseAndCollect does — these tests are *about* collector errors.
func collectDiagnostics(t *testing.T, source string) []string {
	t.Helper()
	tree, err := parser.Parse(source)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	c := collector.NewCollector([]byte(source))
	_, _, _, errs := c.Collect(tree.RootNode())
	msgs := make([]string, 0, len(errs))
	for _, e := range errs {
		msgs = append(msgs, e.Error())
	}
	return msgs
}

func assertOneContaining(t *testing.T, msgs []string, want string) {
	t.Helper()
	for _, m := range msgs {
		if strings.Contains(m, want) {
			return
		}
	}
	t.Errorf("expected a diagnostic containing %q, got %v", want, msgs)
}

func TestModifierOrder_OutOfOrderIsRejected(t *testing.T) {
	msgs := collectDiagnostics(t, "let f = async pure (n: i64) -> i64 => n")
	assertOneContaining(t, msgs, "`pure` must come before `async`")
	assertOneContaining(t, msgs, "unsafe, pure|det, noalloc, async, gen, rec")
}

func TestModifierOrder_RepeatedModifierIsRejected(t *testing.T) {
	msgs := collectDiagnostics(t, "let f = pure pure (n: i64) -> i64 => n")
	assertOneContaining(t, msgs, "`pure` is repeated")
}

// The canonical order collects cleanly — the check must not fire on correct code, which is
// the failure mode that would matter most since every annotated function goes through it.
func TestModifierOrder_CanonicalOrderIsAccepted(t *testing.T) {
	for _, src := range []string{
		"let f = pure (n: i64) -> i64 => n",
		"let f = pure noalloc (n: i64) -> i64 => n",
		"let f = det noalloc (n: i64) -> i64 => n",
		"let f = unsafe pure noalloc (n: i64) -> i64 => n",
		"let f = (n: i64) -> i64 => n",
	} {
		if msgs := collectDiagnostics(t, src); len(msgs) != 0 {
			t.Errorf("%q should collect cleanly, got %v", src, msgs)
		}
	}
}

// A modifier keyword appearing *inside* the body is not part of the prefix — the scan stops
// at the first non-modifier child, so a nested annotated lambda cannot be misread as a
// repeat of the outer one.
func TestModifierOrder_NestedLambdaIsNotReadAsARepeat(t *testing.T) {
	msgs := collectDiagnostics(t, "let f = pure (n: i64) -> i64 => { let g = pure (m: i64) -> i64 => m  g(n) }")
	if len(msgs) != 0 {
		t.Errorf("a nested pure lambda is its own prefix, got %v", msgs)
	}
}
