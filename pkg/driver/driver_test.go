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
