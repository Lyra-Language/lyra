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
			   let raw: u8 = p
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
			   let raw: u8 = q
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
			   let d: i64 = b
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
			   let d: i64 = t.dist
			   u8(d)
			 }`,
			5,
		},
		{
			"as a fixed-array element",
			`newtype Meters = i64
			 let main = () -> u8 => {
			   let xs: [3]Meters = [1, 2, 3]
			   let d: i64 = xs[1]
			   u8(d)
			 }`,
			2,
		},
		{
			"as a dynamic-array element",
			`newtype Meters = i64
			 let main = () -> u8 => {
			   let xs: []Meters = [10, 20, 30]
			   let d: i64 = xs[2]
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
			   let b: bool = f
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
			   let x: i64 = d
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
		 let widen = (n: u8) -> Small => n
		 let main = () -> u8 => {
		   let a: Small = widen(200)
		   let x: u8 = a
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
		   let raw: u8 = p
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
			 let mk = (a: string, b: string) -> Email => a ++ "@" ++ b
			 let main = () -> u8 => {
			   let e: Email = mk("user", "host")
			   let s: string = e
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
			   let n: Email = "a" ++ "b"
			   let u = User { name: n, age: 7 }
			   let copy = u
			   let s: string = copy.name
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
			   let es: []Email = ["a" ++ "1", "b" ++ "2"]
			   let s: string = es[1]
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
	   let n: Email = "a" ++ "b"
	   let u = User { name: n, age: 7 }
	   let copy = u
	   let s: string = copy.name
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
			   var u = User { name: "a" ++ "b", age: 1 }
			   u.name = "c" ++ "d"
			   let s: string = u.name
			   println(s)
			   0
			 }`,
			"cd\n", 2, 3,
		},
		{
			"dynamic-array element", `newtype Email = string
			 let main = () -> u8 => {
			   var xs: []Email = ["a" ++ "b", "c" ++ "d"]
			   xs[0] = "e" ++ "f"
			   let s: string = xs[0]
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
	   let raw: u8 = bump(p)
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
	   let n: Email = "a" ++ "b"
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
	if !strings.Contains(got, "{ i8*, i64 }") {
		t.Errorf("expected the string fat pointer as the field representation:\n%s", got)
	}
}
