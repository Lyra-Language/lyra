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

// TestAnalyze_OutOfRangeIntLiteral_NoPanic: a numeric literal that overflows
// int64 must produce a clean diagnostic and never crash a later pass.
// Regression — the collector returned a nil node for such a literal, which
// entered the AST as a typed-nil expression and panicked propagateLiteralType in
// the typechecker (nil dereference).
func TestAnalyze_OutOfRangeIntLiteral_NoPanic(t *testing.T) {
	// A valid u64 value that overflows int64 (the original crash reproducer,
	// exercised through a call argument so propagateLiteralType runs on it).
	src := "let big = (n: u64) -> u64 => n\n" +
		"let main = () -> u8 => {\n  let x = big(18446744073709551615)\n  0\n}\n"
	res := Analyze([]byte(src)) // must not panic
	if !res.HasErrors() {
		t.Fatal("expected an out-of-range diagnostic")
	}
	if !hasMessageContaining(res, "exceeds the range of i64") {
		t.Fatalf("expected an i64-range message, got: %v", res.Diagnostics)
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
