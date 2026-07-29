package llvm

import (
	"strings"
	"testing"
)

// A narrow signed type's minimum written as a negated literal (i8 -128,
// i16 -32768, i32 -2147483648) must lower at that type's own width, not fall
// back to i64. The magnitude (2^(bits-1)) overflows the type as a positive
// value, so before the width fix propagateLiteralType left the operand untyped
// → i64: the value truncated correctly on a plain read, but the moment it met a
// proper-width operand in typed integer arithmetic the backend emitted an
// `sdiv`/etc. mixing i64 and the narrow type, which clang rejects.

// TestEmit_NarrowSignedMin_Width pins the operand width: the negation lowers at
// the narrow type (`sub iN 0, …`) and the binding allocas at iN, with no i64
// slot. (llir renders the 2^(bits-1) constant as unsigned hex — `u0x8000` — for
// i16/i32 since it's above the signed max; clang accepts it and the exec test
// below compiles, so the assertion checks the width, not the constant spelling.)
func TestEmit_NarrowSignedMin_Width(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		src    string
		sub    string // the negation instruction's width prefix
		alloca string // the binding's alloca width
	}{
		{"i8", `let main = () -> u8 => {
		  let x: i8 = -128
		  0
		}`, "sub i8 0,", "alloca i8"},
		{"i16", `let main = () -> u8 => {
		  let x: i16 = -32768
		  0
		}`, "sub i16 0,", "alloca i16"},
		{"i32", `let main = () -> u8 => {
		  let x: i32 = -2147483648
		  0
		}`, "sub i32 0,", "alloca i32"},
	}
	for _, c := range cases {
		out, err := emitSource(t, c.src)
		if err != nil {
			t.Fatalf("%s: emit: %v", c.name, err)
		}
		if !strings.Contains(out, c.sub) {
			t.Errorf("%s: expected %q in IR, got:\n%s", c.name, c.sub, out)
		}
		if !strings.Contains(out, c.alloca) {
			t.Errorf("%s: expected %q in IR, got:\n%s", c.name, c.alloca, out)
		}
		if strings.Contains(out, "sub i64 0,") {
			t.Errorf("%s: negation lowered at i64 instead of the narrow width:\n%s", c.name, out)
		}
	}
}

// TestExec_NarrowSignedMin runs the case that used to emit invalid IR: the min
// value used in typed arithmetic against a same-width operand. -128 / -2 = 64.
func TestExec_NarrowSignedMin(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		src  string
		want int
	}{
		{"i8 min in arithmetic", `let main = () -> u8 => {
		  let a: i8 = -128
		  let b: i8 = -2
		  u8(a / b)
		}`, 64},
		{"i16 min in arithmetic", `let main = () -> u8 => {
		  let a: i16 = -32768
		  let b: i16 = -256
		  u8(a / b)
		}`, 128},
		{"i32 min in arithmetic", `let main = () -> u8 => {
		  let a: i32 = -2147483648
		  let b: i32 = -16777216
		  u8(a / b)
		}`, 128},
	}
	for _, c := range cases {
		t.Run("", func(t *testing.T) {
			t.Parallel()
			if got := buildAndRun(t, c.src); got != c.want {
				t.Errorf("%s: exited %d; want %d", c.name, got, c.want)
			}
		})
	}
}
