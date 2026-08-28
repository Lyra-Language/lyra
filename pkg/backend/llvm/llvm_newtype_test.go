package llvm

import (
	"strings"
	"testing"
)

// A `newtype` is nominal to the typechecker and *transparent* to codegen: a
// `newtype Percent = u8` value is a u8 at run time, with no wrapper, no tag, and
// no LLVM type of its own. These tests pin that end to end — a newtype value is
// constructed, copied, passed, returned, stored in aggregates, matched on, and
// read back out to its base, and the observable result is the base's.
func TestExec_Newtype(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		src  string
		want int
	}{
		// The base case: construct from a base value, read back out. Assigning to
		// the base type is the only way to read a newtype (it has no accessor), so
		// this shape is in nearly every program that uses one.
		{
			"construct and read out",
			`newtype Percent = u8 where range(0..<=100)
			 let main = () -> u8 => {
			   let p: Percent = 42
			   let raw = u8(p)
			   raw
			 }`,
			42,
		},
		// Across a function boundary, in both directions. This needed the declared
		// return type to resolve — an unresolved `Percent` compared unequal to the
		// same newtype resolved from the annotation ("cannot assign Percent to
		// Percent"), which made a newtype unusable through any call.
		{
			"through a parameter and a return",
			`newtype Percent = u8
			 let bump = (p: Percent) -> Percent => p
			 let main = () -> u8 => {
			   let p: Percent = 42
			   let q: Percent = bump(p)
			   let raw = u8(q)
			   raw
			 }`,
			42,
		},
		// A copy is a copy of the base value — nothing wrapped, nothing boxed.
		{
			"copied between bindings",
			`newtype Meters = i64
			 let main = () -> u8 => {
			   let a: Meters = 5
			   let b: Meters = a
			   let d = i64(b)
			   u8(d)
			 }`,
			5,
		},
		// A struct field declared as a newtype is recorded as an UnresolvedType
		// naming it, so both the layout (i64, not an unknown named type) and the
		// field read (assignable to i64) have to see through the name.
		{
			"as a struct field",
			`newtype Meters = i64
			 struct Trip { dist: Meters, legs: u8 }
			 let main = () -> u8 => {
			   let t = Trip { dist: 5, legs: 2 }
			   let d = i64(t.dist)
			   u8(d)
			 }`,
			5,
		},
		{
			"as a fixed-array element",
			`newtype Meters = i64
			 let main = () -> u8 => {
			   let xs: [3]Meters = [1, 2, 3]
			   let d = i64(xs[1])
			   u8(d)
			 }`,
			2,
		},
		{
			"as a dynamic-array element",
			`newtype Meters = i64
			 let main = () -> u8 => {
			   let xs: []Meters = [10, 20, 30]
			   let d = i64(xs[2])
			   u8(d)
			 }`,
			30,
		},
		// A match dispatches on the base, so the scalar ladder applies unchanged —
		// the range arm is a u8 comparison.
		{
			"match on a newtype scrutinee",
			`newtype Percent = u8 where range(0..<=100)
			 let bucket = (p: Percent) -> u8 => match p {
			   0..<=50 => 1,
			   _ => 2,
			 }
			 let main = () -> u8 => bucket(75)`,
			2,
		},
		// A non-numeric base works the same way: the representation is i1.
		{
			"over bool",
			`newtype Flag = bool
			 let main = () -> u8 => {
			   let f: Flag = true
			   let b = bool(f)
			   if b { 3 } else { 4 }
			 }`,
			3,
		},
		// A signed base keeps its signedness — the negation and the later add are
		// both i64 operations, so this is 5 rather than a wrapped value.
		{
			"over a signed base, negative value",
			`newtype Delta = i64
			 let main = () -> u8 => {
			   let d: Delta = -5
			   let x = i64(d)
			   u8(x + 10)
			 }`,
			5,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if got := buildAndRun(t, c.src); got != c.want {
				t.Errorf("%s: exited %d; want %d", c.name, got, c.want)
			}
		})
	}
}

// Arithmetic under a newtype context happens at the **base's** width and
// signedness, not the i64 default.
//
// This is the case where transparency has teeth. The recorded type of an
// initializer annotated with a newtype is the newtype, so nothing narrowed its
// literal leaves: `let s: Small = 200 + 100` computed 300 in signed i64, never
// tripped a check, and truncated to 44 on the way out — while the identical
// expression against a bare u8 traps. Both halves are pinned here: the constant
// form is now a compile error (the base's range is checked when the newtype
// declares no range constraint of its own), and the runtime form traps.
func TestExec_NewtypeArithmeticUsesBaseWidth(t *testing.T) {
	t.Parallel()

	t.Run("runtime overflow traps at the base width", func(t *testing.T) {
		t.Parallel()
		// The value reaches u8 through a call, so it is not a foldable constant —
		// the check that fires is the emitted trap, not the typechecker's.
		src := `newtype Small = u8
		 let widen = (n: u8) -> Small => Small(n)
		 let main = () -> u8 => {
		   let a: Small = widen(200)
		   let x = u8(a)
		   let y: u8 = x + 100
		   y
		 }`
		if got := buildAndRun(t, src); got != 101 {
			t.Errorf("exited %d; want 101 (the overflow trap)", got)
		}
	})

	t.Run("literal leaves narrow to the base", func(t *testing.T) {
		t.Parallel()
		// 40 + 2 under a `Percent = u8` annotation must lower as u8 arithmetic.
		got, err := emitSource(t, `newtype Percent = u8 where range(0..<=100)
		 let main = () -> u8 => {
		   let p: Percent = 40 + 2
		   let raw = u8(p)
		   raw
		 }`)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(got, "uadd.with.overflow.i8") {
			t.Errorf("expected u8 checked arithmetic; got:\n%s", got)
		}
		if strings.Contains(got, "with.overflow.i64") {
			t.Errorf("arithmetic lowered at the i64 default instead of the base width:\n%s", got)
		}
	})
}

// A newtype over a *managed* base is managed: `newtype Email = string` is a
// string box, so it is retained on copy and released on death exactly as the
// base is. Getting this wrong is not cosmetic — treating the wrapper as
// unmanaged would leak every heap string that passed through one.
func TestExec_NewtypeOverString(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		src     string
		wantOut string
	}{
		{
			// A heap string built inside a function returning the newtype, then read
			// back out to `string` and printed.
			"built and returned as a newtype",
			`newtype Email = string
			 let mk = (a: string, b: string) -> Email => Email(a ++ "@" ++ b)
			 let main = () -> u8 => {
			   let e: Email = mk("user", "host")
			   let s = string(e)
			   println(s)
			   0
			 }`,
			"user@host\n",
		},
		{
			// The newtype sits in a struct field, and the struct is copied: the copy
			// must take its own reference on the field (deep retain), or the first
			// death frees a box the second still holds.
			"as a struct field, copied",
			`newtype Email = string
			 struct User { name: Email, age: u8 }
			 let main = () -> u8 => {
			   let n: Email = Email("a" ++ "b")
			   let u = User { name: n, age: 7 }
			   let copy = u
			   let s = string(copy.name)
			   println(s)
			   0
			 }`,
			"ab\n",
		},
		{
			// Managed elements under a newtype element type: the dynamic array's
			// per-element drop glue has to see the string through the name.
			"as a dynamic-array element",
			`newtype Email = string
			 let main = () -> u8 => {
			   let es: []Email = [Email("a" ++ "1"), Email("b" ++ "2")]
			   let s = string(es[1])
			   println(s)
			   0
			 }`,
			"b2\n",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			out, code := buildAndRunCapture(t, c.src)
			if code != 0 {
				t.Errorf("%s: exited %d; want 0", c.name, code)
			}
			if out != c.wantOut {
				t.Errorf("%s: stdout = %q; want %q", c.name, out, c.wantOut)
			}
		})
	}
}

// The managed-newtype programs above under AddressSanitizer, plus a static
// alloc/retain/release accounting. ASan on macOS reports use-after-free and
// double-free but not leaks, so the counts are what catch a missing release and
// ASan is what catches an early one.
func TestExec_NewtypeOverString_ASan(t *testing.T) {
	t.Parallel()
	clang := lookClang(t)

	// One allocation ("a" ++ "b"), one retain (the struct copy duplicates the
	// reference its Email field holds), two releases (each struct's death).
	src := `newtype Email = string
	 struct User { name: Email, age: u8 }
	 let main = () -> u8 => {
	   let n: Email = Email("a" ++ "b")
	   let u = User { name: n, age: 7 }
	   let copy = u
	   let s = string(copy.name)
	   if s == "ab" { 0 } else { 1 }
	 }`
	if got := buildAndRunASan(t, clang, src); got != 0 {
		t.Errorf("ASan run exited %d; want 0", got)
	}

	ir, err := emitSource(t, src)
	if err != nil {
		t.Fatal(err)
	}
	allocs := strings.Count(ir, "call i8* @lyra_rc_alloc")
	retains := strings.Count(ir, "call void @lyra_rc_retain")
	releases := strings.Count(ir, "call void @lyra_rc_release")
	if allocs != 1 {
		t.Errorf("expected 1 allocation, got %d", allocs)
	}
	if allocs+retains != releases {
		t.Errorf("conservation: %d allocations + %d retains != %d releases", allocs, retains, releases)
	}
}

// Overwriting a managed newtype target releases what the slot held, exactly as
// overwriting the bare base does.
//
// This is where reading the *wrapper* rather than the base did real damage. The
// lvalue walk carries the target's declared type, and a field declared `Email`
// arrives as that name: the managed test said no, so the overwritten box was
// never released — one leak per assignment, invisible to ASan on macOS. Fixing
// only the managed test would have been worse than the leak: the release path
// asks the same type whether the value is a string fat pointer (recover its box)
// or already a box pointer, so it would have released a fat pointer as a box.
func TestExec_NewtypeManagedAssignment(t *testing.T) {
	t.Parallel()
	clang := lookClang(t)
	cases := []struct {
		name         string
		src          string
		wantOut      string
		wantAllocs   int
		wantReleases int
	}{
		{
			// Two boxes: the initial field value and the replacement. Three release
			// sites: the overwritten value, the struct's scope-exit drop glue, and
			// the glue's own body.
			"struct field", `newtype Email = string
			 struct User { name: Email, age: u8 }
			 let main = () -> u8 => {
			   var u = User { name: Email("a" ++ "b"), age: 1 }
			   u.name = Email("c" ++ "d")
			   let s = string(u.name)
			   println(s)
			   0
			 }`,
			"cd\n", 2, 3,
		},
		{
			"dynamic-array element", `newtype Email = string
			 let main = () -> u8 => {
			   var xs: []Email = [Email("a" ++ "b"), Email("c" ++ "d")]
			   xs[0] = Email("e" ++ "f")
			   let s = string(xs[0])
			   println(s)
			   0
			 }`,
			"ef\n", 4, 4,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			out, code := buildAndRunCapture(t, c.src)
			if code != 0 || out != c.wantOut {
				t.Errorf("%s: exited %d with stdout %q; want 0 and %q", c.name, code, out, c.wantOut)
			}
			if got := buildAndRunASan(t, clang, c.src); got != 0 {
				t.Errorf("%s: ASan run exited %d; want 0", c.name, got)
			}
			ir, err := emitSource(t, c.src)
			if err != nil {
				t.Fatal(err)
			}
			// The counts are the leak detector: a missing release-old is perfectly
			// ASan-clean and prints the right answer, and macOS ASan cannot see leaks.
			if got := strings.Count(ir, "call i8* @lyra_rc_alloc"); got != c.wantAllocs {
				t.Errorf("%s: %d allocation sites; want %d", c.name, got, c.wantAllocs)
			}
			if got := strings.Count(ir, "call void @lyra_rc_release"); got != c.wantReleases {
				t.Errorf("%s: %d release sites; want %d (a missing one is the overwritten value leaking)",
					c.name, got, c.wantReleases)
			}
		})
	}
}

// A newtype registers no LLVM type and adds no indirection: the emitted module
// mentions no `%Percent`, and the value moves through plain i8 slots. The
// alternative — an LLVM type alias per newtype — would force every arithmetic,
// comparison, and coercion site to reconcile two llir types for one machine
// value, which is why this asserts the *absence*.
func TestEmit_NewtypeIsTransparent(t *testing.T) {
	t.Parallel()
	got, err := emitSource(t, `newtype Percent = u8 where range(0..<=100)
	 let bump = (p: Percent) -> Percent => p
	 let main = () -> u8 => {
	   let p: Percent = 42
	   let raw = u8(bump(p))
	   raw
	 }`)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(got, "%Percent") {
		t.Errorf("a newtype should register no LLVM type of its own:\n%s", got)
	}
	for _, want := range []string{
		"define i8 @lyra.bump(i8 ", // the parameter and return are the bare base type
		"store i8 42",              // the literal lowers at the base width
	} {
		if !strings.Contains(got, want) {
			t.Errorf("expected IR to contain %q; got:\n%s", want, got)
		}
	}
}

// A newtype over a string does not double-box: its representation is the base's
// fat pointer, so the drop glue generated for it is the base's glue, not a
// second copy under a different name.
func TestEmit_NewtypeOverStringSharesBaseGlue(t *testing.T) {
	t.Parallel()
	got, err := emitSource(t, `newtype Email = string
	 struct User { name: Email, age: u8 }
	 let main = () -> u8 => {
	   let n: Email = Email("a" ++ "b")
	   let u = User { name: n, age: 7 }
	   let copy = u
	   if copy.name == "ab" { 0 } else { 1 }
	 }`)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(got, "Email") {
		t.Errorf("glue should be keyed on the base type, not the newtype name:\n%s", got)
	}
	if !strings.Contains(got, "{ i8*, i64, i64 }") {
		t.Errorf("expected the string fat pointer as the field representation:\n%s", got)
	}
}

// A newtype is **transparent to its base's methods**: a `newtype Name = string`
// that could not be measured, sliced or trimmed would be a string you cannot do
// anything with. The base's builtins and its `self:`-taking prelude functions both
// reach through, and a multi-byte string is used so the rune semantics travel with
// them rather than degrading to bytes.
func TestExec_NewtypeOverStringKeepsStringMethods(t *testing.T) {
	t.Parallel()
	const src = `
module main
newtype Name = string
let main = () -> void => {
  let n: Name = "  héllo  ";
  println("${n.len()}");
  println(n.trim());
  println(n.slice(2, 4));
}
`
	// One call per statement, deliberately: two `slice`/`trim` results in a single
	// expression are corrupted by a pre-existing temp-lifetime bug (todo.md), which has
	// nothing to do with newtypes and would make this test fail for the wrong reason.
	if got := strings.TrimSpace(buildAndRunWithPrelude(t, src, "")); got != "9\nhéllo\nhé" {
		t.Errorf("newtype string methods = %q; want \"9\\nhéllo\\nhé\"", got)
	}
}

// A method *written for* the newtype wins over the base's — the same
// user-code-beats-builtin ordering every other rung of method resolution follows.
// Without it the fallback would make a newtype's own methods unreachable, which is
// worse than not having the fallback at all.
func TestExec_NewtypeOwnMethodBeatsBaseMethod(t *testing.T) {
	t.Parallel()
	const src = `
module main
newtype Name = string
pub let len = (self: Name) -> i64 => 99
let main = () -> void => {
  let n: Name = "abc";
  println("${n.len()}");
}
`
	if got := strings.TrimSpace(buildAndRunWithPrelude(t, src, "")); got != "99" {
		t.Errorf("own method = %q; want \"99\" (the newtype's own len, not the string builtin)", got)
	}
}

// An array newtype, end to end: it indexes and measures like its base. The array
// is the case the nominal-base refusal turns on — a scalar, a string and an array
// have no other way to be named, where a product does (`tuple`, `struct`, `data`).
func TestExec_NewtypeOverArray(t *testing.T) {
	t.Parallel()
	const src = `
module main
newtype Grid = [3]i64
let main = () -> void => {
  let g: Grid = [4, 5, 6];
  println("${g.len()} ${g[1]}");
}
`
	if got := strings.TrimSpace(buildAndRunWithPrelude(t, src, "")); got != "3 5" {
		t.Errorf("array newtype = %q; want \"3 5\"", got)
	}
}

// A newtype constructor lowers to its operand and nothing else (08/12). There is no
// wrapper at runtime — a newtype is nominal to the typechecker and transparent to
// codegen — so `Cents(150)` is a compile-time assertion about which type a value has,
// and the emitted code is what the bare literal would emit.
//
// Both spellings are exercised because the collector erases the juxtaposed one into the
// same node, and the constructor is used in the position that motivated having it at
// all: one with no annotation to infer from. The u8 case pins the width — the operand
// is narrowed to the *base*, so a constructor over a u8 newtype lowers its literal at
// u8 rather than at the i64 default, which is what a wrapper-free lowering has to get
// right for the arithmetic below it to be u8 arithmetic.
func TestExec_NewtypeConstructorLowers(t *testing.T) {
	t.Parallel()
	const src = `
module main
newtype Cents = i64
newtype Small = u8
let take = (c: Cents) -> i64 => {
  let r = i64(c)
  r
}
let main = () -> void => {
  let a = Cents(150)
  let b = Cents 275
  let s = Small(200)
  let sv = u8(s)
  let wrapped: u8 = sv.wrapping_add(100)
  println("${take(a) + take(b)} ${wrapped}")
}
`
	if got := strings.TrimSpace(buildAndRunWithPrelude(t, src, "")); got != "425 44" {
		t.Errorf("newtype construction = %q; want \"425 44\"", got)
	}
}

// `base(v)` is the universal newtype read-out (08/28): it strips exactly one newtype
// layer and is an identity at run time, like the named conversions — the spelling for
// exactly the bases those cannot name. The managed cases are the ones with teeth: the
// ownership pass must treat the call as its operand (TypeTable.IsBaseReadout, since
// the name is shadowable), or the operand's box is bound with neither retain nor
// matching release.
func TestExec_BaseReadout(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		src     string
		wantOut string
	}{
		{
			"array base, indexed and summed",
			`newtype Row = []i64
			 let sum = pure (xs: []i64) -> i64 => {
			   var t = 0
			   for x in xs { t = t + x }
			   t
			 }
			 let main = () -> u8 => {
			   let r: Row = [1, 2, 3]
			   println("${sum(base(r)) + base(r)[0]}")
			   0
			 }`,
			"7\n",
		},
		{
			"managed elements through the read-out",
			`newtype Bag = []string
			 let main = () -> u8 => {
			   let b: Bag = ["a" ++ "1", "b" ++ "2"]
			   println(base(b)[1])
			   0
			 }`,
			"b2\n",
		},
		{
			"function-type base, called",
			`newtype Handler = (i64) -> i64
			 let h: Handler = Handler((n: i64) -> i64 => n + 1)
			 let main = () -> u8 => {
			   println("${base(h)(41)}")
			   0
			 }`,
			"42\n",
		},
		{
			"a chain reads out one layer at a time",
			`newtype Inner = []i64
			 newtype Outer = Inner
			 let main = () -> u8 => {
			   let o: Outer = [5, 6]
			   println("${base(base(o))[1]}")
			   0
			 }`,
			"6\n",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			out, code := buildAndRunCapture(t, c.src)
			if code != 0 {
				t.Errorf("%s: exited %d; want 0", c.name, code)
			}
			if out != c.wantOut {
				t.Errorf("%s: stdout = %q; want %q", c.name, out, c.wantOut)
			}
		})
	}
}

// The managed read-out under AddressSanitizer, plus a **differential** accounting:
// `base(b)` returns the operand's own box, so the program must emit exactly the
// allocs/retains/releases its newtype-free control does — a missing ownership arm
// shows up as a drifted count (the unresolved-callee default retains a result the
// conversion never produced), and an early release as an ASan fault. Differential
// rather than absolute, because an array's element releases live in the runtime's
// drop loop, not in emitted IR, so absolute conservation does not hold for either
// program.
func TestExec_BaseReadout_ASan(t *testing.T) {
	t.Parallel()
	clang := lookClang(t)
	src := `newtype Bag = []string
	 let main = () -> u8 => {
	   let b: Bag = ["a" ++ "1", "b" ++ "2"]
	   let s = base(b)[1]
	   if s == "b2" { 0 } else { 1 }
	 }`
	control := `let main = () -> u8 => {
	   let xs: []string = ["a" ++ "1", "b" ++ "2"]
	   let s = xs[1]
	   if s == "b2" { 0 } else { 1 }
	 }`
	if got := buildAndRunASan(t, clang, src); got != 0 {
		t.Errorf("ASan run exited %d; want 0", got)
	}
	counts := func(source string) [3]int {
		ir, err := emitSource(t, source)
		if err != nil {
			t.Fatal(err)
		}
		return [3]int{
			strings.Count(ir, "call i8* @lyra_rc_alloc"),
			strings.Count(ir, "call void @lyra_rc_retain"),
			strings.Count(ir, "call void @lyra_rc_release"),
		}
	}
	if got, want := counts(src), counts(control); got != want {
		t.Errorf("read-out changed the accounting: allocs/retains/releases = %v, control = %v", got, want)
	}
}
