package llvm

import (
	"strings"
	"testing"
)

// `nullptr` lowers to a **typed** null constant, and pointer `==`/`!=` to an `icmp` on
// the addresses. Both halves matter, and one of them is easy to get wrong twice:
//
//   - **Typed.** clang 15 — the oldest compiler the project supports, and the one the
//     Linux container pins — still uses typed pointers, so `i8* null` and `i64* null` are
//     different constants. The pointee comes from the TypeTable, where the typechecker
//     records what a context pinned; the node itself carries only the untyped placeholder.
//   - **Address comparison, keyed on the *Lyra* type.** A `shared` aggregate is also an
//     LLVM pointer — to its box — and compares by value. Keying the pointer arm on
//     `left.Type()` turned every `shared` equality into an address comparison, and two
//     boxes with equal payloads answered false. TestExec_SharedAggregateEquality caught
//     it; keep both tests.
func TestExec_NullPtr(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		src  string
		want string
	}{
		{
			"a null pointer equals nullptr and a real one does not",
			`let main = () -> void => {
			   var n = 41
			   let real: ^i64 = unsafe { &n }
			   let none: ^i64 = nullptr
			   println("${real == nullptr} ${none == nullptr} ${real != nullptr}")
			 }`,
			"false true true",
		},
		{
			// The guard sits *outside* the unsafe block it protects, which is the whole
			// reason neither the literal nor the comparison is marked unsafe.
			"a null check guards a deref",
			`let read_or = (p: ^i64, fallback: i64) -> i64 =>
			   if p == nullptr { fallback } else { unsafe { p^ } }
			 let main = () -> void => {
			   var n = 7
			   println("${read_or(unsafe { &n }, -1)} ${read_or(nullptr, -1)}")
			 }`,
			"7 -1",
		},
		{
			// Two pointers into different storage differ; two into the same agree. This
			// is the half that would still pass if `nullptr` lowered to the wrong width,
			// which is why the pointee-width case below exists separately.
			"two pointers compare by address",
			`let main = () -> void => {
			   var a = 1
			   var b = 1
			   let pa: ^i64 = unsafe { &a }
			   let pb: ^i64 = unsafe { &b }
			   let pa2: ^i64 = unsafe { &a }
			   println("${pa == pb} ${pa == pa2}")
			 }`,
			"false true",
		},
		{
			// Different pointee widths in one program. Under typed pointers a single
			// shared `i8* null` would fail to compile against the i64 side rather than
			// miscompile — loud, but only if something exercises both.
			"pointees of different widths",
			`let main = () -> void => {
			   let a: ^u8 = nullptr
			   let b: ^i64 = nullptr
			   let c: ^f64 = nullptr
			   println("${a == nullptr} ${b == nullptr} ${c == nullptr}")
			 }`,
			"true true true",
		},
		{
			// Mutability is not part of an address's identity, so the two spellings
			// compare — and a `^mut` slot takes the literal.
			"across mutability",
			`let main = () -> void => {
			   var n = 5
			   let m: ^mut i64 = unsafe { &mut n }
			   let r: ^i64 = m
			   let z: ^mut i64 = nullptr
			   println("${m == r} ${z == nullptr}")
			 }`,
			"true true",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			src := "module main\n" + tc.src
			if got := strings.TrimSpace(buildAndRunWithPrelude(t, src, "")); got != tc.want {
				t.Errorf("got %q; want %q", got, tc.want)
			}
		})
	}
}

// **The case the feature was built for**: a C function that answers a pointer answers
// NULL on failure, and until `nullptr` there was no way to ask which happened.
//
// `getenv` is the right witness — libc, no package on either platform, and it returns
// NULL for a name that is not set rather than needing a fixture to fail on demand. The
// two calls exercise both answers in one program, so a comparison stuck at one constant
// cannot pass.
func TestExec_NullPtr_ACFunctionThatCanFail(t *testing.T) {
	t.Parallel()
	out := buildAndRunWithPrelude(t, `
module main
import std.ffi.{ is_null, to_maybe, cstring_len, with_cstring }
unsafe extern getenv: (name: ^u8) -> ^u8
let lookup = (name: string) -> string =>
  match name.with_cstring((n) => unsafe { getenv(n) }).to_maybe() {
    Some(p) => unsafe { p.decode_utf8(cstring_len(p)) },
    None => "<unset>",
  }
let main = () -> void => {
  let set = "LYRA_NULLPTR_TEST".with_cstring((n) => unsafe { getenv(n) })
  print("${lookup("LYRA_NULLPTR_TEST_ABSENT")} ${is_null(set)}")
}
`, "")
	if got := strings.TrimSpace(out); got != "<unset> true" {
		t.Errorf("got %q; want %q", got, "<unset> true")
	}
}
