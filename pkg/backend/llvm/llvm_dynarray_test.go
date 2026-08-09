package llvm

import (
	"os/exec"
	"strings"
	"testing"
)

// A dynamic array `[]T` is a heap-boxed, ref-counted value — a `ptr` to
// `{ i64 strong, i64 weak, i64 len, [0 x T] }` — reusing the shared-value ownership machinery.
// This first slice covers construction from a literal (incl. empty), indexing
// (bounds-checked against the runtime len, negative-from-end), and by-value flow
// through let/params/returns. See dynarray.go.

func TestExec_DynArray(t *testing.T) {
	t.Parallel()
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
			t.Parallel()
			if got := buildAndRun(t, c.src); got != c.want {
				t.Errorf("expected exit %d, got %d", c.want, got)
			}
		})
	}
}

// A runtime index out of the runtime length traps (exit 101).
func TestExec_DynArray_BoundsTrap(t *testing.T) {
	t.Parallel()
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
//
// The layout pinned here is `{ strong, weak, len, cap, T* }` — the elements live in a
// separate malloc'd buffer rather than an inline `[0 x T]` tail. That indirection is
// what makes `[]T` growable: a `[]T` value is the box pointer, so elements that moved
// would move the box and dangle every alias, and aliasing is observable. The `malloc`
// and its matching `free` (in the box's drop glue) are part of the shape, which is why
// the conservation check below counts rc_alloc/rc_release rather than raw allocations.
func TestEmit_DynArray_IR(t *testing.T) {
	t.Parallel()
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
		"{ i64, i64, i64, i64, i8* }",
		"call i8* @malloc",
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

// A dynamic array of a *managed* element type works: elements are indexable, and
// the box's drop glue releases each one over the runtime length.
func TestExec_DynArray_ManagedElements(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		src  string
		want int
	}{
		{
			// []string: index a string element and compare it.
			"index a string element",
			`let main = () -> u8 => {
  let xs: []string = ["ab", "cd"]
  if xs[1] == "cd" { 7 } else { 0 }
}`,
			7,
		},
		{
			// [][]i64: a dynamic array of dynamic arrays, indexed twice.
			"nested dynamic arrays",
			`let main = () -> u8 => {
  let xs: [][]i64 = [[1, 2], [3, 4]]
  u8(xs[0][1] + xs[1][0])
}`,
			5,
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

// A `[]string` of *heap* strings frees each element when the box dies: the drop
// glue loops over len releasing each. Verified under AddressSanitizer (no double
// free / use-after-free). Unlike a fixed-size array's *unrolled* drop, the dynamic
// drop is a loop, so a static release-site count can't stand in for conservation
// (one call site runs len times) — instead we assert the drop glue is generated and
// its loop body releases an element, and that the box release passes it as drop_fn.
func TestExec_DynArray_ManagedElementsASan(t *testing.T) {
	t.Parallel()
	clang, err := exec.LookPath("clang")
	if err != nil {
		t.Skip("clang not found on PATH; skipping ASan test")
	}
	if !asanAvailable(t, clang) {
		t.Skip("ASan runtime not available; skipping")
	}
	for _, src := range []string{
		// []string with heap-string elements (transferred into the box, freed by the loop).
		`let main = () -> u8 => {
  let a: string = "x" ++ "y"
  let b: string = "p" ++ "q"
  let xs: []string = [a, b]
  0
}
`,
		// [][]i64: the outer drop glue releases each inner box.
		`let main = () -> u8 => {
  let xs: [][]i64 = [[1, 2], [3, 4]]
  u8(xs[0][0])
}
`,
	} {
		if code := buildAndRunASan(t, clang, src); code != 0 && code != 1 {
			t.Errorf("ASan run: unexpected exit %d for %q", code, src)
		}
	}
}

// The dynamic-array drop glue is generated for a managed element type and structured
// as a loop that releases each element; the box's own release passes it as drop_fn
// (a non-null bitcast), so freeing the box frees the elements. This is the leak-side
// check the looped drop makes a static release *count* unable to express.
func TestEmit_DynArray_ManagedElementDropGlue(t *testing.T) {
	t.Parallel()
	src := `let main = () -> u8 => {
  let a: string = "x" ++ "y"
  let xs: []string = [a]
  0
}
`
	ir, err := emitSource(t, src)
	if err != nil {
		t.Fatal(err)
	}
	// A generated drop function that loops and releases an element.
	if !strings.Contains(ir, "@lyra_drop") {
		t.Errorf("expected a generated element drop function:\n%s", ir)
	}
	if !strings.Contains(ir, "loopbody") || !strings.Contains(ir, "loopcond") {
		t.Errorf("expected the drop glue to loop over the length:\n%s", ir)
	}
	// The box release must pass the drop glue (a bitcast to i8*), not a null drop_fn,
	// so releasing the box runs the element drops.
	if !strings.Contains(ir, "bitcast (void (i8*)* @lyra_drop") {
		t.Errorf("expected the box release to pass the drop glue as drop_fn:\n%s", ir)
	}
}

// Construction + indexing + scope-exit free are memory-safe under AddressSanitizer,
// with a static allocations==releases conservation check (macOS ASan can't see
// leaks).
func TestExec_DynArray_ASan(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
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

// `xs.push(v)` — the growth operation. Growing from an empty array exercises every
// branch of the amortized doubling: cap 0 → 4 → 8 → 16, so the realloc path runs three
// times across ten pushes and the store-only path runs the other seven.
func TestExec_DynArrayPushGrows(t *testing.T) {
	t.Parallel()
	src := `let main = () -> u8 => {
  var xs: []i64 = []
  for var i = 0; i < 10; i+=1 { xs.push(i * i) }
  var sum = 0
  for x in xs { sum = sum + x }
  u8((xs.len() * 100 + sum) %% 256)
}
`
	// 10 elements, sum of squares 0..9 = 285; (1000 + 285) % 256 = 5.
	if got := buildAndRun(t, src); got != 5 {
		t.Errorf("push growth: exit %d, want 5 (len 10, sum 285)", got)
	}
}

// **Every alias sees the push**, which is the property the whole representation change
// exists to preserve. A `[]T` value is the box pointer, so growth that moved the box
// would leave `b` pointing at freed memory; the elements live behind an indirection
// precisely so it cannot. This is the same reference semantics `xs[i] = v` already had —
// `let b = a; a[0] = 9` reads through `b` — so a push that `b` could not see would have
// been the odd one out.
func TestExec_DynArrayPushIsVisibleThroughAnAlias(t *testing.T) {
	t.Parallel()
	src := `let main = () -> u8 => {
  var a: []i64 = [7]
  let b = a
  a.push(35)
  u8(b.len() * 10 + b[1])
}
`
	if got := buildAndRun(t, src); got != 55 {
		t.Errorf("alias after push: exit %d, want 55 (b.len 2, b[1] 35)", got)
	}
}

// A **managed** element pushed transfers its reference into the array, and the box's
// drop glue releases it over the runtime length.
//
// The heap string is the case that broke: the typechecker records a builtin method's
// signature on the MemberExpr, so the ownership pass read the argument's mode off a
// signature carrying no written `own` — which means *borrow* — and released the
// temporary after the call while the array kept the pointer. It printed garbage rather
// than leaking. A literal survived it (a literal interns as a pinned box, so its release
// is a no-op), which is exactly the kind of coincidence that makes a memory bug look like
// it works.
//
// The last row is the other half: pushing a *binding* must not consume it. An owning
// position takes a +1, it does not move the caller's value, so `t` is still readable.
func TestExec_DynArrayPushManagedElements(t *testing.T) {
	t.Parallel()
	const src = `
module main
let main = () -> void => {
  var s: []string = []
  s.push("b" ++ "!")
  s.push("alpha")
  let t = "kept" ++ "?"
  s.push(t)
  println("${t}")
  for x in s { println("[${x}]") }
}
`
	want := "kept?\n[b!]\n[alpha]\n[kept?]"
	out, _ := buildAndRunCapture(t, src)
	if got := strings.TrimSpace(out); got != want {
		t.Errorf("managed push:\n%s\nwant:\n%s", got, want)
	}
}

// Pushing onto an array of arrays: the element is itself a box, so this exercises the
// drop glue recursing through a buffer it also has to free.
func TestExec_DynArrayPushNestedArrays(t *testing.T) {
	t.Parallel()
	src := `let main = () -> u8 => {
  var rows: [][]i64 = []
  for var i = 0; i < 4; i+=1 {
    var row: []i64 = []
    row.push(i)
    row.push(i * 2)
    rows.push(row)
  }
  var sum = 0
  for r in rows { for v in r { sum = sum + v } }
  u8(rows.len() * 10 + sum)
}
`
	// rows 4; sum = (0+0)+(1+2)+(2+4)+(3+6) = 18; 40 + 18 = 58.
	if got := buildAndRun(t, src); got != 58 {
		t.Errorf("nested push: exit %d, want 58", got)
	}
}

// push is interior mutation with a different spelling, so it takes the same mutability
// rule — and, deliberately, the same diagnostic. A plain `let` is deeply immutable.
func TestCheck_PushNeedsAMutableReceiver(t *testing.T) {
	t.Parallel()
	const src = `
module main
let main = () -> void => {
  let xs: []i64 = [1]
  xs.push(2)
}
`
	diags := checkWithPrelude(t, src)
	if len(diags) == 0 {
		t.Fatal("push onto a `let` binding must be refused, as `xs[0] = v` is")
	}
	if !strings.Contains(diags[0], "deeply immutable") {
		t.Errorf("expected the interior-immutability diagnostic; got: %s", diags[0])
	}
}

// `noalloc` refuses push. It does not allocate on every call — amortized doubling means
// most pushes are a store — but the bound is a static promise about what a function may
// do, not a statistical one.
func TestCheck_PushIsRefusedByNoalloc(t *testing.T) {
	t.Parallel()
	const src = `
module main
let add = pure noalloc (xs: mut []i64, v: i64) -> void => { xs.push(v) }
let main = () -> void => { var a: []i64 = []; add(a, 1) }
`
	if diags := checkWithPrelude(t, src); len(diags) == 0 {
		t.Fatal("push can grow the buffer, so `noalloc` must refuse it")
	}
}
