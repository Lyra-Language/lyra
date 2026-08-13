package llvm

import (
	"strings"
	"testing"
)

// A float literal compared against a narrow float takes that operand's width
// (08/13). These are exec tests because the failure was not a wrong answer but a
// **build failure**: the literal stayed at the f64 default, the backend emitted
// `fcmp oeq float %1, <double constant>`, and clang rejected the module with
// "floating point constant invalid for type".
//
// The cause was an `else if`. The float-imprecision warning (lyra-W008) sat where
// the width propagation belonged, so the operators it warned about were exactly the
// ones that never propagated — a warning about precision that stopped the program
// compiling. The relational operators were unaffected, their branch propagating
// unconditionally, which is why `x < 0.1` always worked and `x == 0.1` never did.
func TestExec_FloatLiteralComparisonNarrowsToOperandWidth(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		src     string
		wantOut string
	}{
		// The literal becomes an f32, so it names the same value the binding holds.
		{"f32 equality", `let main = () -> void => {
			   let x: f32 = 0.1
			   if x == 0.1 { println("eq") } else { println("ne") }
			 }`, "eq\n"},
		{"f32 inequality", `let main = () -> void => {
			   let x: f32 = 0.1
			   if x != 0.2 { println("differs") } else { println("same") }
			 }`, "differs\n"},
		{"f16 equality", `let main = () -> void => {
			   let x: f16 = 0.5
			   if x == 0.5 { println("eq") } else { println("ne") }
			 }`, "eq\n"},
		// A value that is genuinely not the literal still compares false — the fix is
		// about width, not about making everything equal.
		{"f32 unequal values", `let main = () -> void => {
			   let x: f32 = 0.25
			   if x == 0.5 { println("eq") } else { println("ne") }
			 }`, "ne\n"},
		// The relational operators, which always worked, kept beside them.
		{"f32 relational", `let main = () -> void => {
			   let x: f32 = 0.5
			   if x < 0.6 { println("lt") } else { println("ge") }
			 }`, "lt\n"},
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

// The constant is emitted at the operand's width, and — through floatConst — at the
// correctly rounded value for it. Both of the day's float fixes meet here: 0.1 as an
// f32 is 0x3FB99999A0000000, where the double is 0x3FB999999999999A (the wrong type)
// and the truncated f32 would be 0x3FB9999980000000 (the wrong value).
func TestEmit_FloatLiteralComparisonUsesOperandWidth(t *testing.T) {
	t.Parallel()
	ir, err := emitSource(t, `let main = () -> void => {
		  let x: f32 = 0.1
		  if x == 0.1 { println("eq") }
		}`)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(ir, "0x3FB999999999999A") {
		t.Errorf("comparison constant emitted at double width:\n%s", ir)
	}
	if !strings.Contains(ir, "fcmp oeq float") {
		t.Errorf("expected an f32 comparison:\n%s", ir)
	}
	if !strings.Contains(ir, "0x3FB99999A0000000") {
		t.Errorf("expected the correctly rounded f32 constant:\n%s", ir)
	}
}
