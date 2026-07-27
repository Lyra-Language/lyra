package checker_test

import (
	"strings"
	"testing"

	"github.com/Lyra-Language/lyra/pkg/analyzer/checker"
	"github.com/Lyra-Language/lyra/pkg/analyzer/collector"
	"github.com/Lyra-Language/lyra/pkg/analyzer/typechecker"
	"github.com/Lyra-Language/lyra/pkg/ast"
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
	diags, _ := checker.CheckIntegerRanges(program, tt)
	return diags
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

// ── definite divide-by-zero (lyra-E021) ─────────────────────────────────────

// A variable bound to a constant zero and used as a divisor — the typechecker's
// constant-fold check doesn't propagate the binding, so this is the range pass's
// to catch.
func TestRange_DivByZero_ConstantAssignedVar(t *testing.T) {
	onlyDiag(t, `
		let f = (a: u8) -> u8 => {
			let b: u8 = 0
			a / b
		}
		let main = () -> u8 => 0
	`, diag.CodeDivideByZero, "always divides by zero", "b is always 0")
}

// The flagship flow-sensitive case: a branch refinement proves the divisor is 0.
func TestRange_DivByZero_Refinement(t *testing.T) {
	onlyDiag(t, `
		let f = (a: u8, b: u8) -> u8 => if b == 0 { a / b } else { a }
		let main = () -> u8 => 0
	`, diag.CodeDivideByZero, "b is always 0")
}

// %% (floored remainder) traps on a zero divisor too, so it's caught the same way.
func TestRange_DivByZero_Remainder(t *testing.T) {
	onlyDiag(t, `
		let f = (a: u8) -> u8 => {
			let b: u8 = 0
			a %% b
		}
		let main = () -> u8 => 0
	`, diag.CodeDivideByZero)
}

// ── no false divide-by-zero ──────────────────────────────────────────────────

// A full-range divisor *can* be zero but need not be — a merely *possible*
// divide-by-zero is left to the runtime trap, never flagged.
func TestRange_NoDivByZero_PossibleZero(t *testing.T) {
	noDiag(t, `
		let f = (a: u8, b: u8) -> u8 => a / b
		let main = () -> u8 => 0
	`)
}

// A provably-nonzero divisor is fine (and gets its trap elided, separately).
func TestRange_NoDivByZero_NonzeroConstant(t *testing.T) {
	noDiag(t, `
		let f = (a: u8) -> u8 => {
			let b: u8 = 2
			a / b
		}
		let main = () -> u8 => 0
	`)
}

// A divisor refined to nonzero on the reached path is not flagged.
func TestRange_NoDivByZero_RefinedNonzero(t *testing.T) {
	noDiag(t, `
		let f = (a: u8, b: u8) -> u8 => if b != 0 { a / b } else { a }
		let main = () -> u8 => 0
	`)
}

// ── definite out-of-bounds index (lyra-E022) ────────────────────────────────

// The flagship flow-sensitive case: a refinement proves the index is past the end.
func TestRange_IndexOOB_Refinement(t *testing.T) {
	onlyDiag(t, `
		let f = (xs: [3]u8, i: u8) -> u8 => if i >= 3 { xs[i] } else { 0 }
		let main = () -> u8 => 0
	`, diag.CodeIndexOutOfBounds, "always out of bounds", "size-3")
}

// A negative index counts from the end, so a refinement below -size is also a
// definite out-of-bounds.
func TestRange_IndexOOB_NegativeRefinement(t *testing.T) {
	onlyDiag(t, `
		let f = (xs: [3]u8, i: i64) -> u8 => if i < -3 { xs[i] } else { 0 }
		let main = () -> u8 => 0
	`, diag.CodeIndexOutOfBounds)
}

// ── no false out-of-bounds ───────────────────────────────────────────────────

// A full-range index *can* be out of bounds but need not be — a merely *possible*
// OOB is left to the runtime trap, never flagged.
func TestRange_NoIndexOOB_Possible(t *testing.T) {
	noDiag(t, `
		let f = (xs: [3]u8, i: u8) -> u8 => xs[i]
		let main = () -> u8 => 0
	`)
}

// An index refined into range is fine (and gets its bounds check elided, separately).
func TestRange_NoIndexOOB_RefinedInBounds(t *testing.T) {
	noDiag(t, `
		let f = (xs: [3]u8, i: u8) -> u8 => if i < 3 { xs[i] } else { 0 }
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

// ── u64 tracking ─────────────────────────────────────────────────────────────
//
// u64's true max (2^64-1) doesn't fit int64, so its interval upper is the +∞
// sentinel. The lower bound (0) is exact and load-bearing; every u64 diagnostic
// keys off it (or an "entirely outside" test), never off the fake upper.

// u64 >= 0 is always true — the lower bound of 0 is exact.
func TestRange_U64_GteZero_AlwaysTrue(t *testing.T) {
	onlyDiag(t, `
		let f = (x: u64) -> u64 => if x >= 0 { 1 } else { 0 }
		let main = () -> u8 => 0
	`, diag.CodeConstantComparison, "always true")
}

// u64 < 0 is always false.
func TestRange_U64_LtZero_AlwaysFalse(t *testing.T) {
	onlyDiag(t, `
		let f = (x: u64) -> u64 => if x < 0 { 1 } else { 0 }
		let main = () -> u8 => 0
	`, diag.CodeConstantComparison, "always false")
}

// A u64 index refined past the end is a definite out-of-bounds (E022), shown with
// the +∞ upper rather than the misleading sentinel value.
func TestRange_U64_RefinedIndex_OutOfBounds(t *testing.T) {
	onlyDiag(t, `
		let f = (xs: [3]u64, i: u64) -> u64 => if i >= 3 { xs[i] } else { 0 }
		let main = () -> u8 => 0
	`, diag.CodeIndexOutOfBounds, "always out of bounds", "+∞")
}

// A u64 subtraction proven to go below zero is a definite underflow (E020) — the
// lower bound drives it, so the +∞ upper is irrelevant.
func TestRange_U64_Underflow(t *testing.T) {
	onlyDiag(t, `
		let f = (a: u64) -> u64 => if a < 5 { a - 10 } else { 0 }
		let main = () -> u8 => 0
	`, diag.CodeIntegerOverflow, "always overflows u64")
}

// ── u64 soundness: the +∞ upper must never manufacture a diagnostic ──────────

// The crux: `x > MaxInt64` is genuinely satisfiable for a u64 (its true max is far
// larger), so it must NOT fold to always-false — the compareConst sentinel guard.
func TestRange_U64_GtMaxInt64_NotConstant(t *testing.T) {
	noDiag(t, `
		let f = (x: u64) -> u64 => if x > 9223372036854775807 { 1 } else { 0 }
		let main = () -> u8 => 0
	`)
}

// Two full-range u64s can overflow on add but need not — a merely *possible*
// overflow is left to the runtime trap, never flagged.
func TestRange_U64_FullRangeAdd_NoDiag(t *testing.T) {
	noDiag(t, `
		let f = (a: u64, b: u64) -> u64 => a + b
		let main = () -> u8 => 0
	`)
}

// A u64 variable bound to 0 and used as a divisor is a definite divide-by-zero.
func TestRange_U64_DivByZero(t *testing.T) {
	onlyDiag(t, `
		let f = (a: u64) -> u64 => {
			let b: u64 = 0
			a / b
		}
		let main = () -> u8 => 0
	`, diag.CodeDivideByZero, "b is always 0")
}

// ── per-match-arm scrutinee refinement ──────────────────────────────────────
//
// A match arm refines the scrutinee variable to the values its pattern matches, so
// a definite fault inside the arm is caught — the `match` analogue of `if`-branch
// refinement.

// A range pattern refines the scrutinee so an overflow inside the arm is definite.
func TestRange_Match_ArmRange_Overflow(t *testing.T) {
	onlyDiag(t, `
		let f = (x: i8) -> i8 => match x {
			100..=127 => x + 100,
			_ => 0,
		}
		let main = () -> u8 => 0
	`, diag.CodeIntegerOverflow, "always overflows i8", "[200, 227]")
}

// A literal arm refines the scrutinee to a single value — here the scrutinee is
// itself the divisor, so the arm is a definite divide-by-zero.
func TestRange_Match_ArmLiteral_DivByZero(t *testing.T) {
	onlyDiag(t, `
		let f = (a: i64, x: i64) -> i64 => match x {
			0 => a / x,
			_ => 1,
		}
		let main = () -> u8 => 0
	`, diag.CodeDivideByZero, "x is always 0")
}

// A range pattern refines an index past the end → definite out-of-bounds.
func TestRange_Match_ArmRange_OutOfBounds(t *testing.T) {
	onlyDiag(t, `
		let f = (xs: [3]i64, i: i64) -> i64 => match i {
			5..=10 => xs[i],
			_ => 0,
		}
		let main = () -> u8 => 0
	`, diag.CodeIndexOutOfBounds, "always out of bounds", "[5, 10]")
}

// An exclusive-end range `..<` refines to [start, end-1]: 3..<6 on a size-3 array
// is [3,5], entirely out of bounds.
func TestRange_Match_ArmExclusiveRange_OutOfBounds(t *testing.T) {
	onlyDiag(t, `
		let f = (xs: [3]i64, i: i64) -> i64 => match i {
			3..<6 => xs[i],
			_ => 0,
		}
		let main = () -> u8 => 0
	`, diag.CodeIndexOutOfBounds, "[3, 5]")
}

// ── no false positives from refinement ───────────────────────────────────────

// An in-range arm is safe — the refinement keeps the result within the type.
func TestRange_Match_ArmInRange_NoDiag(t *testing.T) {
	noDiag(t, `
		let f = (x: i8) -> i8 => match x {
			0..=10 => x + 100,
			_ => 0,
		}
		let main = () -> u8 => 0
	`)
}

// A catch-all / identifier arm refines nothing — the scrutinee keeps its full
// range, so no spurious fault is reported.
func TestRange_Match_CatchAll_NoDiag(t *testing.T) {
	noDiag(t, `
		let f = (x: i8) -> i8 => match x {
			0 => 0,
			n => n + 1,
		}
		let main = () -> u8 => 0
	`)
}

// ── flow-sensitive range-constraint enforcement (lyra-E023) ──────────────────
//
// The flow-sensitive twin of the typechecker's constant-value RangeConstraint
// check: a non-constant value proven entirely outside a range-constrained
// newtype's range is an error.

// A variable refined past the constraint's range is a definite violation.
func TestRange_Constraint_RefinedVar(t *testing.T) {
	onlyDiag(t, `
		newtype Percent = u8 where range(0..=100)
		let f = (x: u8) -> u8 => if x > 100 { let p: Percent = x
			0
		} else { 0 }
		let main = () -> u8 => 0
	`, diag.CodeRangeConstraintViolation, "always outside the range 0..=100 of Percent", "[101, 255]")
}

// A binding whose constant value propagates — the typechecker can't fold the
// identifier `y`, so this is the range pass's to catch.
func TestRange_Constraint_ConstPropagatedBinding(t *testing.T) {
	onlyDiag(t, `
		newtype Percent = u8 where range(0..=100)
		let f = () -> u8 => {
			let y: u8 = 150
			let p: Percent = y
			0
		}
		let main = () -> u8 => 0
	`, diag.CodeRangeConstraintViolation, "[150, 150]")
}

// ── no false range-constraint violations ─────────────────────────────────────

// A variable refined into range is fine.
func TestRange_Constraint_RefinedInRange_NoDiag(t *testing.T) {
	noDiag(t, `
		newtype Percent = u8 where range(0..=100)
		let f = (x: u8) -> u8 => if x < 50 { let p: Percent = x
			0
		} else { 0 }
		let main = () -> u8 => 0
	`)
}

// A value that merely *might* be out of range (full-range param) is left to the
// runtime — a possible, not definite, violation.
func TestRange_Constraint_PossibleNotDefinite_NoDiag(t *testing.T) {
	noDiag(t, `
		newtype Percent = u8 where range(0..=100)
		let f = (x: u8) -> u8 => {
			let p: Percent = x
			0
		}
		let main = () -> u8 => 0
	`)
}

// ── safety table (trap elision) ──────────────────────────────────────────────

// firstAddIsSafe runs the pass ONCE and reports whether the first `+` expression
// in the (single) collected program is in the safety table. Both the table and
// the node come from the same AST, so the pointer-keyed lookup matches.
func firstAddIsSafe(t *testing.T, source string) bool {
	t.Helper()
	tree, err := parser.Parse(source)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	program, symTable, scopeTable, _ := collector.NewCollector([]byte(source)).Collect(tree.RootNode())
	tt := typetable.New()
	typechecker.New(symTable, scopeTable, tt).Check(program)
	_, safety := checker.CheckIntegerRanges(program, tt)

	var add ast.Expression
	for _, s := range program.Statements {
		ast.WalkStmt(s.(ast.Statement), func(ast.Statement) bool { return true }, func(e ast.Expression) bool {
			if bin, ok := e.(*ast.MathBinaryOpExpr); ok && bin.Operator == ast.MathBinaryOpAdd && add == nil {
				add = bin
			}
			return true
		})
	}
	if add == nil {
		t.Fatal("no `+` expression found")
	}
	return safety.NoOverflow(add)
}

func TestRange_Safety_ProvableAddIsMarked(t *testing.T) {
	src := `let f = () -> u8 => {
		let a: u8 = 5
		let b: u8 = 3
		a + b
	}
	let main = () -> u8 => 0`
	if !firstAddIsSafe(t, src) {
		t.Error("a provably-non-overflowing add should be in the safety table")
	}
}

func TestRange_Safety_UnprovableAddNotMarked(t *testing.T) {
	src := `let f = (a: u8, b: u8) -> u8 => a + b
	let main = () -> u8 => 0`
	if firstAddIsSafe(t, src) {
		t.Error("a possibly-overflowing add must NOT be in the safety table")
	}
}

// analyzeForSafety runs the front-end + range pass and returns the program (to find
// nodes in) and the SafetyTable (both from the one AST, so pointer lookups match).
func analyzeForSafety(t *testing.T, source string) (*ast.Program, *checker.SafetyTable) {
	t.Helper()
	tree, err := parser.Parse(source)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	program, symTable, scopeTable, _ := collector.NewCollector([]byte(source)).Collect(tree.RootNode())
	tt := typetable.New()
	typechecker.New(symTable, scopeTable, tt).Check(program)
	_, safety := checker.CheckIntegerRanges(program, tt)
	return program, safety
}

// firstExpr returns the first expression matching pred, in walk order.
func firstExpr(t *testing.T, program *ast.Program, pred func(ast.Expression) bool) ast.Expression {
	t.Helper()
	var found ast.Expression
	for _, s := range program.Statements {
		ast.WalkStmt(s.(ast.Statement), func(ast.Statement) bool { return true }, func(e ast.Expression) bool {
			if found == nil && pred(e) {
				found = e
			}
			return true
		})
	}
	if found == nil {
		t.Fatal("no matching expression found")
	}
	return found
}

func isDivExpr(e ast.Expression) bool {
	b, ok := e.(*ast.MathBinaryOpExpr)
	return ok && (b.Operator == ast.MathBinaryOpDiv || b.Operator == ast.MathBinaryOpMod || b.Operator == ast.MathBinaryOpRemainder)
}

func isIndexExpr(e ast.Expression) bool { _, ok := e.(*ast.IndexExpr); return ok }

func TestRange_Safety_ProvableDivIsMarked(t *testing.T) {
	// A literal divisor is provably nonzero; an unsigned op can't be signed INT_MIN/-1.
	program, safety := analyzeForSafety(t, `
		let f = (a: u8) -> u8 => a / 2
		let main = () -> u8 => 0
	`)
	div := firstExpr(t, program, isDivExpr)
	if !safety.NoDivZero(div) {
		t.Error("division by a nonzero constant should be marked NoDivZero")
	}
	if !safety.NoDivOverflow(div) {
		t.Error("unsigned division can't overflow — should be marked NoDivOverflow")
	}
}

func TestRange_Safety_UnprovableDivNotMarked(t *testing.T) {
	// A parameter divisor spans the whole type (includes 0), and the signed dividend
	// spans INT_MIN with a possibly-(-1) divisor: neither fact is provable.
	program, safety := analyzeForSafety(t, `
		let f = (a: i32, b: i32) -> i32 => a / b
		let main = () -> u8 => 0
	`)
	div := firstExpr(t, program, isDivExpr)
	if safety.NoDivZero(div) {
		t.Error("division by a full-range parameter must NOT be marked NoDivZero")
	}
	if safety.NoDivOverflow(div) {
		t.Error("a full-range signed division must NOT be marked NoDivOverflow")
	}
}

func TestRange_Safety_ProvableIndexIsMarked(t *testing.T) {
	// Branch refinement proves i ∈ [0,9] in the then-branch, within a size-10 array.
	program, safety := analyzeForSafety(t, `
		let get = (xs: [10]u8, i: u8) -> u8 => if i < 10 { xs[i] } else { 0 }
		let main = () -> u8 => 0
	`)
	idx := firstExpr(t, program, isIndexExpr)
	if !safety.IndexInBounds(idx) {
		t.Error("a refined index i ∈ [0,9] into a size-10 array should be marked in-bounds")
	}
}

func TestRange_Safety_ProvableLoopIndexIsMarked(t *testing.T) {
	// The loop-widening fixpoint tracks the counter i ∈ [0,2], within a size-3 array.
	program, safety := analyzeForSafety(t, `let main = () -> u8 => {
		let xs: [3]u8 = [10, 20, 30]
		var sum: u8 = 0
		for var i = 0; i < 3; i += 1 {
			sum += xs[i]
		}
		sum
	}`)
	idx := firstExpr(t, program, isIndexExpr)
	if !safety.IndexInBounds(idx) {
		t.Error("a loop counter i ∈ [0,2] indexing a size-3 array should be marked in-bounds")
	}
}

func TestRange_Safety_UnprovableIndexNotMarked(t *testing.T) {
	// A parameter index spans the whole u8 range (0..255), well past size 3.
	program, safety := analyzeForSafety(t, `
		let get = (xs: [3]u8, i: u8) -> u8 => xs[i]
		let main = () -> u8 => 0
	`)
	idx := firstExpr(t, program, isIndexExpr)
	if safety.IndexInBounds(idx) {
		t.Error("a full-range u8 index into a size-3 array must NOT be marked in-bounds")
	}
}

func TestRange_Safety_MatchRefinedIndexInBounds(t *testing.T) {
	// A match arm refines i to [0,2], so the size-3 index is provably in bounds and
	// its bounds check is elided — the elision counterpart to match-arm refinement.
	program, safety := analyzeForSafety(t, `
		let get = (xs: [3]u8, i: u8) -> u8 => match i {
			0..=2 => xs[i],
			_ => 0,
		}
		let main = () -> u8 => 0
	`)
	idx := firstExpr(t, program, isIndexExpr)
	if !safety.IndexInBounds(idx) {
		t.Error("a match arm refining the index to [0,2] should mark the size-3 index in-bounds")
	}
}

// ── precise loop widening ────────────────────────────────────────────────────

// The killer case: a widening/narrowing fixpoint tracks the counter precisely
// (both sides — init gives the lower bound, the guard the upper), so an overflow
// definite only across the counter's real range is caught. Havoc could not: with
// i ∈ [0,255] the sum would merely straddle.
func TestRange_Loop_CounterOverflow(t *testing.T) {
	onlyDiag(t, `
		let f = () -> u8 => {
			var last: u8 = 0
			for var i: u8 = 200; i < 250; i += 1 {
				last = i + 100
			}
			last
		}
		let main = () -> u8 => 0
	`, diag.CodeIntegerOverflow, "always overflows u8", "[300, 349]")
}

// The precise upper bound makes a comparison in the body constant.
func TestRange_Loop_CounterComparison_UpperBound(t *testing.T) {
	onlyDiag(t, `
		let f = () -> u8 => {
			var r: u8 = 0
			for var i: u8 = 0; i < 3; i += 1 {
				if i < 10 { r = 1 } else { r = 2 }
			}
			r
		}
		let main = () -> u8 => 0
	`, diag.CodeConstantComparison, "always true")
}

// A downward counter is tracked too (narrowing recovers the lower bound from the
// `i > 0` guard, the init gives the upper): i ∈ [1,10], so `i > 50` is constant.
func TestRange_Loop_DownwardCounter(t *testing.T) {
	onlyDiag(t, `
		let f = () -> u8 => {
			var r: u8 = 0
			for var i: u8 = 10; i > 0; i -= 1 {
				if i > 50 { r = 1 } else { r = 2 }
			}
			r
		}
		let main = () -> u8 => 0
	`, diag.CodeConstantComparison, "always false")
}

// An accumulator (no bounding guard) still widens to ⊤ — no false overflow on a
// loop that in fact never overflows but whose bound the analysis can't know.
func TestRange_Loop_AccumulatorNoFalsePositive(t *testing.T) {
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

// A large iteration bound must not blow up analysis time — widening reaches a
// fixpoint in a handful of steps, not a million. (A hang would fail the suite.)
func TestRange_Loop_LargeBoundTerminates(t *testing.T) {
	noDiag(t, `
		let f = () -> i32 => {
			var acc: i32 = 0
			for var i: i32 = 0; i < 1000000; i += 1 {
				acc = i
			}
			acc
		}
		let main = () -> u8 => 0
	`)
}

// Nested loops analyze without crashing and stay sound (each level runs its own
// fixpoint; the inner counter is precise inside the outer).
func TestRange_Loop_Nested(t *testing.T) {
	noDiag(t, `
		let f = () -> u8 => {
			var r: u8 = 0
			for var i: u8 = 0; i < 3; i += 1 {
				for var j: u8 = 0; j < 3; j += 1 {
					r = i
				}
			}
			r
		}
		let main = () -> u8 => 0
	`)
}

// ── for…in range widening ────────────────────────────────────────────────────
//
// A numeric-range iterable bounds the loop variable precisely (the for-in analogue
// of the C-style loop counter), when the range is provably non-empty.

// A range that indexes past the end is a definite out-of-bounds — the loop
// definitely runs (5 < 10), so `xs[i]` at i = 5 traps.
func TestRange_ForIn_IndexPastEnd(t *testing.T) {
	onlyDiag(t, `
		let f = (xs: [3]u8) -> u8 => {
			var s: u8 = 0
			for i in 5..<10 {
				s += xs[i]
			}
			s
		}
		let main = () -> u8 => 0
	`, diag.CodeIndexOutOfBounds, "always out of bounds", "[5, 9]")
}

// A range counter that stays in bounds is fine (and its check is elided).
func TestRange_ForIn_IndexInBounds_NoDiag(t *testing.T) {
	noDiag(t, `
		let f = (xs: [3]u8) -> u8 => {
			var s: u8 = 0
			for i in 0..<3 {
				s += xs[i]
			}
			s
		}
		let main = () -> u8 => 0
	`)
}

// An inclusive range `0..=2` bounds the counter to [0,2] — in bounds for size 3.
func TestRange_ForIn_InclusiveRange_NoDiag(t *testing.T) {
	noDiag(t, `
		let f = (xs: [3]u8) -> u8 => {
			var s: u8 = 0
			for i in 0..=2 {
				s += xs[i]
			}
			s
		}
		let main = () -> u8 => 0
	`)
}

// A variable-length range isn't provably non-empty (it might not run), so the loop
// variable is left havoc'd — no false positive.
func TestRange_ForIn_VariableLength_NoDiag(t *testing.T) {
	noDiag(t, `
		let f = (xs: [3]u8, n: u8) -> u8 => {
			var s: u8 = 0
			for i in 0..<n {
				s += xs[i]
			}
			s
		}
		let main = () -> u8 => 0
	`)
}

// The elision counterpart: a for-in range counter proven in bounds elides its check.
func TestRange_Safety_ForInRangeIndexInBounds(t *testing.T) {
	program, safety := analyzeForSafety(t, `
		let f = (xs: [3]u8) -> u8 => {
			var s: u8 = 0
			for i in 0..<3 {
				s += xs[i]
			}
			s
		}
		let main = () -> u8 => 0
	`)
	idx := firstExpr(t, program, isIndexExpr)
	if !safety.IndexInBounds(idx) {
		t.Error("a for-in range counter i ∈ [0,2] indexing a size-3 array should be marked in-bounds")
	}
}
