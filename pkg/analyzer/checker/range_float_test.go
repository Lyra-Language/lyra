package checker_test

import (
	"testing"

	"github.com/Lyra-Language/lyra/pkg/ast"
)

func isRoundingCall(e ast.Expression) bool {
	call, ok := e.(*ast.FunctionCallExpr)
	if !ok {
		return false
	}
	m, ok := call.Function.(*ast.MemberExpr)
	return ok && (m.Property.Name == "floor" || m.Property.Name == "ceil" || m.Property.Name == "round")
}

// **A rounding call whose receiver is provably finite and well inside i64's range drops its
// float→int guard** (09/13); every shape that cannot be proven keeps it.
func TestRange_Safety_FloatToIntTrap(t *testing.T) {
	for _, c := range []struct {
		name, src string
		safe      bool
	}{
		{"a bounded counter scaled", `let f = (i: u8) -> i64 => (f64(i) / 10.0).floor()`, true},
		{"through a float binding", `let f = (i: u16) -> i64 => { let t = f64(i) * 0.5 + 1.0
  t.ceil() }`, true},
		{"through an f32", `let f = (i: u16) -> i64 => { let u: f32 = f32(f64(i) * 0.25)
  u.round() }`, true},
		{"a literal", `let f = () -> i64 => (2.5).round()`, true},
		{"an unbounded parameter", `let f = (x: f64) -> i64 => x.floor()`, false},
		{"a divisor that may be zero", `let f = (i: u8, d: f64) -> i64 => (f64(i) / d).floor()`, false},
		{"an i64 scaled past the margin", `let f = (i: i64) -> i64 => (f64(i) * 4.0).floor()`, false},
		{"a u64, whose top is unbounded", `let f = (i: u64) -> i64 => f64(i).floor()`, false},
		{"a reassigned float", `let f = (i: u8, x: f64) -> i64 => { var t = f64(i)
  t = x
  t.floor() }`, false},
		{"a float modified through a pointer", `let f = (i: u8, x: f64) -> i64 => { var t = f64(i)
  unsafe { let p = &mut t
    p^ = x }
  t.floor() }`, false},
		{"a match arm rebinding the name", `data Opt = Has(f64) | Gone
let f = (m: Opt) -> i64 => { let t = 1.0
  match m { Has(t) => t.floor(), Gone => 0 } }`, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			program, safety := analyzeForSafety(t, c.src+"\nlet main = () -> u8 => 0")
			call := firstExpr(t, program, isRoundingCall)
			if got := safety.NoFloatToIntTrap(call); got != c.safe {
				t.Errorf("NoFloatToIntTrap = %v; want %v", got, c.safe)
			}
		})
	}
}

// **The integer a rounding produces is tracked**, so the checks after it can drop too:
// `xs[(f64(i) / 10.0).floor()]` for a u8 `i` indexes within [0, 25].
func TestRange_Safety_RoundingResultIsTracked(t *testing.T) {
	program, safety := analyzeForSafety(t, `
let f = (xs: [26]u8, i: u8) -> u8 => xs[(f64(i) / 10.0).floor()]
let main = () -> u8 => 0`)
	if !safety.IndexInBounds(firstExpr(t, program, isIndexExpr)) {
		t.Error("a rounded bounded value should prove the index in bounds")
	}
}

// **A name a match arm or a comprehension binds is not the outer binding it shadows.** Both
// inherited the outer interval until 09/13, so `let i = 0` then `Some(i) => xs[i]` — or
// `[i in big | xs[i]]` — dropped the bounds check and read past the array.
func TestRange_Safety_ShadowingBindersDoNotInherit(t *testing.T) {
	for _, c := range []struct{ name, src string }{
		{"match arm", `data Opt = Has(i64) | Gone
let f = (xs: [3]i64, m: Opt) -> i64 => { let i = 0
  match m { Has(i) => xs[i], Gone => 0 } }`},
		{"comprehension", `let f = (xs: [3]i64, big: []i64) -> []i64 => { let i = 0
  [i in big | xs[i]] }`},
	} {
		t.Run(c.name, func(t *testing.T) {
			program, safety := analyzeForSafety(t, c.src+"\nlet main = () -> u8 => 0")
			if safety.IndexInBounds(firstExpr(t, program, isIndexExpr)) {
				t.Error("the shadowing binding must not inherit the outer interval")
			}
		})
	}
	// The outer binding is still known after the arm.
	program, safety := analyzeForSafety(t, `data Opt = Has(i64) | Gone
let f = (xs: [3]i64, m: Opt) -> i64 => { let i = 0
  let a = match m { Has(i) => i, Gone => 0 }
  xs[i] + a }
let main = () -> u8 => 0`)
	if !safety.IndexInBounds(firstExpr(t, program, isIndexExpr)) {
		t.Error("the outer i is restored after the arm, so xs[i] stays provably in bounds")
	}
}
