package llvm

import (
	"os/exec"
	"strings"
	"testing"
)

// A `mut` parameter is a *mutable borrow*: the callee writes through to the
// caller's value. It used to be passed **by value**, so the callee mutated a
// private copy and every write was silently discarded — and the loss depended on
// the type, with no diagnostic either way: a `mut []T` or `mut shared T`
// propagated because it is already a pointer, while a `mut` struct, tuple, or
// `[N]T` dropped the write. `mut` is now passed by reference (functions.go,
// paramIsByRef), so every one of these lands.

// TestExec_MutParameter_WritesReachCaller is the core of the fix: the shapes that
// silently lost their writes, alongside the two that always worked, so a
// regression in either direction shows up here.
func TestExec_MutParameter_WritesReachCaller(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want int
	}{
		{
			// A stack struct: the shape that lost its write.
			"stack struct field", `struct Pt { x: i64, y: i64 }
let poke = (p: mut Pt) -> void => { p.x = 99 }
let main = () -> u8 => {
  var q = Pt { x: 1, y: 2 }
  poke(q)
  u8(q.x)
}`, 99,
		},
		{
			// A fixed-size array: likewise by value, likewise lost. This one also
			// exercises arrayLValue, which would otherwise materialize a copy of the
			// array into a fresh alloca and mutate that.
			"fixed-size array element", `let bump = (xs: mut [3]i64) -> void => { xs[0] = 42 }
let main = () -> u8 => {
  var arr: [3]i64 = [1, 2, 3]
  bump(arr)
  u8(arr[0])
}`, 42,
		},
		{
			// A nested field through a mixed path.
			"nested struct field", `struct Inner { v: i64 }
struct Outer { inner: Inner, n: i64 }
let deep = (o: mut Outer) -> void => { o.inner.v = 77 }
let main = () -> u8 => {
  var o = Outer { inner: Inner { v: 1 }, n: 0 }
  deep(o)
  u8(o.inner.v)
}`, 77,
		},
		{
			// Forwarding a by-ref parameter to another one: the inner callee must get
			// the caller's address, not the address of a local copy of it.
			"forwarded through two callees", `struct Pt { x: i64 }
let inner = (p: mut Pt) -> void => { p.x = 55 }
let outer = (p: mut Pt) -> void => { inner(p) }
let main = () -> u8 => {
  var q = Pt { x: 1 }
  outer(q)
  u8(q.x)
}`, 55,
		},
		{
			// A struct element of an array, addressed through the caller's storage.
			"struct inside a mut array", `struct Pt { x: i64 }
let bump = (ps: mut [2]Pt) -> void => { ps[1].x = 33 }
let main = () -> u8 => {
  var ps: [2]Pt = [Pt { x: 1 }, Pt { x: 2 }]
  bump(ps)
  u8(ps[1].x)
}`, 33,
		},
		{
			// Already worked (the value is a box pointer) — pinned so by-reference
			// passing, which adds one indirection, doesn't break it.
			"dynamic array element", `let bump = (xs: mut []u8) -> void => { xs[0] = 7 }
let main = () -> u8 => {
  var xs: []u8 = [1, 2, 3]
  bump(xs)
  xs[0]
}`, 7,
		},
		{
			// Also already worked, via the shared box.
			"shared struct field", `struct Pt { x: i64 }
let poke = (p: mut shared Pt) -> void => { p.x = 21 }
let main = () -> u8 => {
  var q: shared Pt = Pt { x: 1 }
  poke(q)
  u8(q.x)
}`, 21,
		},
		{
			// An element of the caller's array passed as the `mut` argument: the
			// argument is an lvalue *path*, not a bare binding, so the call site has
			// to compute its address the same way an assignment target would.
			"array element passed as the argument", `struct Pt { x: i64 }
let poke = (p: mut Pt) -> void => { p.x = 88 }
let main = () -> u8 => {
  var ps: [2]Pt = [Pt { x: 1 }, Pt { x: 2 }]
  poke(ps[0])
  u8(ps[0].x)
}`, 88,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := buildAndRun(t, c.src); got != c.want {
				t.Errorf("exited %d; want %d (the `mut` write did not reach the caller)", got, c.want)
			}
		})
	}
}

// A `mut` parameter must not be *framed*: it is a borrow, so the callee releases
// nothing at exit — the caller still owns the value. Assigning a managed field
// through it releases only the value being overwritten (see the release-IR test).
// Together with the write landing, this is the leak that by-reference passing
// closes: the release used to be suppressed entirely for a borrowed root.
func TestExec_MutParameter_ManagedFieldIsLeakFree(t *testing.T) {
	src := `struct Person { name: string }
let rename = (p: mut Person) -> void => {
  p.name = "x" ++ "y"
}
let main = () -> u8 => {
  var p: Person = Person { name: "a" ++ "b" }
  rename(p)
  if p.name == "xy" { 0 } else { 1 }
}`
	if got := buildAndRun(t, src); got != 0 {
		t.Fatalf("exited %d; want 0 (the rename must reach the caller)", got)
	}
	clang, err := exec.LookPath("clang")
	if err != nil || !asanAvailable(t, clang) {
		t.Skip("ASan runtime not available; ran without it")
	}
	if got := buildAndRunASan(t, clang, src); got != 0 {
		t.Errorf("under ASan: exited %d; want 0", got)
	}
}

// The IR shape: a by-reference `mut` parameter is a pointer parameter bound
// directly as the binding's slot, with no entry-block copy. An `own` parameter of
// the same type stays by value — the contrast is the convention.
func TestEmit_MutParameter_IsPointerNoCopy(t *testing.T) {
	byRef, err := emitSource(t, `struct Pt { x: i64 }
let poke = (p: mut Pt) -> void => { p.x = 9 }
let main = () -> u8 => {
  var q = Pt { x: 1 }
  poke(q)
  0
}`)
	if err != nil {
		t.Fatalf("emit: %v", err)
	}
	if !strings.Contains(byRef, "define void @poke(%Pt* ") {
		t.Errorf("expected @poke to take a %%Pt* parameter, got:\n%s", byRef)
	}
	// The callee must not copy the incoming value into its own alloca — that copy
	// *was* the bug.
	if strings.Contains(fnBody(byRef, "poke"), "alloca %Pt") {
		t.Errorf("@poke copied its by-ref parameter into an alloca:\n%s", fnBody(byRef, "poke"))
	}

	byVal, err := emitSource(t, `struct Pt { x: i64 }
let take = (p: own Pt) -> i64 => p.x
let main = () -> u8 => {
  let q = Pt { x: 1 }
  u8(take(q))
}`)
	if err != nil {
		t.Fatalf("emit: %v", err)
	}
	if !strings.Contains(byVal, "define i64 @take(%Pt ") {
		t.Errorf("expected @take to take a by-value %%Pt parameter, got:\n%s", byVal)
	}
}

// A `mut` on a copied scalar stays by value: the modifier is inert there
// (lyra-W010 says so via the shared types.IsCopiedScalar predicate), there is no
// interior to write through, and passing by reference would change the ABI and
// reject a literal argument for no observable gain.
func TestEmit_MutScalarParameter_StaysByValue(t *testing.T) {
	out, err := emitSource(t, `let twice = (n: mut i64) -> i64 => n + n
let main = () -> u8 => u8(twice(21))`)
	if err != nil {
		t.Fatalf("emit: %v", err)
	}
	if !strings.Contains(out, "define i64 @twice(i64 ") {
		t.Errorf("expected @twice to take a by-value i64, got:\n%s", out)
	}
	if got := buildAndRun(t, `let twice = (n: mut i64) -> i64 => n + n
let main = () -> u8 => u8(twice(21))`); got != 42 {
		t.Errorf("exited %d; want 42", got)
	}
}

// fnBody returns the text of one `define`d function from an IR module, so a test
// can assert on a single callee rather than the whole module.
func fnBody(ir, name string) string {
	start := strings.Index(ir, "@"+name+"(")
	if start < 0 {
		return ""
	}
	rest := ir[start:]
	if end := strings.Index(rest, "\ndefine "); end >= 0 {
		return rest[:end]
	}
	return rest
}

// A `ref` parameter is a borrow too, so it is also passed by reference — copying
// a value to lend it out read-only is pure waste (a `ref [8]i64` used to be a
// 64-byte first-class aggregate at every call). It cannot write, so the only
// observable difference is that the callee sees the caller's live value rather
// than a snapshot, and the aliasing that could expose is rejected at the call
// site (checkExclusiveMutableBorrow).
func TestExec_RefParameter_ByReference(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want int
	}{
		{
			"struct binding", `struct Pt { x: i64, y: i64 }
let read = (p: ref Pt) -> i64 => p.x + p.y
let main = () -> u8 => {
  let p = Pt { x: 3, y: 4 }
  u8(read(p))
}`, 7,
		},
		{
			// A temporary has no storage to point at, and unlike `mut` it is
			// perfectly legitimate to lend one out — the call site materializes it.
			"temporary argument", `struct Pt { x: i64, y: i64 }
let read = (p: ref Pt) -> i64 => p.x + p.y
let main = () -> u8 => u8(read(Pt { x: 1, y: 1 }))`, 2,
		},
		{
			"fixed-size array", `let read = (xs: ref [4]i64) -> i64 => xs[0] + xs[3]
let main = () -> u8 => {
  let a: [4]i64 = [10, 20, 30, 40]
  u8(read(a))
}`, 50,
		},
		{
			// Forwarding a by-ref parameter to another one passes the pointer along.
			"forwarded to another ref", `struct Pt { x: i64, y: i64 }
let read = (p: ref Pt) -> i64 => p.x + p.y
let outer = (p: ref Pt) -> i64 => read(p)
let main = () -> u8 => {
  let p = Pt { x: 2, y: 5 }
  u8(outer(p))
}`, 7,
		},
		{
			// A managed temporary lent through `ref`: materialized, read, then
			// released after the statement by the ordinary temp machinery.
			"managed temporary", `let same = (s: ref string) -> bool => s == "hi"
let main = () -> u8 => { if same("h" ++ "i") { 0 } else { 1 } }`, 0,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := buildAndRun(t, c.src); got != c.want {
				t.Errorf("exited %d; want %d", got, c.want)
			}
		})
	}
}

// The ABI: a `ref` aggregate is a pointer, not a copy. A `ref` on a copied scalar
// stays by value for the same reason `mut` does — nothing to point at, and
// lyra-W010 already calls the modifier inert there.
func TestEmit_RefParameter_IsPointer(t *testing.T) {
	out, err := emitSource(t, `let sum = (xs: ref [8]i64) -> i64 => xs[0] + xs[7]
let scale = (n: ref i64) -> i64 => n + 1
let main = () -> u8 => 0`)
	if err != nil {
		t.Fatalf("emit: %v", err)
	}
	if !strings.Contains(out, "define i64 @sum([8 x i64]* ") {
		t.Errorf("expected @sum to take a [8 x i64]* parameter, got:\n%s", out)
	}
	if !strings.Contains(out, "define i64 @scale(i64 ") {
		t.Errorf("expected @scale to keep its by-value i64 parameter, got:\n%s", out)
	}
}
