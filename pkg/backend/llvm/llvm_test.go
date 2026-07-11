package llvm

import (
	"strings"
	"testing"

	"github.com/Lyra-Language/lyra/pkg/driver"
)

// emitSource analyzes src, resolves its entry point, and returns the emitted IR.
func emitSource(t *testing.T, src string) (string, error) {
	t.Helper()
	res := driver.Analyze([]byte(src))
	if res.HasErrors() {
		t.Fatalf("unexpected analysis errors: %v", res.Diagnostics)
	}
	ep, diags := driver.ResolveEntryPoint(res)
	if ep == nil {
		t.Fatalf("no entry point: %v", diags)
	}
	ir, err := New().Emit(res, ep)
	return string(ir), err
}

// TestEmit_IntegerLiteralBody: an i64 entry whose body is an integer literal
// returns that literal — the source value reaches the exit code.
func TestEmit_IntegerLiteralBody(t *testing.T) {
	got, err := emitSource(t, "let main = () -> i64 => 42\n")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"define i64 @main()", "ret i64 42"} {
		if !strings.Contains(got, want) {
			t.Errorf("emitted IR missing %q:\n%s", want, got)
		}
	}
}

// TestEmit_VoidEntry: a void entry exits 0.
func TestEmit_VoidEntry(t *testing.T) {
	got, err := emitSource(t, "let main = () -> void => {}\n")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "ret i64 0") {
		t.Errorf("void entry should `ret i64 0`:\n%s", got)
	}
}

// TestEmit_UnsupportedBody: a body form lowering doesn't handle yet fails the
// build loudly rather than emitting wrong code.
func TestEmit_UnsupportedBody(t *testing.T) {
	_, err := emitSource(t, "let main = () -> i64 => 1 + 2\n")
	if err == nil {
		t.Fatal("expected an error for a not-yet-lowerable body")
	}
	if !strings.Contains(err.Error(), "not implemented") {
		t.Errorf("expected 'not implemented' error, got: %v", err)
	}
}

func TestEmit_NilArgs(t *testing.T) {
	if _, err := New().Emit(nil, nil); err == nil {
		t.Fatal("expected an error for nil program/entry point")
	}
}

// TestBackend_SatisfiesContract is a compile-time-ish check that New() is usable
// as the backend.Backend the compiler expects.
func TestBackend_Name(t *testing.T) {
	if got := New().Name(); got != "llvm" {
		t.Fatalf("Name() = %q, want %q", got, "llvm")
	}
}
