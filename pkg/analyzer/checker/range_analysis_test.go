package checker_test

import (
	"strings"
	"testing"

	"github.com/Lyra-Language/lyra/pkg/analyzer/checker"
	"github.com/Lyra-Language/lyra/pkg/analyzer/collector"
	"github.com/Lyra-Language/lyra/pkg/analyzer/typechecker"
	diag "github.com/Lyra-Language/lyra/pkg/diagnostic"
	"github.com/Lyra-Language/lyra/pkg/parser"
	"github.com/Lyra-Language/lyra/pkg/typetable"
)

// checkRanges runs the front-end through typechecking (the value-range pass needs
// the TypeTable) and returns the range diagnostics.
func checkRanges(t *testing.T, source string) []diag.Diagnostic {
	t.Helper()
	tree, err := parser.Parse(source)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	c := collector.NewCollector([]byte(source))
	program, symTable, scopeTable, collectErrs := c.Collect(tree.RootNode())
	if len(collectErrs) > 0 {
		t.Fatalf("collector errors: %v", collectErrs)
	}
	tt := typetable.New()
	if errs := typechecker.New(symTable, scopeTable, tt).Check(program); len(errs) > 0 {
		// A range test's source should type-check cleanly; a type error would make
		// the recorded types unreliable.
		t.Fatalf("unexpected type errors: %v", errs)
	}
	return checker.CheckIntegerRanges(program, tt)
}

func onlyDiag(t *testing.T, source, wantCode string, wantSubstr ...string) {
	t.Helper()
	diags := checkRanges(t, source)
	if len(diags) != 1 {
		t.Fatalf("expected exactly 1 diagnostic (%s), got %d: %v", wantCode, len(diags), diags)
	}
	if diags[0].Code != wantCode {
		t.Fatalf("expected code %s, got %s: %q", wantCode, diags[0].Code, diags[0].Message)
	}
	for _, sub := range wantSubstr {
		if !strings.Contains(diags[0].Message, sub) {
			t.Errorf("message %q missing %q", diags[0].Message, sub)
		}
	}
}

func noDiag(t *testing.T, source string) {
	t.Helper()
	if diags := checkRanges(t, source); len(diags) != 0 {
		t.Fatalf("expected no range diagnostics, got %d: %v", len(diags), diags)
	}
}

// ── definite overflow (lyra-E020) ────────────────────────────────────────────

// The flagship case: a branch refinement proves the operand range, so an overflow
// invisible to constant-folding is caught. This is what the pass adds over the
// literal range check.
func TestRange_Overflow_BranchRefinement(t *testing.T) {
	onlyDiag(t, `
		let f = (x: i8) -> i8 => if x > 100 { x + 100 } else { 0 }
		let main = () -> u8 => 0
	`, diag.CodeIntegerOverflow, "always overflows i8", "[201, 227]")
}

// Constant propagation across bindings — two in-range constants whose sum isn't.
func TestRange_Overflow_ConstantPropagation(t *testing.T) {
	onlyDiag(t, `
		let f = () -> u8 => {
			let a: u8 = 200
			let b: u8 = 100
			a + b
		}
		let main = () -> u8 => 0
	`, diag.CodeIntegerOverflow, "always overflows u8")
}

func TestRange_Overflow_Subtraction_BelowZero(t *testing.T) {
	onlyDiag(t, `
		let f = () -> u8 => {
			let a: u8 = 10
			let b: u8 = 20
			a - b
		}
		let main = () -> u8 => 0
	`, diag.CodeIntegerOverflow)
}

func TestRange_Overflow_Multiplication(t *testing.T) {
	onlyDiag(t, `
		let f = () -> i8 => {
			let a: i8 = 100
			let b: i8 = 2
			a * b
		}
		let main = () -> u8 => 0
	`, diag.CodeIntegerOverflow)
}

// A definite overflow only on one path (the then-branch), reached via refinement,
// is still an error — the branch runs with those values.
func TestRange_Overflow_CompoundAssign_Refined(t *testing.T) {
	onlyDiag(t, `
		let f = (x: u8) -> u8 => {
			var v = x
			if v > 250 {
				v += 10
			}
			v
		}
		let main = () -> u8 => 0
	`, diag.CodeIntegerOverflow)
}

// ── no false positives on overflow ───────────────────────────────────────────

// Two full-range i8s can overflow but need not — a *possible* overflow is left to
// the runtime trap, never flagged.
func TestRange_NoOverflow_PossibleButNotDefinite(t *testing.T) {
	noDiag(t, `
		let f = (x: i8, y: i8) -> i8 => x + y
		let main = () -> u8 => 0
	`)
}

// A refinement that keeps the sum in range must not be flagged.
func TestRange_NoOverflow_RefinedSafe(t *testing.T) {
	noDiag(t, `
		let f = (x: i8) -> i8 => if x < 20 { x + 100 } else { 0 }
		let main = () -> u8 => 0
	`)
}

// i64 arithmetic is ⊤ (the math doesn't fit int64), so it's never flagged.
func TestRange_NoOverflow_I64(t *testing.T) {
	noDiag(t, `
		let f = (x: i64, y: i64) -> i64 => x + y
		let main = () -> u8 => 0
	`)
}

// A loop havocs its counter, so arithmetic on it is ⊤ — no false overflow.
func TestRange_NoOverflow_LoopCounter(t *testing.T) {
	noDiag(t, `
		let f = () -> u8 => {
			var sum: u8 = 0
			for var i: u8 = 0; i < 3; i += 1 {
				sum += i
			}
			sum
		}
		let main = () -> u8 => 0
	`)
}

// ── constant comparison (lyra-W011) ──────────────────────────────────────────

func TestRange_Comparison_U8_LessThanZero_AlwaysFalse(t *testing.T) {
	onlyDiag(t, `
		let f = (x: u8) -> u8 => if x < 0 { 1 } else { 0 }
		let main = () -> u8 => 0
	`, diag.CodeConstantComparison, "always false")
}

func TestRange_Comparison_U8_GteZero_AlwaysTrue(t *testing.T) {
	onlyDiag(t, `
		let f = (x: u8) -> u8 => if x >= 0 { 1 } else { 0 }
		let main = () -> u8 => 0
	`, diag.CodeConstantComparison, "always true")
}

// A comparison made constant by an earlier refinement (nested if).
func TestRange_Comparison_RefinementContradiction(t *testing.T) {
	onlyDiag(t, `
		let f = (x: i32) -> i32 => if x >= 10 { if x < 5 { 1 } else { 2 } } else { 0 }
		let main = () -> u8 => 0
	`, diag.CodeConstantComparison, "always false")
}

// u8 can't exceed 255, so `> 300` is always false.
func TestRange_Comparison_U8_AboveMax_AlwaysFalse(t *testing.T) {
	onlyDiag(t, `
		let f = (x: u8) -> u8 => if x > 300 { 1 } else { 0 }
		let main = () -> u8 => 0
	`, diag.CodeConstantComparison, "always false")
}

// ── no false comparison warnings ─────────────────────────────────────────────

// A genuinely variable comparison (both outcomes possible) is not flagged.
func TestRange_NoComparison_GenuinelyVariable(t *testing.T) {
	noDiag(t, `
		let f = (x: u8) -> u8 => if x < 100 { 1 } else { 0 }
		let main = () -> u8 => 0
	`)
}

// A loop condition on a havoc'd counter is not constant.
func TestRange_NoComparison_LoopCondition(t *testing.T) {
	noDiag(t, `
		let f = (n: u8) -> u8 => {
			var total: u8 = 0
			for var i: u8 = 0; i < 3; i += 1 {
				total = i
			}
			total
		}
		let main = () -> u8 => 0
	`)
}
