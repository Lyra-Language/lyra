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

// `checked_rem` is `%` and `checked_rem_floor` is `%%`, and the pair exists because a
// remainder fails on exactly the inputs a division does — so the operator that traps has
// a value-answering form like `/` does.
//
// **Which operator each name means is the decision the methods settled** (09/17).
// `checked_rem` is the truncated remainder, the sign of the dividend, because it is
// `checked_div`'s partner: `/` truncates, so `a == (a / b) * b + (a % b)` and the two
// halves of one division use the same word. `checked_rem_floor` is `%%`, the sign of the
// divisor. The grammar's rule names were renamed to match rather than left disagreeing.
//
// `INT_MIN % -1` is the case worth pinning: the mathematical answer is 0, but LLVM's
// `srem` is poison there and `%` traps, so the checked form answers `None` — the name
// means "the operation the operator would have refused", not "the mathematically true
// value". Rust answers `None` there too.
func TestExec_CheckedRemainders(t *testing.T) {
	t.Parallel()
	const src = `
module main
let show<t> where t: Show = (m: Maybe<t>) -> string =>
  match m { Some v => "${v}", None => "none" }
let main = () -> void => {
  println(show(i64(11).checked_rem(-3)));
  println(show(i64(11).checked_rem_floor(-3)));
  println(show(i64(-11).checked_rem(3)));
  println(show(i64(-11).checked_rem_floor(3)));
  println(show(i64(11).checked_rem(0)));
  println(show(i64(11).checked_rem_floor(0)));
  let m: i32 = -2147483648;
  println(show(m.checked_rem(-1)));
  println(show(m.checked_rem_floor(-1)));
  println(show(u8(11).checked_rem(3)));
  println(show(u8(11).checked_rem_floor(3)));
  println(show(u8(11).checked_rem(0)));
}
`
	// The first four are the operators' own answers (`11 % -3 == 2`, `11 %% -3 == -1`),
	// so the methods and the operators cannot drift. Unsigned floored and truncated agree
	// by definition, and the last is the zero divisor an unsigned type still has.
	want := "2\n-1\n-2\n1\nnone\nnone\nnone\nnone\n2\n2\nnone"
	if got := strings.TrimSpace(buildAndRunWithPrelude(t, src, "")); got != want {
		t.Errorf("checked remainders =\n%q\nwant\n%q", got, want)
	}
}

// The methods and the operators must answer the same thing wherever the operator does not
// trap — one lowering rule written twice is how they would drift, so this asks both.
func TestExec_CheckedRemaindersAgreeWithTheOperators(t *testing.T) {
	t.Parallel()
	const src = `
module main
let agree = pure noalloc (a: i64, b: i64) -> bool => {
  let r = match a.checked_rem(b) { Some v => v, None => 0 }
  let f = match a.checked_rem_floor(b) { Some v => v, None => 0 }
  r == a % b && f == a %% b
}
let main = () -> void => {
  println("${agree(11, 3)} ${agree(11, -3)} ${agree(-11, 3)} ${agree(-11, -3)}");
}
`
	if got := strings.TrimSpace(buildAndRunWithPrelude(t, src, "")); got != "true true true true" {
		t.Errorf("methods disagree with the operators: %q", got)
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
