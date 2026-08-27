package llvm

import (
	"strings"
	"testing"
)

// **Passing a Lyra function to C as a callback.** The last of the FFI gaps the audit named,
// and the one that blocks whole libraries: `qsort`, every event loop, every GUI toolkit,
// sqlite, curl.
//
// It works because of a coincidence worth naming: a Lyra **top-level function** already
// emits the C signature. `declareFunctionAs` lowers its parameters directly, with no
// environment word, so `let cmp = (a: ^u8, b: ^u8) -> i32` becomes
// `define i32 @lyra.main.cmp(i8*, i8*)`, which *is* `int (*)(const void*, const void*)` —
// not something convertible to it. A closure is `{code, env}` and has no such word, which is
// why lyra-E066 refuses one.

// libc's `qsort`, which is the canonical case and needs no fixture.
func TestExec_CallbackToQsort(t *testing.T) {
	t.Parallel()
	out := buildAndRunWithPrelude(t, `
module main
import std.ffi.{ data_mut }
unsafe extern qsort: (out: ^mut u8, len: u64, len2: u64, fn: (^u8, ^u8) -> i32) -> void
let cmp = pure (a: ^u8, b: ^u8) -> i32 => {
  let x = unsafe { i32(a^) }
  let y = unsafe { i32(b^) }
  x - y
}
let main = () -> void => {
  var xs: []u8 = [50, 10, 40, 20, 30]
  unsafe { qsort(xs.data_mut(), 5, 1, cmp) }
  print("${xs[0]} ${xs[1]} ${xs[2]} ${xs[3]} ${xs[4]}")
}
`, "")
	if got := strings.TrimSpace(out); got != "10 20 30 40 50" {
		t.Errorf("qsort = %q; want \"10 20 30 40 50\"", got)
	}
}

// **The shape nearly every real C library uses**: a function pointer plus a `void *` context
// the library hands back untouched. That parameter is what replaces a closure's environment
// — what a capture would have carried travels through it explicitly, in a pointer the caller
// owns.
//
// The fold variant returns a value the C side accumulates, so a wrong calling convention is
// a wrong number rather than only a missing side effect.
func TestExec_CallbackWithAContextParameter(t *testing.T) {
	t.Parallel()
	checkFixture(t, `module main
import std.ffi.{ data, data_mut }
unsafe extern lyra_fixture_for_each: (data: ^u8, n: i64, fn: (u8, ^mut u8) -> void, ctx: ^mut u8) -> void
unsafe extern lyra_fixture_fold: (data: ^u8, n: i64, fn: (i64, u8) -> i64, seed: i64) -> i64

/// The context is a one-element buffer the caller owns; C only carries the pointer.
let accumulate = (value: u8, ctx: ^mut u8) -> void => unsafe { ctx^ = ctx^ + value }
let combine = pure (acc: i64, value: u8) -> i64 => acc * 2 + i64(value)

let main = () -> void => unsafe {
  var xs: []u8 = [1, 2, 3, 4]
  var total: []u8 = [0]
  lyra_fixture_for_each(xs.data(), 4, accumulate, total.data_mut())
  let folded = lyra_fixture_fold(xs.data(), 4, combine, 0)
  println("${total[0]} ${folded}")
}
`, "10 26") // 1+2+3+4 = 10; ((((0*2+1)*2+2)*2+3)*2+4) = 26
}

// A **local binding shadowing a top-level function** is the case that would otherwise
// miscompile rather than fail: the backend resolves a callback argument by name through
// `l.funcs`, so it would emit the top-level symbol for a program that means the local. The
// front end compares the scope's resolution against the symbol table's, which is what makes
// the rule exact instead of approximate.
func TestCheck_CallbackRefusesAShadowingLocal(t *testing.T) {
	t.Parallel()
	diags := analyzeWithPreludeErrors(t, `
module main
import std.ffi.{ data_mut }
unsafe extern qsort: (out: ^mut u8, len: u64, len2: u64, fn: (^u8, ^u8) -> i32) -> void
let cmp = pure (a: ^u8, b: ^u8) -> i32 => 0
let main = () -> void => {
  var xs: []u8 = [3, 1, 2]
  let cmp = (a: ^u8, b: ^u8) -> i32 => 1
  unsafe { qsort(xs.data_mut(), 3, 1, cmp) }
}
`)
	if !strings.Contains(diags, "must be a top-level function") {
		t.Errorf("diagnostics = %q; want lyra-E066", diags)
	}
}

// The three other spellings that are not a top-level function, each refused for the same
// representational reason: there is no single code pointer to hand over.
func TestCheck_CallbackRefusesClosureForms(t *testing.T) {
	t.Parallel()
	for _, c := range []struct{ name, arg, extra string }{
		{"a lambda literal", `(a: ^u8, b: ^u8) -> i32 => 0`, ""},
		{"a parameter of function type", `pick`, ""},
		{"a capturing closure", `held`, "\n  let n: i32 = 5\n  let held = (a: ^u8, b: ^u8) -> i32 => n"},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			diags := analyzeWithPreludeErrors(t, `
module main
import std.ffi.{ data_mut }
unsafe extern qsort: (out: ^mut u8, len: u64, len2: u64, fn: (^u8, ^u8) -> i32) -> void
let run = (pick: (^u8, ^u8) -> i32) -> void => {
  var xs: []u8 = [3, 1, 2]`+c.extra+`
  unsafe { qsort(xs.data_mut(), 3, 1, `+c.arg+`) }
}
let main = () -> void => println(1)
`)
			if !strings.Contains(diags, "must be a top-level function") {
				t.Errorf("diagnostics = %q; want lyra-E066", diags)
			}
		})
	}
}

// Every type in a callback's own signature must cross too — the same predicate the extern's
// own parameters take, so `(string) -> i32` is refused for the reason `extern f: (n: string)` is.
func TestCheck_CallbackSignatureMustBeFFISafe(t *testing.T) {
	t.Parallel()
	diags := analyzeWithPreludeErrors(t, `
module main
unsafe extern f: (buf: ^u8, fn: (string) -> i32) -> void
let main = () -> void => println(1)
`)
	if !strings.Contains(diags, "is a callback whose parameter 1 is string") {
		t.Errorf("diagnostics = %q; want the callback-signature error", diags)
	}
}
