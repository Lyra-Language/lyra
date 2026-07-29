package llvm

import (
	"strings"
	"testing"
)

// A non-exhaustive `match` that falls through must trap like every other runtime
// fault in the language — message on stderr, exit 101 — not run off a bare
// `unreachable`. Exhaustiveness is only a *warning* for int/string/rune/float/
// array/tuple/struct scrutinees and warnings never gate a build, so this edge is
// genuinely reachable in a program that compiles clean; it used to be undefined
// behavior (SIGTRAP/133 at -O0, arbitrary under optimization).

// TestExec_MatchFallthrough_Traps covers the scalar ladder: a matched value still
// returns normally, an unmatched one traps.
func TestExec_MatchFallthrough_Traps(t *testing.T) {
	t.Parallel()
	src := `let classify = (x: i64) -> i64 => match x { 1 => 10, 2 => 20 }
	let main = () -> u8 => {
	  print(classify(1))
	  println("")
	  print(classify(3))
	  println("")
	  0
	}`
	out, code := buildAndRunCapture(t, src)
	if code != trapExitCode {
		t.Errorf("exited %d; want %d (the match trap)\noutput:\n%s", code, trapExitCode, out)
	}
	if out != "10\n" {
		t.Errorf("stdout = %q; want %q (the matched arm ran, then the trap fired)", out, "10\n")
	}
}

// A fully-guarded match has no unguarded arm to seal the ladder, so the
// fall-through is reached whenever every guard fails — the deterministic case.
func TestExec_MatchFallthrough_AllGuardsFail_Traps(t *testing.T) {
	t.Parallel()
	src := `let main = () -> u8 => {
	  let g = match 5 { x if x > 100 => 1, y if y > 200 => 2 }
	  print(g)
	  0
	}`
	if _, code := buildAndRunCapture(t, src); code != trapExitCode {
		t.Errorf("exited %d; want %d (the match trap)", code, trapExitCode)
	}
}

// The other ladders: string/rune/float scalars share lowerScalarMatch, arrays
// have their own (match_array.go), and tuples/structs a third
// (lowerAggregateMatch). Each must seal its fall-through with the same trap.
func TestExec_MatchFallthrough_Traps_AllScrutineeKinds(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		src  string
	}{
		{"string", `let f = (s: string) -> i64 => match s { "a" => 1, "b" => 2 }
		let main = () -> u8 => { print(f("z")) 0 }`},
		{"rune", `let f = (c: rune) -> i64 => match c { 'x' => 1 }
		let main = () -> u8 => { print(f('q')) 0 }`},
		{"float", `let f = (x: f64) -> i64 => match x { 1.5 => 1 }
		let main = () -> u8 => { print(f(9.5)) 0 }`},
		{"dynamic array", `let f = (xs: []i64) -> i64 => match xs { [1] => 1, [2, 3] => 2 }
		let main = () -> u8 => { print(f([9, 9])) 0 }`},
		{"tuple", `let f = (p: (i64, i64)) -> i64 => match p { (0, b) => b, (1, b) => b }
		let main = () -> u8 => { print(f((7, 8))) 0 }`},
		{"struct", `struct Pt { x: i64, y: i64 }
		let f = (p: Pt) -> i64 => match p { { x: 0 } => 1 }
		let main = () -> u8 => { print(f(Pt { x: 5, y: 6 })) 0 }`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			out, code := buildAndRunCapture(t, c.src)
			if code != trapExitCode {
				t.Errorf("exited %d; want %d (the match trap)\noutput:\n%s", code, trapExitCode, out)
			}
		})
	}
}

// An exhaustive match must not pay for the trap: no panic function, no call.
func TestEmit_ExhaustiveMatch_NoTrap(t *testing.T) {
	t.Parallel()
	src := `let main = () -> u8 => {
	  let v = match 3 { 1 => 10, _ => 20 }
	  u8(v)
	}`
	out, err := emitSource(t, src)
	if err != nil {
		t.Fatalf("emit: %v", err)
	}
	if strings.Contains(out, "lyra_panic_match_failed") {
		t.Errorf("exhaustive match emitted the match trap:\n%s", out)
	}
}

// The trap replaces the bare `unreachable` on the fall-through edge: the panic
// function is defined and called. (A trap block is still `call` + `unreachable`,
// which is what makes it noreturn — so assert on the call, not the absence of
// the keyword.)
func TestEmit_NonExhaustiveMatch_EmitsTrapCall(t *testing.T) {
	t.Parallel()
	src := `let main = () -> u8 => {
	  let v = match 3 { 1 => 10, 2 => 20 }
	  u8(v)
	}`
	out, err := emitSource(t, src)
	if err != nil {
		t.Fatalf("emit: %v", err)
	}
	for _, want := range []string{
		"define void @lyra_panic_match_failed()",
		"call void @lyra_panic_match_failed()",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q in IR, got:\n%s", want, out)
		}
	}
}

// An irrefutable destructuring arm covers a single-shape aggregate completely,
// so the match is exhaustive: it runs normally and emits no trap. (The
// typechecker no longer warns on these either — see the tuple/struct
// exhaustiveness tests.)
func TestExec_IrrefutableAggregateMatch_NoTrap(t *testing.T) {
	t.Parallel()
	src := `struct Pt { x: i64, y: i64 }
	let main = () -> u8 => {
	  let s = match (3, 4) { (a, b) => a + b }
	  print(s)
	  println("")
	  let q = match Pt { x: 1, y: 2 } { { x, y } => x + y }
	  print(q)
	  println("")
	  let n = match ((1, 2), 3) { ((a, b), c) => a + b + c }
	  print(n)
	  0
	}`
	out, code := buildAndRunCapture(t, src)
	if code != 0 {
		t.Fatalf("exited %d; want 0\noutput:\n%s", code, out)
	}
	if out != "7\n3\n6" {
		t.Errorf("stdout = %q; want %q", out, "7\n3\n6")
	}
}
