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
			"the last element via from_end",
			`let main = () -> u8 => {
  let xs: []u8 = [1, 2, 3]
  xs.from_end(1)
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

// `bytes.push_utf8(s)` — a string's bytes appended to a `[]u8` in one memcpy (08/30). It
// shipped first as ordinary Lyra in the prelude and kept its name when it became a builtin,
// so the tests written against the prelude version (llvm_string_methods_test.go) exercise
// this one unchanged. What is new here is the growth arithmetic, which is where a bulk
// append can go wrong in ways a per-byte push cannot.

// **Grow to what is needed, not merely to double.** `push` doubles because it adds one
// element and cannot know what is coming; this knows exactly how many bytes are arriving,
// so a piece larger than twice the capacity must still fit. Appending 500 bytes to an empty
// buffer would overflow a buffer grown to the floor of 4, or to 2x of nothing.
func TestExec_PushUtf8GrowsToTheNeededSize(t *testing.T) {
	t.Parallel()
	src := `
let main = () -> void => {
  var big = "x"
  for _ in 0..<9 { big = big ++ big }   // 512 bytes, far past any doubling step
  var b: []u8 = []
  b.push_utf8(big)
  let s = b.decode_utf8()
  print("${b.len()} ${s.len()} ${s[0]} ${s.from_end(1)}")
}
`
	got, _ := buildAndRunCapture(t, src)
	if got = strings.TrimSpace(got); got != "512 512 x x" {
		t.Errorf("push_utf8 of a 512-byte piece = %q; want %q", got, "512 512 x x")
	}
}

// It must also still be *amortized* when the pieces are small, which is what the "at least
// double" half of the growth is for: 2000 one-byte appends onto a buffer grown only to
// `len + 1` each time would be 2000 reallocations. Correctness is what is asserted here —
// the cost is in the doc comment — since a mis-sized growth shows up as a corrupt tail.
func TestExec_PushUtf8ManySmallAppends(t *testing.T) {
	t.Parallel()
	src := `
let main = () -> void => {
  var b: []u8 = []
  for i in 0..<2000 { b.push_utf8("${i % 10}") }
  let s = b.decode_utf8()
  print("${b.len()} ${s.slice(0, 3)} ${s.slice(s.len() - 3, s.len())}")
}
`
	got, _ := buildAndRunCapture(t, src)
	// 2000 single digits; the first three are 012 and the last three 789 (1999 % 10 == 9).
	if got = strings.TrimSpace(got); got != "2000 012 789" {
		t.Errorf("2000 small appends = %q; want %q", got, "2000 012 789")
	}
}

// Interleaving with `push` has to agree about the length and the capacity, since the two
// maintain the same two fields by different arithmetic — one adds a byte, the other adds a
// run — and a disagreement writes over the other's bytes rather than erroring.
func TestExec_PushUtf8InterleavesWithPush(t *testing.T) {
	t.Parallel()
	src := `
let main = () -> void => {
  var b: []u8 = []
  b.push(65)
  b.push_utf8("BC")
  b.push(68)
  b.push_utf8("")
  b.push(69)
  print("${b.decode_utf8()} ${b.len()}")
}
`
	got, _ := buildAndRunCapture(t, src)
	if got = strings.TrimSpace(got); got != "ABCDE 5" {
		t.Errorf("interleaved = %q; want %q", got, "ABCDE 5")
	}
}

// `noalloc` refuses it, on push's reasoning: it can grow the buffer, and the bound is a
// static promise about what a function may do rather than a statistical one.
func TestCheck_PushUtf8IsRefusedByNoalloc(t *testing.T) {
	t.Parallel()
	const src = `
module main
let add = pure noalloc (b: mut []u8, s: string) -> void => { b.push_utf8(s) }
let main = () -> void => { var a: []u8 = []; add(a, "x") }
`
	if diags := checkWithPrelude(t, src); len(diags) == 0 {
		t.Fatal("push_utf8 can grow the buffer, so `noalloc` must refuse it")
	}
}

// An immutable receiver is refused, by the same predicate and the same diagnostic as
// `xs.push(v)` — it is interior mutation with a different spelling.
func TestCheck_PushUtf8NeedsAMutableReceiver(t *testing.T) {
	t.Parallel()
	const src = `
module main
let main = () -> void => { let b: []u8 = []; b.push_utf8("x") }
`
	diags := checkWithPrelude(t, src)
	if len(diags) == 0 {
		t.Fatal("push_utf8 writes through its receiver, so a `let` binding must be refused")
	}
	if !strings.Contains(diags[0], "deeply immutable") {
		t.Errorf("expected the interior-immutability diagnostic; got: %s", diags[0])
	}
}

// `xs.clear()` — length to zero, buffer kept (08/30). Rebinding (`xs = []`) already empties
// an array; what this adds is keeping the capacity, so a scratch buffer refilled every frame
// allocates once ever instead of once a frame.

// The buffer survives, which is the feature and is not directly observable — capacity is not
// a value the language exposes. What is observable is that a cleared array refills and reads
// correctly, and that its length is the refill's and not the sum of both.
func TestExec_ClearEmptiesAndRefills(t *testing.T) {
	t.Parallel()
	src := `
let main = () -> void => {
  var b: []u8 = []
  b.push_utf8("hello")
  b.clear()
  b.push_utf8("hi")
  var xs: []i64 = [1, 2, 3, 4, 5]
  xs.clear()
  xs.push(9)
  print("${b.len()} ${b.decode_utf8()} ${xs.len()} ${xs[0]}")
}
`
	got, _ := buildAndRunCapture(t, src)
	if got = strings.TrimSpace(got); got != "2 hi 1 9" {
		t.Errorf("clear then refill = %q; want %q", got, "2 hi 1 9")
	}
}

// **A managed element is released, not abandoned.** The element loop is the drop glue's
// without the free that follows it there, and it has to run *before* the length is zeroed
// since it is bounded by that length. Under ASan (llvm_ownership_test.go's suite) a missing
// release is a leak and a double one is a fault; here the assertion is that the strings the
// clear released are gone while the array is still usable.
func TestExec_ClearReleasesManagedElements(t *testing.T) {
	t.Parallel()
	src := `
let main = () -> void => {
  var xs: []string = []
  for i in 0..<50 { xs.push("value ${i}") }
  xs.clear()
  for i in 0..<3 { xs.push("after ${i}") }
  print("${xs.len()} ${xs[0]} ${xs.from_end(1)}")
}
`
	got, _ := buildAndRunCapture(t, src)
	if got = strings.TrimSpace(got); got != "3 after 0 after 2" {
		t.Errorf("clear with managed elements = %q; want %q", got, "3 after 0 after 2")
	}
}

// Clearing an already-empty array is a no-op rather than an edge case — the element loop
// runs zero times and the length is already what it is being set to.
func TestExec_ClearOfAnEmptyArray(t *testing.T) {
	t.Parallel()
	src := `
let main = () -> void => {
  var xs: []i64 = []
  xs.clear()
  xs.clear()
  xs.push(7)
  print("${xs.len()} ${xs[0]}")
}
`
	got, _ := buildAndRunCapture(t, src)
	if got = strings.TrimSpace(got); got != "1 7" {
		t.Errorf("clear of an empty array = %q; want %q", got, "1 7")
	}
}

// **Every alias sees it**, because the box is the value and this writes one of its fields —
// the same reference semantics `push` and `xs[i] = v` already have. A clear that only the
// clearing binding could see would be a different language.
func TestExec_ClearIsSeenThroughAliases(t *testing.T) {
	t.Parallel()
	src := `
let main = () -> void => {
  var a: []i64 = [1, 2, 3]
  var b = a
  a.clear()
  b.push(9)
  print("${a.len()} ${b.len()} ${a[0]}")
}
`
	got, _ := buildAndRunCapture(t, src)
	if got = strings.TrimSpace(got); got != "1 1 9" {
		t.Errorf("clear through an alias = %q; want %q", got, "1 1 9")
	}
}

// **`noalloc` permits it**, unlike `push` and `push_utf8` — it frees nothing and allocates
// nothing, it writes a length. A `noalloc` function that clears and refills is refused at
// the refill, which is where the allocation is.
func TestCheck_ClearIsAllowedByNoalloc(t *testing.T) {
	t.Parallel()
	const src = `
module main
let reset = pure noalloc (xs: mut []i64) -> void => { xs.clear() }
let main = () -> void => { var a: []i64 = [1, 2]; reset(a) }
`
	if diags := checkWithPrelude(t, src); len(diags) != 0 {
		t.Errorf("clear allocates nothing, so `noalloc` must permit it; got %v", diags)
	}
}

// It writes through its receiver, so an immutable binding is refused by the same predicate
// and the same diagnostic as `xs.push(v)`.
func TestCheck_ClearNeedsAMutableReceiver(t *testing.T) {
	t.Parallel()
	const src = `
module main
let main = () -> void => { let xs: []i64 = [1, 2]; xs.clear() }
`
	diags := checkWithPrelude(t, src)
	if len(diags) == 0 {
		t.Fatal("clear writes through its receiver, so a `let` binding must be refused")
	}
	if !strings.Contains(diags[0], "deeply immutable") {
		t.Errorf("expected the interior-immutability diagnostic; got: %s", diags[0])
	}
}

// **`[v; n]` then `clear()` is the capacity spelling**, and the reason there is no
// `reserve`: the repeat form allocates n in one go and `clear` gives back the length
// without the memory. Pinned because it is a *composition* rather than a feature — two
// builtins that each have their own reason to exist happen to make a third thing, and
// nothing would fail if `clear` started dropping the buffer except this.
func TestExec_RepeatThenClearIsASizedEmptyBuffer(t *testing.T) {
	t.Parallel()
	src := `
let main = () -> void => {
  var b: []u8 = [0; 1024]
  b.clear()
  b.push_utf8("hi")
  var xs: []i64 = [0; 100]
  xs.clear()
  xs.push(7)
  // A managed element type works too: [v; n] retains per slot and clear releases per
  // slot, so the two balance however many were made.
  var ss: []string = [""; 10]
  ss.clear()
  ss.push("a")
  print("${b.len()} ${b.decode_utf8()} ${xs.len()} ${xs[0]} ${ss.len()} ${ss[0]}")
}
`
	got, _ := buildAndRunCapture(t, src)
	if got = strings.TrimSpace(got); got != "2 hi 1 7 1 a" {
		t.Errorf("sized empty buffers = %q; want %q", got, "2 hi 1 7 1 a")
	}
}

// `xs.reserve(n)` — room for at least n elements, without adding any (08/30). It was argued
// against first, on the grounds that `[v; n]` then `clear()` already composes into a sized
// buffer and nothing in the tree builds one big enough to care. What it buys over that
// composition is the n stores the repeat form makes into slots about to be forgotten:
// filling 64 MB by appending is 6,780 µs growing, 3,536 through the composition, 2,318
// reserved.

// It grows, keeps what is already there, and adds nothing — the length after a reserve is
// the length before it, which is the whole difference from `[v; n]`.
func TestExec_ReserveGrowsWithoutAddingElements(t *testing.T) {
	t.Parallel()
	src := `
let main = () -> void => {
  var xs: []i64 = [1, 2, 3]
  xs.reserve(1000)
  xs.push(4)
  var b: []u8 = []
  b.reserve(4096)
  b.push_utf8("hi")
  print("${xs.len()} ${xs[0]} ${xs[3]} ${b.len()} ${b.decode_utf8()}")
}
`
	got, _ := buildAndRunCapture(t, src)
	if got = strings.TrimSpace(got); got != "4 1 4 2 hi" {
		t.Errorf("reserve = %q; want %q", got, "4 1 4 2 hi")
	}
}

// **It is a floor, never a shrink.** Asking for less room than the buffer already has is a
// no-op — not a request to hand memory back, and above all not a truncation of the elements
// living in it. A `reserve` that shrank would also invalidate a `data()` pointer on a call
// that reads like it does nothing.
func TestExec_ReserveNeverShrinksOrTruncates(t *testing.T) {
	t.Parallel()
	src := `
let main = () -> void => {
  var xs: []i64 = []
  xs.reserve(1000)
  for i in 0..<10 { xs.push(i) }
  xs.reserve(2)     // far below both the capacity and the length
  xs.reserve(0)
  xs.push(99)
  print("${xs.len()} ${xs[0]} ${xs[9]} ${xs[10]}")
}
`
	got, _ := buildAndRunCapture(t, src)
	if got = strings.TrimSpace(got); got != "11 0 9 99" {
		t.Errorf("reserve below the current size = %q; want %q", got, "11 0 9 99")
	}
}

// A realloc may move the buffer, so managed elements have to arrive at the new address
// intact — their boxes are not touched, only the pointers to them are copied, and nothing
// is retained or released by a reserve.
func TestExec_ReserveMovesManagedElements(t *testing.T) {
	t.Parallel()
	src := `
let main = () -> void => {
  var ss: []string = []
  for i in 0..<5 { ss.push("value ${i}") }
  ss.reserve(100000)   // large enough that the buffer almost certainly moves
  ss.push("after")
  print("${ss.len()} ${ss[0]} ${ss[4]} ${ss[5]}")
}
`
	got, _ := buildAndRunCapture(t, src)
	if got = strings.TrimSpace(got); got != "6 value 0 value 4 after" {
		t.Errorf("reserve with managed elements = %q; want %q", got, "6 value 0 value 4 after")
	}
}

// A negative n traps, through the same trap `[v; n]`'s runtime count uses. The alternative
// is a `realloc` of a sign-extended enormous size, which fails in a way that has nothing to
// do with what the caller got wrong.
func TestExec_ReserveOfANegativeCountTraps(t *testing.T) {
	t.Parallel()
	src := `
let main = () -> void => {
  var xs: []i64 = []
  var n = -1
  xs.reserve(n)
  println("unreachable")
}
`
	stderr, code := buildAndRunPanic(t, src)
	if code == 0 {
		t.Fatal("a negative reserve must trap")
	}
	if !strings.Contains(stderr, "must not be negative") {
		t.Errorf("expected the negative-length trap; got %q", stderr)
	}
}

// `noalloc` refuses it — it exists to allocate. That is the one thing separating it from
// `clear`, which writes a length and is therefore permitted.
func TestCheck_ReserveIsRefusedByNoalloc(t *testing.T) {
	t.Parallel()
	const src = `
module main
let prep = pure noalloc (xs: mut []i64, n: i64) -> void => { xs.reserve(n) }
let main = () -> void => { var a: []i64 = []; prep(a, 8) }
`
	if diags := checkWithPrelude(t, src); len(diags) == 0 {
		t.Fatal("reserve allocates, so `noalloc` must refuse it")
	}
}

// It writes the buffer pointer and the capacity, so an immutable receiver is refused by the
// same predicate as `xs.push(v)`.
func TestCheck_ReserveNeedsAMutableReceiver(t *testing.T) {
	t.Parallel()
	const src = `
module main
let main = () -> void => { let xs: []i64 = [1, 2]; xs.reserve(64) }
`
	diags := checkWithPrelude(t, src)
	if len(diags) == 0 {
		t.Fatal("reserve writes through its receiver, so a `let` binding must be refused")
	}
	if !strings.Contains(diags[0], "deeply immutable") {
		t.Errorf("expected the interior-immutability diagnostic; got: %s", diags[0])
	}
}
