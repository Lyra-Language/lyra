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

// `own` on a trait method parameter is **accepted** as of 08/03, once ownership analysis
// covered method bodies (driver.OwnershipByMethod) and use-after-move learned to resolve a
// method callee. It was rejected before that (lyra-E030, now retired): the pass analyzed no
// method body, so a transferred value was dropped by the callee and still used by the
// caller — a heap-use-after-free confirmed under ASan.
//
// The transfer cases live where they can be *run*: llvm_own_receiver_test.go under the ASan
// harness, and use_after_move_test.go for the diagnostic that makes them safe.
func TestEffectBounds_OwnOnTraitParameterIsAccepted(t *testing.T) {
	errs := checkEffectBounds(t, `
trait Consume { take: (Self, own string) -> string }`)
	if len(errs) != 0 {
		t.Fatalf("expected no diagnostic, got %v", errs)
	}
}

// The supported modes are not swept up with it — the rejection must be precise, or it would
// take the working half of the feature with it.
func TestEffectBounds_RefAndMutOnTraitParametersAreAccepted(t *testing.T) {
	for _, src := range []string{
		`trait Bump { bump: (mut Self) -> void }`,
		`trait Peek { peek: (ref Self) -> i64 }`,
		`trait Fill { fill: (Self, mut i64) -> void }`,
		`trait Plain { plain: (Self) -> i64 }`,
	} {
		if errs := checkEffectBounds(t, src); len(errs) != 0 {
			t.Errorf("%q should be accepted, got %v", src, errs)
		}
	}
}
