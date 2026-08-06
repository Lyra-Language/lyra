package llvm

import (
	"strings"
	"testing"
)

// Logical not (`!`), lowered in arithmetic.go as `xor i1 x, true`.
//
// It type-checked long before it lowered, so `!x` reached the backend as a hard
// error ("expression lowering not implemented for *ast.NotBooleanExpr") and no
// program using it could be built at all. That is also why the precedence bug the
// second test pins went unnoticed for so long — see TestExec_NotBindsTighterThanAnd.

func TestExec_Not(t *testing.T) {
	t.Parallel()
	cases := []struct {
		src  string
		want int
	}{
		{"let main = () -> u8 => if !true { 1 } else { 0 }\n", 0},
		{"let main = () -> u8 => if !false { 1 } else { 0 }\n", 1},
		// Double negation is the identity.
		{"let main = () -> u8 => if !!true { 1 } else { 0 }\n", 1},
		// `!` over a comparison needs parens (its operand is postfix-width), and
		// the parenthesized form must still say what it looks like.
		{"let main = () -> u8 => if !(1 < 2) { 1 } else { 0 }\n", 0},
		// Not of a bool-valued binding, the shape `parse`-style code uses.
		{"let main = () -> u8 => {\n  let ok = false;\n  if !ok { 7 } else { 0 }\n}\n", 7},
	}
	for _, c := range cases {
		t.Run("", func(t *testing.T) {
			t.Parallel()
			if got := buildAndRun(t, c.src); got != c.want {
				t.Errorf("%q exited %d; want %d", c.src, got, c.want)
			}
		})
	}
}

// `!a && b` must group as `(!a) && b`, not `!(a && b)`.
//
// It grouped the wrong way until 08/05: `!`'s operand was `$.expression`, so it
// absorbed the `&&` — the opposite of every C-family language, and silent, since
// both readings are well-typed `bool`. The fix narrowed the operand to a postfix
// expression (tree-sitter-lyra `_not_operand`); PREC.UNARY on the rule could not
// do it, because a precedence does not stop a wider operand rule from absorbing
// more.
//
// This is checked by *observable side effect* rather than by the result, because
// the two readings often agree on the value and differ only in whether the right
// operand is evaluated. Here `a` is true, so:
//
//	(!a) && side()  →  false && …  →  side() never runs
//	!(a && side())  →  !(true)     →  side() runs, and the `if` still takes `else`
//
// Both readings take the same branch, so a test on the *result* passes either
// way. Whether `side` printed is the only thing that separates them.
func TestExec_NotBindsTighterThanAnd(t *testing.T) {
	t.Parallel()
	const src = `
let side = () -> bool => { println("RAN"); true }
let main = () -> void => {
  let a = true;
  if !a && side() { println("then") } else { println("else") }
}
`
	out, _ := buildAndRunCapture(t, src)
	if strings.Contains(out, "RAN") {
		t.Errorf("side() was evaluated; `!a && side()` grouped as `!(a && side())`. output: %q", out)
	}
}

// The mirror for `||`: `!a || side()` with `a` false is `(!a) || …`, which
// short-circuits true and never evaluates the right operand.
func TestExec_NotBindsTighterThanOr(t *testing.T) {
	t.Parallel()
	const src = `
let side = () -> bool => { println("RAN"); true }
let main = () -> void => {
  let a = false;
  if !a || side() { println("then") } else { println("else") }
}
`
	out, _ := buildAndRunCapture(t, src)
	if strings.Contains(out, "RAN") {
		t.Errorf("side() was evaluated; `!a || side()` grouped as `!(a || side())`. output: %q", out)
	}
}
