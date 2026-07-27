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
