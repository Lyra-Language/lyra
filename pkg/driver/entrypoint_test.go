package driver

import (
	"strings"
	"testing"

	diag "github.com/Lyra-Language/lyra/pkg/diagnostic"
)

func TestResolveEntryPoint_ExitCode(t *testing.T) {
	res := Analyze([]byte("let main = () -> i64 => 0\n"))
	ep, diags := ResolveEntryPoint(res)
	if len(diags) != 0 {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
	if ep == nil || ep.Name != "main" || ep.Returns != EntryReturnExitCode {
		t.Fatalf("expected main/ExitCode entry point, got %+v", ep)
	}
	if ep.Lambda == nil || ep.Decl == nil {
		t.Fatal("expected Lambda and Decl to be populated")
	}
}

func TestResolveEntryPoint_Void(t *testing.T) {
	for _, src := range []string{
		"let main = () -> void => {}\n",
		"let main = () => {}\n", // no annotation is treated as void
	} {
		res := Analyze([]byte(src))
		ep, diags := ResolveEntryPoint(res)
		if len(diags) != 0 {
			t.Fatalf("src %q: unexpected diagnostics: %v", src, diags)
		}
		if ep == nil || ep.Returns != EntryReturnVoid {
			t.Fatalf("src %q: expected Void entry point, got %+v", src, ep)
		}
	}
}

func TestResolveEntryPoint_Missing(t *testing.T) {
	res := Analyze([]byte("let x = 5\n"))
	ep, diags := ResolveEntryPoint(res)
	if ep != nil {
		t.Fatal("expected no entry point")
	}
	if !diagContains(diags, "no entry point") {
		t.Fatalf("expected 'no entry point', got %v", diags)
	}
}

func TestResolveEntryPoint_NotAFunction(t *testing.T) {
	res := Analyze([]byte("let main = 5\n"))
	ep, diags := ResolveEntryPoint(res)
	if ep != nil {
		t.Fatal("expected no entry point")
	}
	if !diagContains(diags, "must be a function") {
		t.Fatalf("expected 'must be a function', got %v", diags)
	}
}

func TestResolveEntryPoint_HasParameters(t *testing.T) {
	res := Analyze([]byte("let main = (x: i64) -> i64 => x\n"))
	ep, diags := ResolveEntryPoint(res)
	if ep != nil {
		t.Fatal("expected no entry point")
	}
	if !diagContains(diags, "must take no parameters") {
		t.Fatalf("expected 'must take no parameters', got %v", diags)
	}
}

func TestResolveEntryPoint_WrongReturnType(t *testing.T) {
	res := Analyze([]byte(`let main = () -> string => "hi"` + "\n"))
	ep, diags := ResolveEntryPoint(res)
	if ep != nil {
		t.Fatal("expected no entry point")
	}
	if !diagContains(diags, "must return i64 or void") {
		t.Fatalf("expected 'must return i64 or void', got %v", diags)
	}
}

func diagContains(diags []diag.Diagnostic, substr string) bool {
	for _, d := range diags {
		if strings.Contains(d.Message, substr) {
			return true
		}
	}
	return false
}
