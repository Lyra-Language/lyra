package llvm

import (
	"strings"
	"testing"
)

// Integer literals past 64 bits (08/08). `IntegerLiteralExpr.Value` is an int64 and the
// collector parsed with `strconv.ParseInt(…, 64)`, so a true 128-bit constant could not
// be *written* — it had to be reached through arithmetic or an `i128(x)` conversion, on a
// type the language otherwise supports fully.
//
// The magnitude now lives in a `Wide *big.Int`, nil for every literal that fits 64 bits.
// The interesting half is not the parse but the **readers**: a dozen places take
// `.Value`, and each would silently see 0.

func TestExec_WideLiteralsAtTheBoundaries(t *testing.T) {
	t.Parallel()
	const src = `
module main
let main = () -> void => {
  let mx: i128 = 170141183460469231731687303715884105727;
  let mn: i128 = -170141183460469231731687303715884105728;
  let um: u128 = 340282366920938463463374607431768211455;
  println("${mx}");
  println("${mn}");
  println("${um}");
}
`
	want := "170141183460469231731687303715884105727\n" +
		"-170141183460469231731687303715884105728\n" +
		"340282366920938463463374607431768211455"
	if got := strings.TrimSpace(buildAndRunWithPrelude(t, src, "")); got != want {
		t.Errorf("wide literals =\n%q\nwant\n%q", got, want)
	}
}

// A non-decimal base has to carry the same magnitude — the digits are parsed by the same
// big.Int path, and `GetName` renders them back in the base they were written in.
func TestExec_WideLiteralInHex(t *testing.T) {
	t.Parallel()
	const src = `
module main
let main = () -> void => {
  let h: u128 = 0xFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFF;
  let b: u128 = 0b1000000000000000000000000000000000000000000000000000000000000000000;
  println("${h} ${b}");
}
`
	if got := strings.TrimSpace(buildAndRunWithPrelude(t, src, "")); got != "340282366920938463463374607431768211455 73786976294838206464" {
		t.Errorf("wide non-decimal literals = %q", got)
	}
}

// Arithmetic on one, and a 128-bit **match pattern** — the pattern path had its own
// `strconv.ParseInt(…, 64)` and failed with a message about strconv rather than about
// the program.
func TestExec_WideLiteralInArithmeticAndPatterns(t *testing.T) {
	t.Parallel()
	const src = `
module main
let main = () -> void => {
  let mx: i128 = 170141183460469231731687303715884105727;
  println("${mx - 1}");
  let um: u128 = 340282366920938463463374607431768211455;
  match um {
    340282366920938463463374607431768211455 => println("matched"),
    _ => println("no"),
  };
}
`
	if got := strings.TrimSpace(buildAndRunWithPrelude(t, src, "")); got != "170141183460469231731687303715884105726\nmatched" {
		t.Errorf("wide arithmetic/pattern = %q", got)
	}
}

// A 64-bit literal is unchanged — `Wide` is nil for it, so every existing reader of
// `.Value` is exactly as correct as it was. This is the regression half.
func TestExec_NarrowLiteralsAreUnchanged(t *testing.T) {
	t.Parallel()
	const src = `
module main
let main = () -> void => {
  let a = 42;
  let b: u64 = 18446744073709551615;
  let c: i64 = -9223372036854775808;
  let d = 0xFF;
  println("${a} ${b} ${c} ${d}");
}
`
	if got := strings.TrimSpace(buildAndRunWithPrelude(t, src, "")); got != "42 18446744073709551615 -9223372036854775808 255" {
		t.Errorf("narrow literals = %q", got)
	}
}
