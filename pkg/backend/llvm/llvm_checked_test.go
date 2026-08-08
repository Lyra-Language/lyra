package llvm

import (
	"strings"
	"testing"
)

// `checked_add`/`checked_sub`/`checked_mul`/`checked_div` (08/08) — the third member of
// the explicit-overflow family, and the one that *answers* rather than deciding:
// `(T) -> Maybe<T>`, `None` where the operation would have overflowed.
//
// The three cover the three sensible reactions to overflow, which is the point of
// trapping by default: `+` traps (the safe answer when nobody thought about it),
// `wrapping_*` means modular arithmetic, `saturating_*` means clamp, and `checked_*`
// means "I will handle it" — handing back a Maybe the caller must open.

func TestExec_CheckedArithmeticAnswersNoneOnOverflow(t *testing.T) {
	t.Parallel()
	const src = `
module main
let show<t> where t: Show = (m: Maybe<t>) -> string =>
  match m { Some v => "${v}", None => "none" }
let main = () -> void => {
  println(show(i32(2147483647).checked_add(1)));
  println(show(i32(5).checked_add(6)));
  println(show(u8(10).checked_sub(20)));
  println(show(u8(200).checked_mul(2)));
  println(show(u8(20).checked_mul(2)));
}
`
	want := "none\n11\nnone\nnone\n40"
	if got := strings.TrimSpace(buildAndRunWithPrelude(t, src, "")); got != want {
		t.Errorf("checked arithmetic =\n%q\nwant\n%q", got, want)
	}
}

// `checked_div`'s two failures are the two cases `/` traps on: a zero divisor, and
// `INT_MIN / -1`, whose true quotient is INT_MAX+1. Both must answer `None` rather than
// executing a division LLVM calls undefined — the lowering substitutes 1 for the
// divisor on those paths and the select discards the result.
func TestExec_CheckedDivRefusesItsTwoUndefinedCases(t *testing.T) {
	t.Parallel()
	const src = `
module main
let show<t> where t: Show = (m: Maybe<t>) -> string =>
  match m { Some v => "${v}", None => "none" }
let main = () -> void => {
  let m: i32 = -2147483648;
  println(show(m.checked_div(-1)));
  println(show(i32(10).checked_div(0)));
  println(show(i32(10).checked_div(3)));
  println(show(u8(10).checked_div(0)));
}
`
	want := "none\nnone\n3\nnone"
	if got := strings.TrimSpace(buildAndRunWithPrelude(t, src, "")); got != want {
		t.Errorf("checked_div =\n%q\nwant\n%q", got, want)
	}
}

// 128-bit widths, which is where the intrinsic story is not uniform: a *signed* 128-bit
// multiply-with-overflow expands to compiler-rt's `__muloti4`, which Linux clang does
// not link, so the backend substitutes its own helper of the same `{ iN, i1 }` shape
// (i128MulOverflow). That substitution is invisible from here, which is the point.
func TestExec_CheckedArithmeticAt128Bits(t *testing.T) {
	t.Parallel()
	const src = `
module main
let main = () -> void => {
  let a = i128(9000000000000000000);
  let b = match a.checked_mul(i128(20)) { Some v => v, None => i128(0) };
  match b.checked_mul(b) { Some v => println("some"), None => println("none") };
  let u = u128(18000000000000000000);
  match u.checked_mul(u) { Some v => println("some"), None => println("none") };
}
`
	if got := strings.TrimSpace(buildAndRunWithPrelude(t, src, "")); got != "none\nsome" {
		t.Errorf("128-bit checked = %q; want \"none\\nsome\"", got)
	}
}

// It is arithmetic over a scalar returning an inline union, so it allocates nothing and
// reads nothing — usable from exactly the code that most wants it.
func TestExec_CheckedArithmeticIsPureAndNoalloc(t *testing.T) {
	t.Parallel()
	const src = `
module main
let add = pure noalloc (a: i64, b: i64) -> i64 =>
  match a.checked_add(b) { Some v => v, None => 0 }
let main = () -> void => {
  println("${add(1, 2)} ${add(9223372036854775807, 1)}");
}
`
	if got := strings.TrimSpace(buildAndRunWithPrelude(t, src, "")); got != "3 0" {
		t.Errorf("pure noalloc checked = %q; want \"3 0\"", got)
	}
}

// The lowering emits **no branches**: the with-overflow intrinsic already hands back
// `{ result, overflowed }`, so the union is a select. Asserted on the IR because it is a
// property with a history — see the `<=>` and `read_line` notes in the README.
func TestEmit_CheckedArithmeticIsBranchless(t *testing.T) {
	t.Parallel()
	// Declares its own Maybe rather than using the prelude's: this asserts on emitted
	// IR, and emitSource compiles a single unit. The canonical-shape fallback stamps an
	// unmarked `data Maybe` when no marker claims the kind, so the builtin still finds
	// the type it returns.
	const src = `
data Maybe<t> = Some(t) | None
let pick = (a: i32, b: i32) -> i32 => match a.checked_add(b) { Some v => v, None => 0 }
let main = () -> u8 => { let n = pick(1, 2); 0 }
`
	got, err := emitSource(t, src)
	if err != nil {
		t.Fatal(err)
	}
	fn := funcBody(got, "pick")
	if fn == "" {
		t.Fatalf("could not find the emitted pick function in:\n%s", got)
	}
	// The `match` itself branches; what must not appear is a branch introduced by the
	// checked call, so the assertion is on the intrinsic plus the select that consumes
	// its overflow bit.
	if !strings.Contains(fn, "llvm.sadd.with.overflow.i32") {
		t.Errorf("expected the with-overflow intrinsic:\n%s", fn)
	}
	if !strings.Contains(fn, "select") {
		t.Errorf("expected a select building the Maybe:\n%s", fn)
	}
}
