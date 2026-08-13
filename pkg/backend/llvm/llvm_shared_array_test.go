package llvm

import (
	"os/exec"
	"strings"
	"testing"
)

// A `shared [N]T` is a fixed-size array living in a ref-counted heap box (a `ptr`
// to `{ i64 strong, i64 weak, [N x T] }`), reusing the same shared-box construction, ownership,
// and drop machinery as a `shared` struct/data value. Construction boxes the inline
// array; indexing geps through the box's payload; the box is freed at the binding's
// scope exit (running per-element drop glue when the elements are themselves
// managed). See ALLOCATION.md.

func TestExec_SharedArray(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		src  string
		want int
	}{
		{
			// Construct + constant index through the box.
			"construct and constant-index",
			`let main = () -> u8 => {
  let xs: shared [3]u8 = [10, 20, 30]
  xs[1]
}`,
			20,
		},
		{
			// A `shared` array passed to a callee (a bare/borrowed param) and indexed there.
			"borrowed param + index in callee",
			`let third = (xs: shared [3]i64) -> i64 => xs[2]
let main = () -> u8 => {
  let a: shared [3]i64 = [4, 5, 6]
  u8(third(a))
}`,
			6,
		},
		{
			// A function returns a freshly-boxed `shared` array (construction in return position).
			"return a shared array",
			`let make = () -> shared [3]i64 => [7, 8, 9]
let main = () -> u8 => {
  let a: shared [3]i64 = make()
  u8(a[0] + a[1])
}`,
			15,
		},
		{
			// Runtime index through the box (index is a param, so no compile-time fold).
			"runtime index",
			`let at = (xs: shared [3]u8, i: i64) -> u8 => xs[i]
let main = () -> u8 => {
  let a: shared [3]u8 = [5, 15, 25]
  at(a, 2)
}`,
			25,
		},
		{
			// The end-relative read goes through the box the same way — from_end on a
			// `shared` fixed array reaches its payload rather than an inline alloca.
			"from_end through the box",
			`let main = () -> u8 => {
  let xs: shared [3]u8 = [1, 2, 3]
  xs.from_end(1)
}`,
			3,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if got := buildAndRun(t, c.src); got != c.want {
				t.Errorf("expected exit %d, got %d", c.want, got)
			}
		})
	}
}

// A runtime index out of range traps (exit 101), through the box just like an
// inline array.
func TestExec_SharedArray_BoundsTrap(t *testing.T) {
	t.Parallel()
	src := `let at = (xs: shared [3]u8, i: i64) -> u8 => xs[i]
let main = () -> u8 => {
  let a: shared [3]u8 = [1, 2, 3]
  at(a, 5)
}`
	if got := buildAndRun(t, src); got != 101 {
		t.Errorf("expected out-of-bounds trap (exit 101), got %d", got)
	}
}

// The box is heap-allocated and freed: the IR shows one lyra_rc_alloc for the array
// box, the box type is `{ i64, [N x T] }`, and allocations balance releases (no leak
// or double free).
func TestEmit_SharedArray_IR(t *testing.T) {
	t.Parallel()
	src := `let main = () -> u8 => {
  let xs: shared [3]u8 = [10, 20, 30]
  xs[1]
}
`
	got, err := emitSource(t, src)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"call i8* @lyra_rc_alloc",
		"{ i64, i64, [3 x i8] }",
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

// A `shared` array of *managed* elements (heap strings) frees each element when the
// box dies: the per-type drop glue walks the array. Verified under AddressSanitizer,
// plus a static allocations==releases conservation check (macOS ASan can't see
// leaks). Two heap-string elements + the array box = 3 allocations; the box's
// release and the drop glue's two element releases = 3 releases.
func TestExec_SharedArray_ManagedElementsASan(t *testing.T) {
	t.Parallel()
	clang, err := exec.LookPath("clang")
	if err != nil {
		t.Skip("clang not found on PATH; skipping ASan test")
	}
	if !asanAvailable(t, clang) {
		t.Skip("ASan runtime not available; skipping")
	}
	src := `let main = () -> u8 => {
  let s: string = "a" ++ "b"
  let t: string = "c" ++ "d"
  let xs: shared [2]string = [s, t]
  0
}
`
	if code := buildAndRunASan(t, clang, src); code != 0 {
		t.Errorf("ASan run: expected exit 0, got %d", code)
	}
	ir, err := emitSource(t, src)
	if err != nil {
		t.Fatal(err)
	}
	allocs := strings.Count(ir, "call i8* @lyra_rc_alloc")
	releases := strings.Count(ir, "call void @lyra_rc_release")
	if allocs != releases {
		t.Errorf("conservation: %d allocations vs %d releases (leak or double free)\n%s", allocs, releases, ir)
	}
	if !strings.Contains(ir, "@lyra_drop") {
		t.Errorf("expected per-type drop glue for the managed-element array:\n%s", ir)
	}
}

// An array whose *elements* carry the modifier — `[N]shared T`, `[]shared T`,
// `[N]weak T` — as opposed to `shared [N]T`, which boxes the array itself. The two
// read alike and mean different things: one array of N boxes, versus one box holding
// N inline values.
//
// This syntax did not exist until 08/03 (`array_type`'s element was
// `_non_allocated_type`), which is why the shape a tree actually wants —
// `kids: []shared Node` — had no spelling. The backend needed no change for it: an
// element flavor flows through the same layout and ownership paths as any other, and
// a `shared` element is pointer-sized, so these tests are here to hold that.
func TestExec_ArrayOfManagedElements(t *testing.T) {
	t.Parallel()
	clang := lookClang(t)
	cases := []struct {
		name string
		src  string
		want int
	}{
		{
			// Fixed-size array of boxes; each element is an independent allocation.
			"fixed-size array of shared elements",
			`struct Node { n: i64 }
			 let main = () -> u8 => {
			   let a: shared Node = Node { n: 3 }
			   let b: shared Node = Node { n: 4 }
			   let xs: [2]shared Node = [a, b]
			   u8(xs[0].n + xs[1].n)
			 }`,
			7,
		},
		{
			// The dynamic form, which is the one a tree's children want.
			"dynamic array of shared elements",
			`struct Node { n: i64 }
			 let main = () -> u8 => {
			   let a: shared Node = Node { n: 5 }
			   let xs: []shared Node = [a]
			   u8(xs[0].n)
			 }`,
			5,
		},
		{
			// for-in over managed elements: each iteration borrows an element rather
			// than taking a reference the loop then has to give back.
			"for-in over an array of shared elements",
			`struct Node { n: i64 }
			 let main = () -> u8 => {
			   let a: shared Node = Node { n: 1 }
			   let b: shared Node = Node { n: 2 }
			   let c: shared Node = Node { n: 4 }
			   let kids: []shared Node = [a, b, c]
			   var sum = 0
			   for k in kids { sum = sum + k.n }
			   u8(sum)
			 }`,
			7,
		},
		{
			// An array of weak references — the observer-list shape. Each element is
			// upgraded independently, and the referents are still alive here.
			"array of weak elements, upgraded",
			`struct Node { n: i64 }
			 let main = () -> u8 => {
			   let a: shared Node = Node { n: 6 }
			   let xs: [1]weak Node = [a.weak()]
			   var out = 0
			   if let p = xs[0] { out = p.n }
			   u8(out)
			 }`,
			6,
		},
		{
			// Both element modifiers in one type, which is the whole shape the two
			// 08/03 changes were for: children owned `shared`, parent held `weak` so
			// the cycle does not leak.
			"a tree: shared children and a weak parent edge",
			`data Maybe<t> = None | Some(t)
			 struct Node { n: i64, parent: Maybe<weak Node>, kids: []shared Node }
			 let main = () -> u8 => {
			   let a: shared Node = Node { n: 1, parent: None, kids: [] }
			   let b: shared Node = Node { n: 2, parent: None, kids: [] }
			   let root: shared Node = Node { n: 4, parent: None, kids: [a, b] }
			   var sum = 0
			   for k in root.kids { sum = sum + k.n }
			   u8(sum + root.n)
			 }`,
			7,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if got := buildAndRun(t, c.src); got != c.want {
				t.Errorf("%s: exited %d; want %d", c.name, got, c.want)
			}
			if got := buildAndRunASan(t, clang, c.src); got != c.want {
				t.Errorf("%s: ASan run exited %d; want %d", c.name, got, c.want)
			}
		})
	}
}
