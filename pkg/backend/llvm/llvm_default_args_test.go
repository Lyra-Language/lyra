package llvm

import (
	"strings"
	"testing"
)

// Default parameter values.
//
// The grammar, collector and arity check already understood them; what was missing is that
// the *call site* never received them, so the backend saw a call shorter than the function's
// parameter list and refused the function outright ("default parameter values are not
// implemented yet"). They are now filled in by the typechecker, so the backend needs no
// notion of defaults at all.

func TestExec_DefaultArgumentOmitted(t *testing.T) {
	t.Parallel()
	got := buildAndRun(t, `
let add = (a: i64, b: i64 = 10) -> i64 => a + b
let main = () -> u8 => u8(add(5))`)
	if got != 15 {
		t.Errorf("exit = %d, want 15", got)
	}
}

// An explicit argument still wins over the default — the fill is only for omitted trailing
// arguments, so this is the case that would break if it filled unconditionally.
func TestExec_DefaultArgumentSupplied(t *testing.T) {
	t.Parallel()
	got := buildAndRun(t, `
let add = (a: i64, b: i64 = 10) -> i64 => a + b
let main = () -> u8 => u8(add(5, 2))`)
	if got != 7 {
		t.Errorf("exit = %d, want 7", got)
	}
}

// Several defaults, filled partially: one supplied, one omitted.
func TestExec_DefaultArgumentsPartiallySupplied(t *testing.T) {
	t.Parallel()
	got := buildAndRun(t, `
let f = (a: i64, b: i64 = 2, c: i64 = 4) -> i64 => a + b + c
let main = () -> u8 => u8(f(1) + f(1, 1) + f(1, 1, 1))`)
	// (1+2+4) + (1+1+4) + (1+1+1) = 7 + 6 + 3 = 16
	if got != 16 {
		t.Errorf("exit = %d, want 16", got)
	}
}

// The default expression is a *shared* AST node, appended to every call that omits it. This
// is the case that would expose that as a problem: a heap-allocating default at two call
// sites, where a shared ownership decision would double-free or leak. Run under the same
// harness as the rest, so an ASan-instrumented build covers it.
func TestExec_ManagedDefaultAtSeveralCallSites(t *testing.T) {
	t.Parallel()
	out, code := buildAndRunCapture(t, `
let greet = (name: string, greeting: string = "hi " ++ "there ") -> string => greeting ++ name
let main = () -> u8 => {
  print(greet("a"))
  print(greet("b"))
  print(greet("c", "yo "))
  0
}`)
	if code != 0 {
		t.Fatalf("exit = %d, want 0", code)
	}
	if want := "hi there ahi there byo c"; out != want {
		t.Errorf("output = %q, want %q", out, want)
	}
}

// Defaults compose with the multi-clause desugaring: the fill happens at the call, the
// desugaring at the declaration, and neither knows about the other.
func TestExec_DefaultArgumentsWithMultiClause(t *testing.T) {
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

// A default on a *generic* function: the fill happens before type variables are solved, so
// the default participates in solving like any other argument.
func TestExec_DefaultArgumentOnAGenericFunction(t *testing.T) {
	t.Parallel()
	got := buildAndRun(t, `
let pick<t> = (a: t, b: t, first: bool = true) -> t => if first { a } else { b }
let main = () -> u8 => u8(pick(7, 9))`)
	if got != 7 {
		t.Errorf("exit = %d, want 7", got)
	}
}

// The emitted function takes every parameter — defaults are a call-site notion, so the
// signature is unchanged by them.
func TestEmit_DefaultParameterIsAnOrdinaryParameter(t *testing.T) {
	t.Parallel()
	got, err := emitSource(t, `
let add = (a: i64, b: i64 = 10) -> i64 => a + b
let main = () -> u8 => u8(add(5))`)
	if err != nil {
		t.Fatal(err)
	}
	body := funcBody(got, "add")
	if body == "" {
		t.Fatalf("no emitted add:\n%s", got)
	}
	if !strings.Contains(body, "i64 %") || strings.Count(body, "i64 %") < 2 {
		t.Errorf("add should take both parameters:\n%s", body)
	}
}
