package llvm

import (
	"os/exec"
	"strings"
	"testing"
)

// A dynamic array `[]T` is a heap-boxed, ref-counted value — a `ptr` to
// `{ i64 rc, i64 len, [0 x T] }` — reusing the shared-value ownership machinery.
// This first slice covers construction from a literal (incl. empty), indexing
// (bounds-checked against the runtime len, negative-from-end), and by-value flow
// through let/params/returns. See dynarray.go.

func TestExec_DynArray(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want int
	}{
		{
			"construct and index",
			`let main = () -> u8 => {
  let xs: []u8 = [10, 20, 30]
  xs[1]
}`,
			20,
		},
		{
			"index the last via negative",
			`let main = () -> u8 => {
  let xs: []u8 = [1, 2, 3]
  xs[-1]
}`,
			3,
		},
		{
			// A `[]T` passed to a callee (a box pointer) and indexed there.
			"pass to callee and index",
			`let at = (xs: []i64, i: i64) -> i64 => xs[i]
let main = () -> u8 => {
  let a: []i64 = [4, 5, 6]
  u8(at(a, 2))
}`,
			6,
		},
		{
			// A function builds and returns a dynamic array.
			"return a dynamic array",
			`let make = () -> []i64 => [7, 8, 9]
let main = () -> u8 => {
  let a: []i64 = make()
  u8(a[0] + a[2])
}`,
			16,
		},
		{
			// An empty dynamic array still allocates a (len-0) box and round-trips.
			"empty array is valid",
			`let main = () -> u8 => {
  let xs: []i64 = []
  42
}`,
			42,
		},
		{
			// A copy at its source's last use *moves* the box (no retain).
			"copy (move) then index",
			`let main = () -> u8 => {
  let a: []u8 = [10, 20, 30]
  let b: []u8 = a
  b[1]
}`,
			20,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := buildAndRun(t, c.src); got != c.want {
				t.Errorf("expected exit %d, got %d", c.want, got)
			}
		})
	}
}

// A runtime index out of the runtime length traps (exit 101).
func TestExec_DynArray_BoundsTrap(t *testing.T) {
	src := `let at = (xs: []u8, i: i64) -> u8 => xs[i]
let main = () -> u8 => {
  let a: []u8 = [1, 2, 3]
  at(a, 5)
}`
	if got := buildAndRun(t, src); got != 101 {
		t.Errorf("expected out-of-bounds trap (exit 101), got %d", got)
	}
}

// The box is heap-allocated and freed exactly once: the IR shows lyra_rc_alloc for
// the array box and allocations balance releases (no leak / double free).
func TestEmit_DynArray_IR(t *testing.T) {
	src := `let main = () -> u8 => {
  let xs: []u8 = [10, 20, 30]
  xs[1]
}
`
	got, err := emitSource(t, src)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"call i8* @lyra_rc_alloc",
		"{ i64, i64, [0 x i8] }",
		"call void @lyra_rc_release",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("emitted IR missing %q:\n%s", want, got)
		}
	}
	if allocs, releases := strings.Count(got, "call i8* @lyra_rc_alloc"),
		strings.Count(got, "call void @lyra_rc_release"); allocs != releases {
		t.Errorf("conservation: %d allocations vs %d releases; must balance", allocs, releases)
	}
}

// A managed element type is deferred with a loud error (the box's per-element drop
// glue isn't implemented yet).
func TestEmit_DynArray_ManagedElementDeferred(t *testing.T) {
	src := `let main = () -> u8 => {
  let xs: []string = ["a", "b"]
  0
}
`
	if _, err := emitSource(t, src); err == nil {
		t.Fatal("expected a loud error for a dynamic array of a managed element type, got none")
	}
}

// Construction + indexing + scope-exit free are memory-safe under AddressSanitizer,
// with a static allocations==releases conservation check (macOS ASan can't see
// leaks).
func TestExec_DynArray_ASan(t *testing.T) {
	clang, err := exec.LookPath("clang")
	if err != nil {
		t.Skip("clang not found on PATH; skipping ASan test")
	}
	if !asanAvailable(t, clang) {
		t.Skip("ASan runtime not available; skipping")
	}
	src := `let sum = (xs: []i64) -> i64 => xs[0] + xs[1] + xs[2]
let main = () -> u8 => {
  let a: []i64 = [10, 20, 12]
  u8(sum(a))
}
`
	if code := buildAndRunASan(t, clang, src); code != 42 {
		t.Errorf("ASan run: expected exit 42, got %d", code)
	}
	ir, err := emitSource(t, src)
	if err != nil {
		t.Fatal(err)
	}
	if allocs, releases := strings.Count(ir, "call i8* @lyra_rc_alloc"),
		strings.Count(ir, "call void @lyra_rc_release"); allocs != releases {
		t.Errorf("conservation: %d allocations vs %d releases (leak or double free)\n%s", allocs, releases, ir)
	}
}

// Aliasing a `[]T` binding used after the copy retains the box (rc 1→2), so both
// bindings can be released (2→1→0, freed once). ASan proves no double free; the
// static conservation invariant here is allocations + retains == releases.
func TestExec_DynArray_AliasRetainASan(t *testing.T) {
	clang, err := exec.LookPath("clang")
	if err != nil {
		t.Skip("clang not found on PATH; skipping ASan test")
	}
	if !asanAvailable(t, clang) {
		t.Skip("ASan runtime not available; skipping")
	}
	src := `let main = () -> u8 => {
  let a: []u8 = [10, 20, 30]
  let b: []u8 = a
  u8(a[0] + b[2])
}
`
	if code := buildAndRunASan(t, clang, src); code != 40 {
		t.Errorf("ASan run: expected exit 40, got %d", code)
	}
	ir, err := emitSource(t, src)
	if err != nil {
		t.Fatal(err)
	}
	allocs := strings.Count(ir, "call i8* @lyra_rc_alloc")
	retains := strings.Count(ir, "call void @lyra_rc_retain")
	releases := strings.Count(ir, "call void @lyra_rc_release")
	if retains == 0 {
		t.Errorf("expected a retain for the aliased copy, got none:\n%s", ir)
	}
	if allocs+retains != releases {
		t.Errorf("conservation: %d allocs + %d retains != %d releases (leak or double free)\n%s", allocs, retains, releases, ir)
	}
}
