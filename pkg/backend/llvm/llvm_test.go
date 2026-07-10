package llvm

import (
	"strings"
	"testing"

	"github.com/Lyra-Language/lyra/pkg/driver"
)

// TestEmit_EntrySkeleton pins the skeleton output shape: a valid module that
// defines @main and returns an i64. (Grow this into behavioral tests as real
// lowering lands.)
func TestEmit_EntrySkeleton(t *testing.T) {
	res := driver.Analyze([]byte("let main = () -> i64 => 42\n"))
	if res.HasErrors() {
		t.Fatalf("unexpected analysis errors: %v", res.Diagnostics)
	}
	ep, diags := driver.ResolveEntryPoint(res)
	if ep == nil {
		t.Fatalf("no entry point: %v", diags)
	}

	ir, err := New().Emit(res, ep)
	if err != nil {
		t.Fatal(err)
	}
	got := string(ir)
	for _, want := range []string{"define i64 @main()", "ret i64"} {
		if !strings.Contains(got, want) {
			t.Errorf("emitted IR missing %q:\n%s", want, got)
		}
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
