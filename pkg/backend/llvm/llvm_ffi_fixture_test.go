package llvm

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The FFI fixture: `testdata/ffi_fixture.c`, compiled and linked into the program under
// test. Its own header comment says what belongs in it.
//
// **Why a fixture at all, when the other extern tests deliberately call real libraries.**
// libc and libm prove the thing that matters most — that Lyra matches an ABI it did not
// choose — but their surface is i32/i64/f64 and pointers, so they leave most of the
// boundary untested: `float`, the narrow integer widths, a mixed register-class argument
// list, a list long enough to spill to the stack, and a struct whose layout both sides must
// agree on. Those are exactly the cases where a mismatch **links cleanly and computes the
// wrong answer**, which is the failure mode worth having tests for. zlib is the fuller
// real-library proof and lives in `examples/`; putting it in CI needs a package installed
// (todo.md), and this needs nothing.

// The two computed expectations, in one place because two things assert them: the Lyra
// programs below, and `testdata/ffi_oracle.c`, which makes the same calls from C. See
// TestExec_FFIFixture_CAgreesWithLyra for why the second one exists.
const (
	wantNarrow = "319197"                     // -3 + 200*2 + -300*4 + 40000*8
	wantPoint  = "24 1013.5 1116 11 4 1 1100" // sizeof, before, after, then the bumped fields
)

// fixturePath is the C fixture's absolute path, and fixtureSource its bytes — the latter
// because the compile cache keys on `extraArgs`, which names the file rather than its
// contents. See compileCachedSalted.
func fixtureParts(t *testing.T) (path, source string) {
	t.Helper()
	p := testdataPath(t, "ffi_fixture.c")
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return p, string(b)
}

// runWithFixture builds src against the fixture and returns its stdout. Output rather than
// an exit code: these tests compare arithmetic that does not fit in 8 bits, and a truncated
// answer is precisely the kind of wrongness they exist to catch.
func runWithFixture(t *testing.T, src string) string {
	t.Helper()
	clang := lookClang(t)
	// Through the prelude path, not emitSource: these programs import std.ffi, and
	// driver.Analyze resolves no import graph — an unresolved import binds nothing and
	// every use of it reports "undefined", which reads as a language bug rather than a
	// harness one.
	ir := emitWithPrelude(t, src)
	path, source := fixtureParts(t)
	bin := compileCachedSalted(t, clang, ir, source, path)
	out, err := exec.Command(bin).Output()
	if err != nil {
		var ee *exec.ExitError
		if !errors.As(err, &ee) {
			t.Fatalf("running the binary failed: %v", err)
		}
		t.Fatalf("the program exited %d\nstderr: %s", ee.ExitCode(), ee.Stderr)
	}
	return strings.TrimSpace(string(out))
}

func checkFixture(t *testing.T, src, want string) {
	t.Helper()
	if got := runWithFixture(t, src); got != want {
		t.Errorf("got %q; want %q", got, want)
	}
}

// **The narrow integer widths**, which libc's surface never exercises. Through a prototype
// C applies no default promotions, so each argument is passed at its declared width and the
// callee may assume the high bits are extended the right way — sign for i8/i16, zero for
// u8/u16. Getting that backwards is invisible in the IR and wrong only for some values, so
// the fixture weights each argument by width: any one arriving mangled changes the sum.
//
// -3 and -300 are the ones that matter. A i8 passed zero-extended reads as 253, and a u8
// passed sign-extended reads as -56.
func TestExec_FFIFixture_NarrowIntegerWidths(t *testing.T) {
	t.Parallel()
	checkFixture(t, `module main
unsafe extern pure lyra_fixture_narrow: (i8, u8, i16, u16) -> i32
let main = () -> void => unsafe {
  let a: i8 = -3
  let b: u8 = 200
  let c: i16 = -300
  let d: u16 = 40000
  println(lyra_fixture_narrow(a, b, c, d))
}
`, wantNarrow)
}

// **`float`.** libm is double-only, so nothing else in the suite crosses a f32 — and a
// Lyra `f32` lowered as a `double` still links, because C has no way to complain.
func TestExec_FFIFixture_Float32(t *testing.T) {
	t.Parallel()
	checkFixture(t, `module main
unsafe extern pure lyra_fixture_f32: (f32, f32) -> f32
let main = () -> void => unsafe {
  let x: f32 = 2.5
  let y: f32 = 4.0
  println(lyra_fixture_f32(x, y))
}
`, "11.5")
}

// **Mixed register classes in one argument list.** Integers and floats are allocated from
// separate banks on both targets, so where the sixth argument lands depends on how the
// first five were classified — a width error anywhere shifts everything after it.
func TestExec_FFIFixture_MixedRegisterClasses(t *testing.T) {
	t.Parallel()
	checkFixture(t, `module main
unsafe extern pure lyra_fixture_mixed: (i32, f64, i64, f32, u8, f64) -> f64
let main = () -> void => unsafe {
  let a: i32 = 1
  let d: f32 = 8.0
  let e: u8 = 16
  println(lyra_fixture_mixed(a, 2.5, 4, d, e, 32.25))
}
`, "63.75") // 1 + 2.5 + 4 + 8 + 16 + 32.25
}

// **An argument list long enough to spill.** Ten i64s exceeds the register budget on both
// AArch64 (8) and x86-64 (6 integer), so the tail arrives on the stack at offsets the two
// sides must agree on independently.
func TestExec_FFIFixture_ArgumentsSpillToTheStack(t *testing.T) {
	t.Parallel()
	checkFixture(t, `module main
unsafe extern pure lyra_fixture_many: (i64, i64, i64, i64, i64, i64, i64, i64, i64, i64) -> i64
let main = () -> void =>
  unsafe { println(lyra_fixture_many(1, 1, 1, 1, 1, 1, 1, 1, 1, 1)) }
`, "55") // 1+2+3+…+10
}

// **C's `long` through std.ffi's aliases**, which is the one width that moves between
// targets. The point is not that the arithmetic is hard but that `CLong` is a grep target:
// the day this compiler targets Windows x64, this test is what fails.
func TestExec_FFIFixture_CLong(t *testing.T) {
	t.Parallel()
	checkFixture(t, `module main
import std.ffi.{ CLong, CULong }
unsafe extern pure lyra_fixture_long: (CLong, CULong) -> CLong
let main = () -> void => unsafe {
  let a: CLong = 1000000
  let b: CULong = 500
  println(lyra_fixture_long(a, b))
}
`, "1000250")
}

// **Out-parameters at three widths.** Writing through a pointer is how a C function returns
// more than one value, and `&mut x` is the whole of Lyra's side of it.
func TestExec_FFIFixture_OutParameters(t *testing.T) {
	t.Parallel()
	checkFixture(t, `module main
unsafe extern pure lyra_fixture_out: (^mut i32, ^mut f64, ^mut u8) -> void
let main = () -> void => unsafe {
  var i: i32 = 0
  var d: f64 = 0.0
  var b: u8 = 0
  lyra_fixture_out(&mut i, &mut d, &mut b)
  println("${i} ${d} ${b}")
}
`, "-12345 2.5 200")
}

// **A struct across the boundary, by pointer** — by value has no C spelling in a Lyra
// signature (lyra-E063), so this is the whole of the aggregate boundary.
//
// It is the sharpest test here, because nothing about it is checked by either compiler:
// both sides independently lay out `{ i32, u8, f64, i64 }`, and if Lyra's field order,
// alignment or tail padding differ by a byte the program still links and still runs. The
// fixture reports its own `sizeof` and `offsetof` so a disagreement is reported as the
// layout mismatch it is, rather than as a wrong sum somebody has to work backwards from.
func TestExec_FFIFixture_StructLayoutMatchesC(t *testing.T) {
	t.Parallel()
	checkFixture(t, `module main
struct Point { x: i32, tag: u8, weight: f64, id: i64 }
unsafe extern pure lyra_fixture_point_size: () -> i64
unsafe extern pure lyra_fixture_point_offset: (i32) -> i64
unsafe extern pure lyra_fixture_read_point: (^Point) -> f64
unsafe extern pure lyra_fixture_bump_point: (^mut Point) -> void
let main = () -> void => unsafe {
  var p = Point { x: 10, tag: 3, weight: 0.5, id: 1000 }
  let before = lyra_fixture_read_point(&p)
  lyra_fixture_bump_point(&mut p)
  let after = lyra_fixture_read_point(&p)
  println("${lyra_fixture_point_size()} ${before} ${after} ${p.x} ${p.tag} ${p.weight} ${p.id}")
}
`, wantPoint)
}

// **A buffer in and out**, which is what a real library's data interface looks like: the
// caller owns the memory and passes a length, because ownership never crosses. `data()` and
// `data_mut()` are std.ffi's two spellings of the pointer, and this is the first test that
// runs them against a C function rather than against Lyra.
func TestExec_FFIFixture_ByteBuffers(t *testing.T) {
	t.Parallel()
	checkFixture(t, `module main
import std.ffi.{ data, data_mut }
unsafe extern pure lyra_fixture_sum_bytes: (^u8, i64) -> i64
unsafe extern pure lyra_fixture_fill_bytes: (^mut u8, i64, u8) -> void
let main = () -> void => unsafe {
  var xs: []u8 = [0, 0, 0, 0, 0]
  lyra_fixture_fill_bytes(xs.data_mut(), 5, 10)
  println("${xs[0]} ${xs[4]} ${lyra_fixture_sum_bytes(xs.data(), 5)}")
}
`, "10 14 60") // 10,11,12,13,14
}

// **A C string coming back**, read through std.ffi rather than by hand — the direction the
// module's `cstring_len` and `decode_utf8` exist for. The greeting is deliberately not
// ASCII: a byte length and a rune count differ, which is the whole reason `decode_utf8`
// counts the way it does.
func TestExec_FFIFixture_CStringComingBack(t *testing.T) {
	t.Parallel()
	checkFixture(t, `module main
import std.ffi.{ CBuffer, cstring_len, decode_utf8 }
unsafe extern pure lyra_fixture_greeting: () -> ^u8
let main = () -> void => unsafe {
  let p = lyra_fixture_greeting()
  let n = cstring_len(p)
  let s = decode_utf8(CBuffer { ptr: p, len: n })
  println("${n} ${s.len()} ${s}")
}
`, "11 10 héllo, ffi")
}

// `rune` is an i32 code point on the Lyra side and a plain int32_t here, so it crosses as
// one — the one non-numeric scalar a foreign signature accepts.
func TestExec_FFIFixture_Rune(t *testing.T) {
	t.Parallel()
	checkFixture(t, `module main
unsafe extern pure lyra_fixture_next_rune: (rune) -> rune
let main = () -> void => unsafe { println(lyra_fixture_next_rune('a')) }
`, "b")
}

// **The expectations above are C's, not Lyra's**, and this is what says so.
//
// An ABI test whose expected value was read off what the Lyra program printed asserts that
// Lyra agrees with itself — true by construction, and not the claim. `testdata/ffi_oracle.c`
// makes the same calls from C and prints them the same way; demanding the two outputs match
// is the claim. It also survives editing: change the fixture's arithmetic and both sides
// move together, so a change that moves only one is exactly the bug worth catching.
//
// Only the two *computed* cases are mirrored. The rest are single values a reader can check
// against the fixture by eye, and a C main for each would be ceremony rather than evidence.
func TestExec_FFIFixture_CAgreesWithLyra(t *testing.T) {
	t.Parallel()
	clang := lookClang(t)
	fixture := testdataPath(t, "ffi_fixture.c")
	oracle := testdataPath(t, "ffi_oracle.c")

	bin := filepath.Join(t.TempDir(), "oracle")
	if out, err := exec.Command(clang, "-std=c99", oracle, fixture, "-o", bin).CombinedOutput(); err != nil {
		t.Fatalf("compiling the oracle failed: %v\n%s", err, out)
	}
	out, err := exec.Command(bin).Output()
	if err != nil {
		t.Fatalf("running the oracle failed: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) != 2 {
		t.Fatalf("the oracle printed %d lines, want 2:\n%s", len(lines), out)
	}
	if got := strings.TrimSpace(lines[0]); got != wantNarrow {
		t.Errorf("C computes narrow = %q; the Lyra test expects %q", got, wantNarrow)
	}
	if got := strings.TrimSpace(lines[1]); got != wantPoint {
		t.Errorf("C computes the point line = %q; the Lyra test expects %q", got, wantPoint)
	}
}

// testdataPath is the absolute path of a file beside this test. Absolute because clang is
// run from wherever the harness happens to be, which is not necessarily this directory.
func testdataPath(t *testing.T, name string) string {
	t.Helper()
	p, err := filepath.Abs(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// **A variadic `extern` calls a variadic C function**, which a fixed-arity declaration of
// one does not: it links and computes garbage, because Apple aarch64 passes variadic
// arguments on the stack while the fixed convention passes them in registers. That was the
// state until 08/26 — the compiler accepted the declaration and said nothing.
//
// Tested through the fixture's own `va_arg` readers rather than through printf. A format
// string is a second language, and asserting on rendered text would be testing the C
// library's formatter rather than the boundary.
func TestExec_FFIFixture_VariadicCall(t *testing.T) {
	t.Parallel()
	checkFixture(t, `module main
unsafe extern lyra_fixture_va_sum: (i32, ...) -> i64
let main = () -> void => unsafe {
  let n: i32 = 4
  let a: i32 = 1
  let b: i32 = 2
  let c: i32 = 30
  let d: i32 = 400
  println(lyra_fixture_va_sum(n, a, b, c, d))
}
`, "433")
}

// **C's default argument promotions, which the compiler owes the caller.** An integer
// narrower than `int` widens to `int` and a `float` to `double`, and `va_arg` reads each
// argument at the promoted type — so an unpromoted one is a slot of the wrong size in the
// wrong place. The emitted IR is `zext i8 → i32`, `fpext float → double`, matching what
// clang emits for the same call.
func TestExec_FFIFixture_VariadicPromotions(t *testing.T) {
	t.Parallel()
	checkFixture(t, `module main
unsafe extern lyra_fixture_va_mixed: (i32, ...) -> f64
let main = () -> void => unsafe {
  let n: i32 = 3
  let a: u8 = 1
  let b: f32 = 2.0
  let c: f64 = 3.0
  println(lyra_fixture_va_mixed(n, f64(a), b, c))
}
`, "123")
}

// **Signedness is the half LLVM cannot recover.** An i16 and a u16 are the same `i16` in
// the IR, so the extension's sign has to come from the Lyra type; without it -300 arrives
// as 65236, which is what this printed before `promoteVariadicArg` consulted the recorded
// type. Both signs, so a fix that hardcodes either one fails.
func TestExec_FFIFixture_VariadicSignedness(t *testing.T) {
	t.Parallel()
	checkFixture(t, `module main
unsafe extern lyra_fixture_va_signed: (i32, ...) -> i64
let main = () -> void => unsafe {
  let n: i32 = 4
  let neg: i16 = -300
  let pos: u16 = 60000
  let small: i8 = -5
  let byte: u8 = 250
  println(lyra_fixture_va_signed(n, neg, pos, small, byte))
}
`, "59945") // -300 + 60000 + -5 + 250
}
