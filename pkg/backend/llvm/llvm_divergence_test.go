package llvm

import (
	"strings"
	"testing"

	"github.com/Lyra-Language/lyra/pkg/ast"
)

// A diverging expression in operand position, across every aggregate the backend builds.
//
// `panic(msg)` has type `never`, so it is assignable everywhere and the typechecker accepts
// it as a struct field's value, an array element, a call argument. Lowering it terminates
// the current block and returns a **nil** value — the divergence protocol `diverged(v, block)`
// tests for — and every consumer must stop rather than build an instruction around the nil.
//
// Nine of them did not. The symptom is the reason this is a test rather than a comment: llir
// accepts a nil operand when the instruction is built and dies at *module serialization*, so
// the stack trace points at `m.String()` — one frame, in the emitter, naming neither the
// expression nor the pass at fault. Four sibling sites already had the guard, each added when
// a bug report happened to land on it; the nine without it were never reached by one.
//
// One sibling guard is deliberately untested here. A comprehension's result expression
// (`emitCompBody`) takes the same nil, but no program can reach it today: `[n in xs | panic("x")]`
// infers as `[]never`, and `[]never` is not assignable to `[]i64` — assignability lifts `never`
// at the scalar but not through a container — so the front end refuses it before lowering.
// The guard stays because the reachability is the *front end's* current answer rather than a
// property of the backend, and it costs two lines.
//
// The test asserts both halves: the compiler does not crash, and the program panics rather
// than being silently miscompiled into something that runs.
func TestExec_DivergingOperandInEveryAggregatePosition(t *testing.T) {
	t.Parallel()
	for _, c := range []struct{ name, src string }{
		{"struct field", `struct N { v: i64 }
let main = () -> void => {
  let n = N { v: panic("x") }
  println(n.v)
}`},
		{"data payload", `data D = Wrap(i64) | Nil
let main = () -> void => {
  let d: D = Wrap(panic("x"))
  println(match d { Wrap(n) => n, Nil => 0 })
}`},
		{"dynamic array element", `let main = () -> void => {
  var xs: []i64 = [panic("x"), 2]
  println(xs[0])
}`},
		// Already guarded before this change — one of the four siblings a bug report had
		// landed on. Kept so the fixed-size path is covered too: it is the same loop as the
		// dynamic one a few lines up, and nothing but a test keeps the two in step.
		{"fixed array element", `let main = () -> void => {
  var xs: [2]i64 = [panic("x"), 2]
  println(xs[0])
}`},
		{"push argument", `let main = () -> void => {
  var xs: []i64 = [1]
  xs.push(panic("x"))
  println(xs[0])
}`},
		{"assignment value", `struct N { v: i64 }
let main = () -> void => {
  var n = N { v: 1 }
  n.v = panic("x")
  println(n.v)
}`},
		{"call argument", `let g = pure (a: i64, b: i64) -> i64 => a + b
let main = () -> void => println(g(panic("x"), 2))`},
		// A by-reference parameter takes the *address* path (argumentAddress), which is a
		// second consumer of the same nil and was guarded separately.
		{"by-ref argument", `let f = pure (xs: ref [3]i64) -> i64 => xs[0]
let main = () -> void => println(f(panic("x")))`},
		// A trait method's operands go through methodOperand, a third path — and the one
		// whose crash surfaced only at serialization even after the aggregate sites were fixed.
		{"trait method argument", `struct B { n: i64 }
trait T { pure take: (Self, i64) -> i64 }
impl T for B { take = pure (self, k) => k }
let main = () -> void => {
  let b = B { n: 1 }
  println(b.take(panic("x")))
}`},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			stderr, code := buildAndRunPanic(t, c.src)
			if code != 101 || !strings.Contains(stderr, "panic: x") {
				t.Errorf("exit %d, stderr %q; want exit 101 and a \"panic: x\" — "+
					"a diverging operand must reach the panic rather than being lowered around",
					code, stderr)
			}
		})
	}
}

// The backstop under the nine guards: every aggregate element funnels through
// coerceAggregateElem, so a site that gains an unguarded lowerExpr later fails there with a
// message naming the expression and its position, rather than at `m.String()` with neither.
//
// It cannot be provoked from Lyra source while the guards hold — which is the point — so it
// is asserted directly.
func TestCoerceAggregateElem_NilValueIsALoudError(t *testing.T) {
	t.Parallel()
	l := &lowerer{}
	src := &ast.IdentifierExpr{Name: "elem"}
	_, err := l.coerceAggregateElem(nil, nil, nil, src)
	if err == nil {
		t.Fatal("a nil element value returned no error; it must be rule 5's loud error, " +
			"since the alternative is a nil deref one line down")
	}
	// err.Error() formats the message, location included, so a panic in the location
	// rendering fails here rather than needing its own call.
	if !strings.Contains(err.Error(), "diverged") {
		t.Errorf("error %q does not mention divergence, which is the only way v is nil", err)
	}
}
