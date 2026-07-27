package driver

import (
	"strings"
	"testing"

	diag "github.com/Lyra-Language/lyra/pkg/diagnostic"
)

// TestAnalyze_CleanProgram: a well-typed program yields no errors and every
// table a later stage (codegen) needs is populated.
func TestAnalyze_CleanProgram(t *testing.T) {
	res := Analyze([]byte("let x: i64 = 42\nlet y = x + 1\n"))
	if res.HasErrors() {
		t.Fatalf("expected no errors, got: %v", res.Diagnostics)
	}
	if res.Program == nil || res.SymbolTable == nil || res.ScopeTable == nil ||
		res.TypeTable == nil || res.MethodTable == nil {
		t.Fatalf("expected all tables populated, got program=%v sym=%v scope=%v type=%v method=%v",
			res.Program != nil, res.SymbolTable != nil, res.ScopeTable != nil,
			res.TypeTable != nil, res.MethodTable != nil)
	}
}

// TestAnalyze_TypeError surfaces a typechecker diagnostic through the unified
// diagnostic list.
func TestAnalyze_TypeError(t *testing.T) {
	res := Analyze([]byte(`let x: i64 = "hi"` + "\n"))
	if !res.HasErrors() {
		t.Fatal("expected a type error")
	}
	if !hasCode(res, diag.CodeTypeError) {
		t.Fatalf("expected %s, got: %v", diag.CodeTypeError, res.Diagnostics)
	}
	// The program is still returned (partial), so later passes/tooling can run.
	if res.Program == nil {
		t.Fatal("expected a (partial) program even with type errors")
	}
}

// TestAnalyze_SyntaxError surfaces a CST-level parse error as an error diagnostic.
func TestAnalyze_SyntaxError(t *testing.T) {
	res := Analyze([]byte("let x: i64 =\n"))
	if !res.HasErrors() {
		t.Fatalf("expected a syntax error, got: %v", res.Diagnostics)
	}
	if !hasMessageContaining(res, "syntax error", "missing") {
		t.Fatalf("expected a parse diagnostic, got: %v", res.Diagnostics)
	}
}

func hasCode(res *Result, code string) bool {
	for _, d := range res.Diagnostics {
		if d.Code == code {
			return true
		}
	}
	return false
}

func hasMessageContaining(res *Result, substrs ...string) bool {
	for _, d := range res.Diagnostics {
		for _, s := range substrs {
			if strings.Contains(d.Message, s) {
				return true
			}
		}
	}
	return false
}

// TestAnalyze_LargeU64Literal_TypeChecks: a literal that overflows int64 but
// fits u64 (18446744073709551615) is a valid u64 value — it type-checks in a
// u64 context. This is also the original crash reproducer (a nil literal node
// once panicked propagateLiteralType); it must analyze without panic or error.
func TestAnalyze_LargeU64Literal_TypeChecks(t *testing.T) {
	src := "let big = (n: u64) -> u64 => n\n" +
		"let main = () -> u8 => {\n  let x = big(18446744073709551615)\n  0\n}\n"
	res := Analyze([]byte(src)) // must not panic
	if res.HasErrors() {
		t.Fatalf("expected a large u64 literal to type-check, got: %v", res.Diagnostics)
	}
}

// TestAnalyze_LargeU64Literal_RejectsSignedTarget: the same literal is NOT
// silently accepted as a signed value — assigning it to i64 is a clean error,
// never a wrong (negative) value.
func TestAnalyze_LargeU64Literal_RejectsSignedTarget(t *testing.T) {
	res := Analyze([]byte("let main = () -> u8 => {\n  let x: i64 = 18446744073709551615\n  0\n}\n"))
	if !res.HasErrors() {
		t.Fatal("expected a type error assigning a large u64 literal to i64")
	}
	if !hasMessageContaining(res, "cannot assign u64 to i64") {
		t.Fatalf("expected a u64-to-i64 assignability error, got: %v", res.Diagnostics)
	}
}

// TestAnalyze_HugeIntLiteral_NoPanic: a literal beyond even u64 range is a clean
// error, not a crash.
func TestAnalyze_HugeIntLiteral_NoPanic(t *testing.T) {
	res := Analyze([]byte("let x = 99999999999999999999999999\nlet main = () -> u8 => 0\n"))
	if !res.HasErrors() {
		t.Fatal("expected an out-of-range diagnostic")
	}
	if !hasMessageContaining(res, "too large to represent") {
		t.Fatalf("expected a too-large message, got: %v", res.Diagnostics)
	}
}

// TestAnalyze_OutOfRangeFloatLiteral_NoPanic: the float parse-failure path is
// the same typed-nil hazard as the integer one.
func TestAnalyze_OutOfRangeFloatLiteral_NoPanic(t *testing.T) {
	res := Analyze([]byte("let x = 1.0e999999\nlet main = () -> u8 => 0\n"))
	if !res.HasErrors() {
		t.Fatal("expected an out-of-range diagnostic")
	}
	if !hasMessageContaining(res, "out of range for f64") {
		t.Fatalf("expected a float-range message, got: %v", res.Diagnostics)
	}
}

// TestAnalyze_RangeAnalysis_DefiniteOverflow: the value-range pass is wired into
// the pipeline, so a provable overflow (via a branch refinement — beyond what the
// literal range check catches) surfaces as an error through Analyze.
func TestAnalyze_RangeAnalysis_DefiniteOverflow(t *testing.T) {
	src := "let f = (x: i8) -> i8 => if x > 100 { x + 100 } else { 0 }\n" +
		"let main = () -> u8 => 0\n"
	res := Analyze([]byte(src))
	if !res.HasErrors() {
		t.Fatal("expected a definite-overflow error")
	}
	if !hasMessageContaining(res, "always overflows i8") {
		t.Fatalf("expected an overflow message, got: %v", res.Diagnostics)
	}
}

// TestAnalyze_RangeAnalysis_DefiniteDivideByZero: the range pass surfaces a
// definite divide-by-zero (lyra-E021) on a *variable* divisor proven zero by flow
// — beyond the typechecker's literal/folded-constant check.
func TestAnalyze_RangeAnalysis_DefiniteDivideByZero(t *testing.T) {
	src := "let f = (a: u8, b: u8) -> u8 => if b == 0 { a / b } else { a }\n" +
		"let main = () -> u8 => 0\n"
	res := Analyze([]byte(src))
	if !hasCode(res, diag.CodeDivideByZero) {
		t.Fatalf("expected a definite divide-by-zero (%s), got: %v", diag.CodeDivideByZero, res.Diagnostics)
	}
}

// A literal-zero divisor stays the typechecker's constant-fold check; the range
// pass deliberately does not also emit E021, so the bug is reported exactly once.
func TestAnalyze_LiteralDivideByZero_NoDuplicate(t *testing.T) {
	res := Analyze([]byte("let main = () -> u8 => 5 / 0\n"))
	if hasCode(res, diag.CodeDivideByZero) {
		t.Fatalf("a literal 5/0 should be the typechecker's error only, not a duplicate E021: %v", res.Diagnostics)
	}
	if !hasMessageContaining(res, "division by zero") {
		t.Fatalf("expected the typechecker's division-by-zero error, got: %v", res.Diagnostics)
	}
}

// TestAnalyze_RangeAnalysis_DefiniteOutOfBounds: the range pass surfaces a
// definite out-of-bounds (lyra-E022) on an index proven past the end by flow —
// beyond the typechecker's constant-index range check.
func TestAnalyze_RangeAnalysis_DefiniteOutOfBounds(t *testing.T) {
	src := "let at = (xs: [3]u8, i: u8) -> u8 => if i >= 3 { xs[i] } else { 0 }\n" +
		"let main = () -> u8 => 0\n"
	res := Analyze([]byte(src))
	if !hasCode(res, diag.CodeIndexOutOfBounds) {
		t.Fatalf("expected a definite out-of-bounds (%s), got: %v", diag.CodeIndexOutOfBounds, res.Diagnostics)
	}
}

// A constant index stays the typechecker's own range check; the range pass (whose
// OOB diagnostic requires a non-singleton range) does not also emit E022, so the
// bug is reported exactly once.
func TestAnalyze_ConstantOutOfBounds_NoDuplicate(t *testing.T) {
	src := "let main = () -> u8 => {\n  let xs: [3]u8 = [1, 2, 3]\n  xs[5]\n}\n"
	res := Analyze([]byte(src))
	if hasCode(res, diag.CodeIndexOutOfBounds) {
		t.Fatalf("a constant xs[5] should be the typechecker's error only, not a duplicate E022: %v", res.Diagnostics)
	}
	if !hasMessageContaining(res, "out of range for array") {
		t.Fatalf("expected the typechecker's out-of-range error, got: %v", res.Diagnostics)
	}
}

// TestAnalyze_RangeConstraint_Violation: a constant outside a range-constrained
// newtype's declared range surfaces as lyra-E023 through the full pipeline.
func TestAnalyze_RangeConstraint_Violation(t *testing.T) {
	src := "newtype Percent = u8 where range(0..=100)\n" +
		"let p: Percent = 150\n" +
		"let main = () -> u8 => 0\n"
	res := Analyze([]byte(src))
	if !hasCode(res, diag.CodeRangeConstraintViolation) {
		t.Fatalf("expected a range-constraint violation (%s), got: %v", diag.CodeRangeConstraintViolation, res.Diagnostics)
	}
}

// TestAnalyze_WeakType_TypeChecks: `weak T` used to hard-error in the collector
// ("unknown type node kind: weak_type"), making it unusable. A recursive type
// whose back-edge is a `weak` field now analyzes cleanly through the whole
// pipeline — weak breaks the size cycle like `shared` (lyra-E014).
func TestAnalyze_WeakType_TypeChecks(t *testing.T) {
	src := "struct Node {\n  value: i64,\n  parent: weak Node,\n}\n" +
		"data List = Nil | Cons(i64, weak List)\n" +
		"let main = () -> u8 => 0\n"
	res := Analyze([]byte(src))
	if res.HasErrors() {
		t.Fatalf("expected a weak-broken recursive type to analyze cleanly, got: %v", res.Diagnostics)
	}
}
