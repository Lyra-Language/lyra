package llvm

import (
	"strings"
	"testing"
)

// A printed float reads back as the same value (08/13). Printing was one
// `snprintf("%g")` until then, whose default is **six** significant digits, so a
// printed float was routinely a different number from the one the program held —
// with nothing to say so. `println(0.1 + 0.2)` printed `0.3`; `1.0 / 3.0` printed
// `0.333333`; `1234567890.0` printed `1.23457e+09`. Reading a printed value back
// is the ordinary way to move data between programs, and it was not safe to do.
//
// The formatter renders at increasing precision and `strtod`s each candidate,
// stopping at the first that comes back equal — so every case below is also an
// assertion that the string parses back to the value that produced it.
func TestExec_PrintFloatRoundTrips(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		expr    string
		wantOut string
	}{
		// The canonical demonstration that binary floats are not decimals. Printing
		// `0.3` here was the finding: two numbers that are not equal printed alike.
		{"0.1 + 0.2 is not 0.3", `0.1 + 0.2`, "0.30000000000000004\n"},
		// …while 0.3 itself still prints as 0.3, which is what makes the pair
		// meaningful: the printer is not simply verbose.
		{"0.3 prints short", `0.3`, "0.3\n"},
		{"0.1 prints short", `0.1`, "0.1\n"},
		{"one third", `1.0 / 3.0`, "0.3333333333333333\n"},
		{"pi to full precision", `3.14159265358979`, "3.14159265358979\n"},
		// %g with six digits switched to exponent form at a million; a value that
		// fits its digits now prints as itself.
		{"large integral value stays fixed", `1234567890.0`, "1234567890\n"},
		{"exponent form when genuinely large", `1.0e300`, "1e+300\n"},
		{"exponent form when genuinely small", `1.0e-300`, "1e-300\n"},
		{"zero", `0.0`, "0\n"},
		{"whole number", `100.0`, "100\n"},
		{"negative", `0.0 - 2.5`, "-2.5\n"},
		// 9007199254740993 is the first odd integer a double cannot hold; the value
		// really is …992, and printing it says so instead of rounding to 9.0072e+15.
		{"beyond 2^53", `9007199254740993.0`, "9007199254740992\n"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			out, code := buildAndRunCapture(t, "let main = () -> void => println("+c.expr+")")
			if out != c.wantOut {
				t.Errorf("stdout = %q; want %q", out, c.wantOut)
			}
			if code != 0 {
				t.Errorf("exit = %d; want 0", code)
			}
		})
	}
}

// The round-trip check is made at the value's **own** width, not at the double
// varargs promote it to. 0.1f32 widened to a double is 0.10000000149011612, so a
// check performed as a double would reject `0.1` and print all of that; narrowing
// back to float first is what keeps a narrow float's output narrow.
func TestExec_PrintFloatNarrowWidths(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		src     string
		wantOut string
	}{
		{"f32 literal", `let main = () -> void => {
			   let x: f32 = 0.1
			   println(x)
			 }`, "0.1\n"},
		{"f32 through a call", `let id = (x: f32) -> f32 => x
			 let main = () -> void => println(id(0.1))`, "0.1\n"},
		// An f32 sum that is genuinely inexact prints its own digits rather than a
		// double's view of them.
		{"f32 arithmetic", `let main = () -> void => {
			   let x: f32 = 0.1
			   println(x + x)
			 }`, "0.2\n"},
		{"f16 literal", `let main = () -> void => {
			   let x: f16 = 0.1
			   println(x)
			 }`, "0.1\n"},
		{"f16 exact value", `let main = () -> void => {
			   let x: f16 = 1.5
			   println(x)
			 }`, "1.5\n"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			out, code := buildAndRunCapture(t, c.src)
			if out != c.wantOut {
				t.Errorf("stdout = %q; want %q", out, c.wantOut)
			}
			if code != 0 {
				t.Errorf("exit = %d; want 0", code)
			}
		})
	}
}

// A narrow float constant is **rounded** to its type, not truncated (floatConst).
// llir stores the float64 and truncates the mantissa when it emits, so
// `let x: f32 = 0.1` emitted `float 0x3FB9999980000000` — one ULP below 0.1f32
// (`0x3FB99999A0000000`), meaning the program held a number its source did not
// name. Rounding in Go first makes the value exactly representable, after which
// llir's truncation has nothing to remove.
//
// This shipped, and was invisible while printing was lossy: at six significant
// digits the wrong constant and the right one both printed `0.1`. It is the
// argument for round-tripping output in general — a lossy printer does not merely
// lose detail, it conceals other faults.
func TestEmit_NarrowFloatConstantIsRoundedNotTruncated(t *testing.T) {
	t.Parallel()
	ir, err := emitSource(t, `let main = () -> void => {
		  let x: f32 = 0.1
		  println(x)
		}`)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(ir, "0x3FB9999980000000") {
		t.Errorf("f32 0.1 emitted as the truncated constant (one ULP low):\n%s", ir)
	}
	if !strings.Contains(ir, "0x3FB99999A0000000") {
		t.Errorf("expected f32 0.1 to emit as the correctly rounded 0x3FB99999A0000000:\n%s", ir)
	}
}

// NOTE: `let x: f32 = 0.1; x == 0.1` cannot be tested here because it does not
// build — the literal in a comparison is not narrowed to the operand's width, so
// the backend emits `fcmp oeq float %1, 0x3FB999999999999A` (a double constant in
// a float compare) and clang rejects the module. That is a separate, pre-existing
// literal-propagation gap, tracked in todo.md; it predates the rounding fix above
// and is unaffected by it, since the constant is emitted at double where
// floatConst is the identity. It fails loudly rather than silently, which is why
// it is recorded rather than worked around here.
