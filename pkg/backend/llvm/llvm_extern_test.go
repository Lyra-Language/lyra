package llvm

import (
	"strings"
	"testing"
)

// Foreign functions, end to end: `extern` declares a C symbol, a call to one is an
// ordinary call, and `@link` reaches the link line.
//
// These call **real libraries** — libc and libm, which the test harness already links
// (`-lm`, and libc unavoidably) — rather than a fixture whose both sides we wrote. That is
// deliberate: a fixture proves the IR is self-consistent, and the thing worth proving is
// that Lyra talks to a library nobody wrote for it, with an ABI it has to match rather
// than choose. zlib is the fuller version of that proof (todo.md); these are the ones that
// need no package installed.

// The declared symbol is the name as written — `@abs`, not `lyra.main.abs`. A foreign
// symbol belongs to the linker, so mangling it would name a function nobody defines, and
// the failure would be a link error about a symbol the source never mentions.
func TestExtern_DeclaresTheCSymbolUnmangled(t *testing.T) {
	t.Parallel()
	got, err := emitSource(t, `module main
unsafe extern pure abs: (n: i32) -> i32
let main = () -> u8 => unsafe { u8(abs(-7)) }
`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "declare i32 @abs(i32") {
		t.Errorf("expected an unmangled `declare i32 @abs`:\n%s", got)
	}
	if strings.Contains(got, "@lyra.main.abs") {
		t.Errorf("a foreign symbol must not be mangled:\n%s", got)
	}
	if strings.Contains(got, "define i32 @abs") {
		t.Errorf("an extern is declared, never defined:\n%s", got)
	}
}

// The call runs: libc's abs, with the result reaching the exit code.
func TestExec_ExternCallsLibc(t *testing.T) {
	t.Parallel()
	src := `module main
unsafe extern pure abs: (n: i32) -> i32
let main = () -> u8 => unsafe { u8(abs(-7)) }
`
	if got := buildAndRun(t, src); got != 7 {
		t.Errorf("abs(-7) exited %d; want 7", got)
	}
}

// Floats cross too, and `@link("m")` is the declaration saying so. libm is what the
// harness links anyway — the point here is the f64 ABI, which no integer test exercises.
func TestExec_ExternFloatsAndLink(t *testing.T) {
	t.Parallel()
	src := `module main
@link("m")
unsafe extern pure sqrt: (x: f64) -> f64
let main = () -> u8 => unsafe { u8(sqrt(81.0).floor()) }
`
	if got := buildAndRun(t, src); got != 9 {
		t.Errorf("sqrt(81.0) exited %d; want 9", got)
	}
}

// **A `^mut T` is how a value comes back**, since nothing about C's out-parameter
// convention is expressible any other way: `frexp(8.0, &e)` returns 0.5 *and* writes 4
// through the pointer. Both halves are checked, so a pointer that was passed but never
// written to would fail rather than pass on the return value alone.
func TestExec_ExternWritesThroughAPointer(t *testing.T) {
	t.Parallel()
	src := `module main
@link("m")
unsafe extern pure frexp: (x: f64, out: ^mut i32) -> f64
let main = () -> u8 => {
  var exp: i32 = 0
  unsafe {
    let mantissa = frexp(8.0, &mut exp)
    u8(exp) * 10 + u8((mantissa * 10.0).round())
  }
}
`
	// frexp(8.0) is 0.5 * 2^4: the exponent 4 in the tens place, the mantissa's 5 in the
	// units. A pointer never written to leaves exp at 0 and fails on the tens digit.
	if got := buildAndRun(t, src); got != 45 {
		t.Errorf("frexp(8.0, &mut exp) exited %d; want 45 (exp 4, mantissa 0.5)", got)
	}
}

// A buffer crosses as `^u8` plus a length — never as a `[]T`, which lyra-E063 refuses —
// and `&xs[0]` is where the pointer comes from. strlen reads until its NUL, so this also
// checks that what C receives really is the array's own storage rather than a copy.
func TestExec_ExternTakesAPointerIntoAnArray(t *testing.T) {
	t.Parallel()
	src := `module main
unsafe extern pure strlen: (buf: ^u8) -> u64
let main = () -> u8 => {
  var bytes: []u8 = [104, 105, 33, 0]
  unsafe { u8(strlen(&bytes[0])) }
}
`
	if got := buildAndRun(t, src); got != 3 {
		t.Errorf(`strlen("hi!") exited %d; want 3`, got)
	}
}

// A `void` return is a call with no value, in statement position. Nothing observable comes
// back from srand, which is the point — the previous tests would all pass on a lowering
// that assumed every extern produces a value.
func TestExec_ExternReturningVoid(t *testing.T) {
	t.Parallel()
	src := `module main
unsafe extern det srand: (n: u32) -> void
let main = () -> u8 => {
  unsafe { srand(42) }
  3
}
`
	got, err := emitSource(t, src)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "declare void @srand(i32") {
		t.Errorf("expected `declare void @srand`:\n%s", got)
	}
	if code := buildAndRun(t, src); code != 3 {
		t.Errorf("program exited %d; want 3", code)
	}
}

// **Two modules declaring one foreign function share a single `declare`.** They must:
// each `extern` is private to the module that writes it (there is no `pub extern`), but
// the C symbol they name is global, so two `declare`s of one name is invalid IR. This is
// the ordinary case rather than a corner — two libraries both using `strlen` is what a
// standard library looks like — and it is also what would break if an extern were keyed
// as exported, since the two declarations would then collide in the front end instead.
func TestExec_OneDeclarePerForeignSymbolAcrossModules(t *testing.T) {
	t.Parallel()
	files := map[string]string{
		"app.lyra": `import lib.a
import lib.b
let main = () -> u8 => u8(a.twice(3) + b.thrice(2))
`,
		"lib/a.lyra": `module lib.a
unsafe extern pure abs: (n: i32) -> i32
pub let twice = unsafe (n: i32) -> i64 => i64(unsafe { abs(n) }) * 2
`,
		"lib/b.lyra": `module lib.b
unsafe extern pure abs: (n: i32) -> i32
pub let thrice = unsafe (n: i32) -> i64 => i64(unsafe { abs(n) }) * 3
`,
	}
	if n := strings.Count(emitModules(t, files), "declare i32 @abs("); n != 1 {
		t.Errorf("got %d declarations of @abs; want exactly 1", n)
	}
	if got := buildAndRunModules(t, files); got != 12 {
		t.Errorf("program exited %d; want 12 (3*2 + 2*3)", got)
	}
}

// And two that *disagree* are refused, loudly. Only one of them can describe the function
// that will be linked, so emitting either silently picks a winner — rule 5, and the one
// case where sharing the declaration would be wrong.
func TestExtern_ConflictingSignaturesForOneSymbolAreRefused(t *testing.T) {
	t.Parallel()
	_, err := emitModulesErr(t, map[string]string{
		"app.lyra": `import lib.a
import lib.b
let main = () -> u8 => u8(a.one() + b.two())
`,
		"lib/a.lyra": `module lib.a
unsafe extern pure abs: (n: i32) -> i32
pub let one = unsafe () -> i64 => i64(unsafe { abs(-1) })
`,
		"lib/b.lyra": `module lib.b
unsafe extern pure abs: (n: i64) -> i64
pub let two = unsafe () -> i64 => unsafe { abs(-2) }
`,
	})
	if err == nil {
		t.Fatal("two `extern abs` declarations with different signatures must be refused")
	}
	// The message names both declarations, by file: two externs of one name are usually
	// in two files, so a bare line:col would print the same position twice.
	for _, want := range []string{"declared twice", "lib/a.lyra", "lib/b.lyra"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q should mention %q", err, want)
		}
	}
}

// **A closure may call a foreign function**, which it could not until 08/19.
//
// `captures.globalNames` is the set of names a lambda body reaches without capturing — a
// switch over the top-level declaration kinds — and it had a case for `VarDeclStmt` and
// `TypeDeclStmt` and none for `ExternDeclStmt`. So the extern's name was collected as a
// **free variable**: nothing records a type for a binding that is not one, and the backend
// failed with *no type recorded for captured binding "strlen"* on a program the front end
// had checked clean. Hazard 8, and the fourth switch over declaration kinds to be missing
// this same node.
//
// Found while testing a scoped `with_cstring(s, f)`, which is the shape every language
// shipping a CString also ships — so this is the bug that stood between Lyra and that
// form, rather than an oddity about closures.
func TestExec_ExternCalledFromInsideAClosure(t *testing.T) {
	t.Parallel()
	src := `module main
unsafe extern pure strlen: (buf: ^u8) -> u64
let apply<t> = (bytes: mut []u8, f: (^u8) -> t) -> t => unsafe { f(&bytes[0]) }
let main = () -> u8 => {
  var b: []u8 = [104, 105, 33, 0]
  u8(apply(b, (p: ^u8) -> u64 => unsafe { strlen(p) }))
}
`
	if got := buildAndRun(t, src); got != 3 {
		t.Errorf("strlen through a closure exited %d; want 3", got)
	}
}
