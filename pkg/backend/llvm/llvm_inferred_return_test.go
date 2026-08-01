package llvm

import "testing"

// A function written without `-> T` now lowers: the typechecker fills the return type in
// from the body, so the backend sees an ordinary annotated function. These are the exec
// cases that prove the inferred type is the *right* one — the front-end tests can only
// say inference produced something.
func TestExec_InferredReturnTypes(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		src  string
		want int
	}{
		{
			"single-expression body",
			`let sum = ((a, b): (i64, i64)) => a + b
			 let main = () -> u8 => u8(sum((3, 4)))`,
			7,
		},
		{
			"block body",
			`let double = (n: i64) => {
			   let d = n * 2
			   d
			 }
			 let main = () -> u8 => u8(double(4))`,
			8,
		},
		{
			// The inferred type has to be the concrete one, not the provisional
			// "integer literal": an i64 return truncated at the u8 call site is the
			// observable difference.
			"untyped literal becomes i64",
			`let big = () => 300
			 let main = () -> u8 => u8(big() - 44)`,
			0, // 300 - 44 = 256, truncated to u8 → 0; an i64 return is what makes this reachable
		},
		{
			"struct return",
			`struct Pt { x: i64 }
			 let mk = (n: i64) => Pt { x: n }
			 let main = () -> u8 => u8(mk(7).x)`,
			7,
		},
		{
			// Recursion resolves when a non-recursive branch fixes the type.
			"recursion through a non-recursive branch",
			`let fact = (n: i64) => if n == 0 { 1 } else { n * fact(n - 1) }
			 let main = () -> u8 => u8(fact(5))`,
			120,
		},
		{
			"mutual recursion",
			`let isEven = (n: i64) => if n == 0 { 1 } else { isOdd(n - 1) }
			 let isOdd = (n: i64) => if n == 0 { 0 } else { isEven(n - 1) }
			 let main = () -> u8 => u8(isEven(4))`,
			1,
		},
		{
			// An inferred function passed as a value: the filled-in signature is what
			// the parameter's function type is checked against, and what the closure
			// lowers with.
			"inferred function used as a value",
			`let apply = (g: (i64) -> i64, n: i64) -> i64 => g(n)
			 let double = (n: i64) => n * 2
			 let main = () -> u8 => u8(apply(double, 21))`,
			42,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if got := buildAndRun(t, c.src); got != c.want {
				t.Errorf("%s: exited %d; want %d", c.name, got, c.want)
			}
		})
	}
}

// TestExec_EntryPointIgnoresAnInferredReturn: `let main = () => { 0 }` is a documented
// spelling of a *void* entry point and must stay one.
//
// Inference would otherwise type it from the trailing `0` and the entry-point check would
// reject it — "must return u8 or void, got i64" — breaking a program that compiled before
// a feature unrelated to it. Only a written annotation decides, so this exits 0 (the value
// is discarded) while `-> u8` still means an exit code. An inferred *u8* is honored,
// since there the inferred type and the convention agree.
func TestExec_EntryPointIgnoresAnInferredReturn(t *testing.T) {
	t.Parallel()
	if got := buildAndRun(t, "let main = () => {\n  0\n}\n"); got != 0 {
		t.Errorf("an unannotated main is void: exited %d; want 0", got)
	}
	if got := buildAndRun(t, "let main = () => u8(7)\n"); got != 7 {
		t.Errorf("an inferred u8 main is an exit code: exited %d; want 7", got)
	}
}
