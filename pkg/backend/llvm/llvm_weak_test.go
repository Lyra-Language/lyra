package llvm

import (
	"strings"
	"testing"
)

// A `weak` reference is a non-owning pointer, so a `weak` field lowers to an
// opaque `i8*` — pointer-sized, which makes a recursive type finite (the whole
// point: `weak` breaks a `shared`-cycle leak, and along the way the size cycle).
// The non-owning runtime semantics are deferred and unconstructible today, so
// these cover only the layout: the declaration lowers and compiles.

func TestEmit_WeakField_LowersToPointer(t *testing.T) {
	src := `struct Node { value: i64, parent: weak Node }
let main = () -> u8 => 0`
	ir, err := emitSource(t, src)
	if err != nil {
		t.Fatalf("emit: %v", err)
	}
	// The recursive struct is finite: the weak field is a pointer, not Node by value.
	if !strings.Contains(ir, "%Node = type { i64, i8* }") {
		t.Errorf("expected %%Node with a pointer weak field, got:\n%s", ir)
	}
}

// A program declaring a weak-broken recursive type compiles and runs (the weak
// field never crashes layout the way it used to crash parseType).
func TestExec_WeakRecursiveTypeBuilds(t *testing.T) {
	src := `struct Node { value: i64, parent: weak Node }
data List = Nil | Cons(i64, weak List)
let main = () -> u8 => 7`
	if got := buildAndRun(t, src); got != 7 {
		t.Errorf("exited %d, want 7", got)
	}
}
