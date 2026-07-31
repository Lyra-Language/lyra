package llvm

import "testing"

// Multi-clause functions.
//
//	let fib = (n: i64, a: i64, b: i64) -> i64 {
//	  (0, a, _) => a,
//	  (n, a, b) => fib(n - 1, b, a + b),
//	}
//
// The grammar, collector and typechecker always accepted these; only the backend refused
// ("multi-clause functions are not implemented yet"). They are now desugared in the front end
// into a match on the parameters, so what is tested here is that the desugaring produces a
// program that *runs* — the lowering itself is the match lowering, already covered elsewhere.

// The motivating shape: several clauses over several parameters, with a literal pattern
// selecting the base case, and recursion.
func TestExec_MultiClauseFunction(t *testing.T) {
	t.Parallel()
	got := buildAndRun(t, `
let fib = (n: i64, a: i64 = 0, b: i64 = 1) -> i64 {
  (0, a, _) => a,
  (n, a, b) => fib(n - 1, b, a + b),
}
let main = () -> u8 => u8(fib(10))`)
	if got != 55 {
		t.Errorf("exit = %d, want 55", got)
	}
}

// A one-parameter function matches its parameter *directly* rather than through a
// one-element tuple, so it reaches the scalar ladder. Guards work because a clause guard
// becomes an arm guard.
func TestExec_MultiClauseSingleParameterAndGuard(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		arg  string
		want int
	}{
		{"literal clause", "0", 1},
		{"guarded clause", "-5", 2},
		{"catch-all", "7", 3},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			got := buildAndRun(t, `
let classify = (n: i64) -> i64 {
  (0) => 1,
  (n) if n < 0 => 2,
  (_) => 3,
}
let main = () -> u8 => u8(classify(`+c.arg+`))`)
			if got != c.want {
				t.Errorf("exit = %d, want %d", got, c.want)
			}
		})
	}
}

// Data patterns across clauses — the form that reads best as multiple clauses.
func TestExec_MultiClauseDataPatterns(t *testing.T) {
	t.Parallel()
	got := buildAndRun(t, `
data Shape = Circle(i64) | Square(i64)
let area = (s: Shape) -> i64 {
  (Circle(r)) => r * r * 3,
  (Square(w)) => w * w,
}
let main = () -> u8 => u8(area(Circle(2)) + area(Square(3)))`)
	if got != 21 {
		t.Errorf("exit = %d, want 21", got)
	}
}

// A **generic** multi-clause function. The backend used to refuse these twice over — once
// for being multi-clause, and once in declareSpecialization ("a multi-clause generic function
// is not implemented yet"). Desugaring removes both, since the body is then ordinary.
func TestExec_MultiClauseGenericFunction(t *testing.T) {
	t.Parallel()
	got := buildAndRun(t, `
data Opt<t> = Nil | Just(t)
let unwrap<t> = (m: Opt<t>, fallback: t) -> t {
  (Just(v), _) => v,
  (Nil, d) => d,
}
let main = () -> u8 => {
  let a: Opt<i64> = Just(5)
  let b: Opt<i64> = Nil
  u8(unwrap(a, 0) + unwrap(b, 2))
}`)
	if got != 7 {
		t.Errorf("exit = %d, want 7", got)
	}
}

// No clause matching is a **trap**, not undefined behaviour: the desugared match's
// fall-through is sealed by sealMatchFallthrough, so a function-clause error exits 101 with a
// message, the same as any other non-exhaustive match. That is the behaviour Erlang and
// Elixir have for the same construct.
func TestExec_MultiClauseNoMatchTraps(t *testing.T) {
	t.Parallel()
	got := buildAndRun(t, `
let only = (n: i64) -> i64 {
  (0) => 1,
}
let main = () -> u8 => u8(only(9))`)
	if got != trapExitCode {
		t.Errorf("exit = %d, want %d (a trap, not undefined behaviour)", got, trapExitCode)
	}
}

// A data pattern nested inside an aggregate pattern, where the element type is a *generic*
// instantiation. This failed with "data pattern on non-data value of type Opt<i64>" — the
// top-level match path normalizes its scrutinee, but the sub-pattern path read the element
// type straight off the tuple, where it is still parameterized. Pre-existing and independent
// of clauses: the hand-written match fails the same way, which is why the test is written
// that way round.
func TestExec_GenericDataPatternInsideATupleMatch(t *testing.T) {
	t.Parallel()
	got := buildAndRun(t, `
data Opt<t> = Nil | Just(t)
let unwrap<t> = (m: Opt<t>, fallback: t) -> t => match (m, fallback) {
  (Just(v), _) => v,
  (Nil, d) => d,
}
let main = () -> u8 => {
  let a: Opt<i64> = Just(5)
  u8(unwrap(a, 0))
}`)
	if got != 5 {
		t.Errorf("exit = %d, want 5", got)
	}
}
