package collector_test

import (
	"strings"
	"testing"

	diag "github.com/Lyra-Language/lyra/pkg/diagnostic"
)

// A range pattern must say whether its end bound is included (`lyra-E032`).
//
// The grammar accepts `0..9`, and every reader of `RangePattern.EndOperator`
// tests `== "<"` — so an empty operator fell through to *inclusive* and `0..9`
// silently meant `0..<=9`. The extra value is not cosmetic: it is the boundary the
// exhaustiveness checker and the emitted comparison would disagree on.

func rangeEndOperatorErrors(t *testing.T, source string) []diag.Diagnostic {
	t.Helper()
	var out []diag.Diagnostic
	for _, raw := range parseAndCollectErrors(t, source) {
		d, ok := raw.(diag.Diagnostic)
		if ok && d.Code == diag.CodeMissingRangeEndOperator {
			out = append(out, d)
		}
	}
	return out
}

func TestRangePattern_MissingEndOperatorIsRejected(t *testing.T) {
	got := rangeEndOperatorErrors(t, `
		let f = (n: i64) -> i64 => match n {
			0..9 => 1,
			_ => 0,
		}
	`)
	if len(got) != 1 {
		t.Fatalf("expected 1 missing-end-operator error, got %d: %v", len(got), got)
	}
	if got[0].Severity != diag.SeverityError {
		t.Errorf("should be an error, got severity %v", got[0].Severity)
	}
	// The message must show both fixes, not just name the problem.
	for _, want := range []string{"0..<=9", "0..<9"} {
		if !strings.Contains(got[0].Message, want) {
			t.Errorf("message should offer %q: %q", want, got[0].Message)
		}
	}
}

func TestRangePattern_InclusiveIsAccepted(t *testing.T) {
	if got := rangeEndOperatorErrors(t, `
		let f = (n: i64) -> i64 => match n {
			0..<=9 => 1,
			_ => 0,
		}
	`); len(got) != 0 {
		t.Fatalf("`..<=` is explicit and must not be flagged, got: %v", got)
	}
}

func TestRangePattern_ExclusiveIsAccepted(t *testing.T) {
	if got := rangeEndOperatorErrors(t, `
		let f = (n: i64) -> i64 => match n {
			0..<9 => 1,
			_ => 0,
		}
	`); len(got) != 0 {
		t.Fatalf("`..<` is explicit and must not be flagged, got: %v", got)
	}
}

// A signed bound goes through `_signed_number_literal`, a separate grammar path
// — the check must not be blind to it.
func TestRangePattern_MissingEndOperatorWithSignedBounds(t *testing.T) {
	if got := rangeEndOperatorErrors(t, `
		let f = (n: i64) -> i64 => match n {
			-128..-1 => 1,
			_ => 0,
		}
	`); len(got) != 1 {
		t.Fatalf("expected 1 error on a signed operator-less range, got %d: %v", len(got), got)
	}
}

// Each offending arm is reported, so a `match` full of them does not report one.
func TestRangePattern_EachArmReported(t *testing.T) {
	if got := rangeEndOperatorErrors(t, `
		let f = (n: i64) -> i64 => match n {
			0..9 => 1,
			10..19 => 2,
			_ => 0,
		}
	`); len(got) != 2 {
		t.Fatalf("expected 2 errors, got %d: %v", len(got), got)
	}
}

// The same rule, at the other two sites the `..` notation appears. Before the
// three range grammars were unified these were three separate rules with three
// different strictness settings; one shared collector check (ctx.RangeEndOperator)
// now enforces one rule at all three, and the suggestion is spliced from the
// source so it is right for each form.

func TestRangeExpr_MissingEndOperatorIsRejected(t *testing.T) {
	got := rangeEndOperatorErrors(t, `let r = 0..10`)
	if len(got) != 1 {
		t.Fatalf("expected 1 error on a range expression, got %d: %v", len(got), got)
	}
	for _, want := range []string{"range expression", "0..<=10", "0..<10"} {
		if !strings.Contains(got[0].Message, want) {
			t.Errorf("message should contain %q: %q", want, got[0].Message)
		}
	}
}

func TestRangeExpr_ExplicitOperatorsAccepted(t *testing.T) {
	for _, src := range []string{`let r = 0..<10`, `let r = 0..<=10`, `let r = 0..<=10:2`} {
		if got := rangeEndOperatorErrors(t, src); len(got) != 0 {
			t.Errorf("%s must not be flagged, got: %v", src, got)
		}
	}
}

// A stepped range splices at the first `..`, leaving the `:2` in place.
func TestRangeExpr_SuggestionKeepsStep(t *testing.T) {
	got := rangeEndOperatorErrors(t, `let r = 0..10:2`)
	if len(got) != 1 {
		t.Fatalf("expected 1 error, got %d: %v", len(got), got)
	}
	if !strings.Contains(got[0].Message, "0..<=10:2") {
		t.Errorf("suggestion should keep the step: %q", got[0].Message)
	}
}

func TestRangeConstraint_MissingEndOperatorIsRejected(t *testing.T) {
	got := rangeEndOperatorErrors(t, `newtype Pct = u8 where range(0..100)`)
	if len(got) != 1 {
		t.Fatalf("expected 1 error on a range constraint, got %d: %v", len(got), got)
	}
	for _, want := range []string{"range constraint", "range(0..<=100)", "range(0..<100)"} {
		if !strings.Contains(got[0].Message, want) {
			t.Errorf("message should contain %q: %q", want, got[0].Message)
		}
	}
}

func TestRangeConstraint_ExplicitAndOpenFormsAccepted(t *testing.T) {
	for _, src := range []string{
		`newtype Pct = u8 where range(0..<=100)`,
		`newtype Angle = f64 where range(0..<360)`,
		`newtype AtLeast = i64 where range(0..)`, // open end: no operator to write
		`newtype Below = i64 where range(..<10)`,
	} {
		if got := rangeEndOperatorErrors(t, src); len(got) != 0 {
			t.Errorf("%s must not be flagged, got: %v", src, got)
		}
	}
}

// An open-ended *pattern* has no end bound, so there is no operator to require.
func TestRangePattern_OpenEndNeedsNoOperator(t *testing.T) {
	if got := rangeEndOperatorErrors(t, `
		let f = (n: i64) -> i64 => match n {
			10.. => 1,
			_ => 0,
		}
	`); len(got) != 0 {
		t.Fatalf("an open-ended range has no end bound to qualify, got: %v", got)
	}
}

// --- the two step spellings, held to one rule (lyra-E033) ---
//
// An expression range's `:step` and a newtype's `step()` constraint stay separate
// spellings on purpose, but they must not mean different things. Before this,
// the expression step was checked for numeric type-compatibility only and the
// constraint step was validated by nothing at all.

func stepErrors(t *testing.T, source string) []diag.Diagnostic {
	t.Helper()
	var out []diag.Diagnostic
	for _, raw := range parseAndCollectErrors(t, source) {
		d, ok := raw.(diag.Diagnostic)
		if ok && d.Code == diag.CodeInvalidRangeStep {
			out = append(out, d)
		}
	}
	return out
}

func TestStepConstraint_ZeroIsRejected(t *testing.T) {
	got := stepErrors(t, `newtype Q = f32 where step(0)`)
	if len(got) != 1 {
		t.Fatalf("expected 1 invalid-step error, got %d: %v", len(got), got)
	}
	if !strings.Contains(got[0].Message, "never advances") {
		t.Errorf("message should say why: %q", got[0].Message)
	}
}

func TestStepConstraint_FractionalOverIntegerBaseIsRejected(t *testing.T) {
	got := stepErrors(t, `newtype N = u8 where range(0..<=100), step(0.5)`)
	if len(got) != 1 {
		t.Fatalf("expected 1 invalid-step error, got %d: %v", len(got), got)
	}
	if !strings.Contains(got[0].Message, "u8") {
		t.Errorf("message should name the base type: %q", got[0].Message)
	}
}

// The motivating good case: a quarter-step over a float domain, which is exactly
// what the constraint spelling is for and what `saturating_add` cannot express.
func TestStepConstraint_FractionalOverFloatBaseIsAccepted(t *testing.T) {
	if got := stepErrors(t, `newtype Quarter = f32 where range(0..<=100), step(0.25)`); len(got) != 0 {
		t.Fatalf("a fractional step over f32 is legal, got: %v", got)
	}
}

func TestStepConstraint_WholeStepOverIntegerBaseIsAccepted(t *testing.T) {
	if got := stepErrors(t, `newtype Even = u8 where range(0..<=100), step(2)`); len(got) != 0 {
		t.Fatalf("a whole step over u8 is legal, got: %v", got)
	}
}

// A step that folds to no constant is legal and simply not decidable here.
func TestStepConstraint_NonConstantStepIsNotFlagged(t *testing.T) {
	if got := stepErrors(t, `newtype S = f32 where step(SOME_CONST)`); len(got) != 0 {
		t.Fatalf("a non-constant step cannot be judged here, got: %v", got)
	}
}
