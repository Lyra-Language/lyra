package checker_test

import (
	"testing"

	"github.com/Lyra-Language/lyra/pkg/analyzer/checker"
	"github.com/Lyra-Language/lyra/pkg/analyzer/collector"
	"github.com/Lyra-Language/lyra/pkg/parser"
)

func checkEffectBounds(t *testing.T, source string) []checker.EffectBoundsError {
	t.Helper()
	tree, err := parser.Parse(source)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	c := collector.NewCollector([]byte(source))
	program, _, _, errs := c.Collect(tree.RootNode())
	if len(errs) > 0 {
		t.Fatalf("collector errors: %v", errs)
	}
	return checker.CheckEffectBounds(program)
}

func assertNoEffectBoundErrors(t *testing.T, source string) {
	t.Helper()
	if errs := checkEffectBounds(t, source); len(errs) != 0 {
		t.Fatalf("expected no effect-bound errors, got %d: %v", len(errs), errs)
	}
}

func assertPureDetConflict(t *testing.T, source string) {
	t.Helper()
	errs := checkEffectBounds(t, source)
	if len(errs) != 1 {
		t.Fatalf("expected 1 effect-bound error, got %d: %v", len(errs), errs)
	}
	if errs[0].Code != "lyra-E015" {
		t.Errorf("wrong code: got %q, want lyra-E015", errs[0].Code)
	}
}

func TestEffectBounds_PureAndDet_Conflicts(t *testing.T) {
	assertPureDetConflict(t, "let f = pure det (x: i64) -> i64 => { x }")
}

func TestEffectBounds_DetAlone_OK(t *testing.T) {
	assertNoEffectBoundErrors(t, "let f = det (x: i64) -> i64 => { x }")
}

func TestEffectBounds_PureAlone_OK(t *testing.T) {
	assertNoEffectBoundErrors(t, "let f = pure (x: i64) -> i64 => { x }")
}

// noalloc is an orthogonal resource bound — it stacks with any purity rung and
// must never be flagged as a conflict.
func TestEffectBounds_PureNoalloc_OK(t *testing.T) {
	assertNoEffectBoundErrors(t, "let f = pure noalloc (x: i64) -> i64 => { x }")
}

func TestEffectBounds_DetNoalloc_OK(t *testing.T) {
	assertNoEffectBoundErrors(t, "let f = det noalloc (x: i64) -> i64 => { x }")
}

func TestEffectBounds_TraitMethod_PureAndDet_Conflicts(t *testing.T) {
	assertPureDetConflict(t, `impl Physics for World {
  tick = pure det (self) => { self }
}`)
}

func TestEffectBounds_TraitMethod_DetAlone_OK(t *testing.T) {
	assertNoEffectBoundErrors(t, `impl Physics for World {
  tick = det (self) => { self }
}`)
}
