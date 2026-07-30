package llvm

import (
	"strings"
	"testing"
)

// Generic functions, end to end: the typechecker solves each call's type variables
// from its arguments, and the backend emits one specialized function per distinct
// binding set — monomorphization by *substitution*, lowering the one shared body
// with the instantiation's bindings installed rather than cloning the AST.
//
// Before this, a generic function did not even type-check: `identity(7)` reported
// "cannot assign integer literal to t", because nothing solved `t`.
func TestExec_GenericFunctions(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		src  string
		want int
	}{
		{
			"identity at one type",
			`let identity = (x: t) -> t => x
			 let main = () -> u8 => u8(identity(7))`,
			7,
		},
		{
			// Two instantiations of one function, each emitted separately.
			"identity at two types",
			`let identity = (x: t) -> t => x
			 let main = () -> u8 => {
			   let a = identity(7)
			   let b = identity(true)
			   if b { u8(a) } else { 0 }
			 }`,
			7,
		},
		{
			// A type variable under a composite type: solved from the argument's
			// element type.
			"a type variable inside an array type",
			`let first = (xs: [3]t) -> t => xs[0]
			 let main = () -> u8 => u8(first([7, 8, 9]))`,
			7,
		},
		{
			// Two variables in one signature, solved independently.
			"two type variables",
			`let takeFirst = (a: t, b: u) -> t => a
			 let main = () -> u8 => u8(takeFirst(7, true))`,
			7,
		},
		{
			// The same variable in several positions: both arguments must agree, and
			// the body works at whichever type they agree on.
			"one variable in several positions",
			`let addBoth = (a: t, b: t) -> t => a
			 let main = () -> u8 => u8(addBoth(3, 4))`,
			3,
		},
		{
			// The body does real work at the instantiated type — a local of the
			// variable's type, which is what proves the substitution reaches the body's
			// own storage and not just the signature.
			"a local of the variable's type in the body",
			`let identity = (x: t) -> t => {
			   let copy: t = x
			   copy
			 }
			 let main = () -> u8 => u8(identity(7))`,
			7,
		},
		{
			// A generic function called from another (non-generic) function.
			"called from another function",
			`let identity = (x: t) -> t => x
			 let twice = (n: i64) -> i64 => identity(n) + identity(n)
			 let main = () -> u8 => u8(twice(3))`,
			6,
		},
		{
			// Instantiated at a narrower width via an explicit conversion at the call
			// site: an untyped literal argument settles to its default (i64), so `u8`
			// is reached by saying so.
			"instantiated at a narrow width",
			`let identity = (x: t) -> t => x
			 let main = () -> u8 => identity(u8(7))`,
			7,
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

// One emitted function per distinct instantiation, named after its bindings — and
// *no* function under the bare generic name, which has no representation to emit.
func TestEmit_GenericSpecializations(t *testing.T) {
	t.Parallel()
	got, err := emitSource(t, `let identity = (x: t) -> t => x
	 let main = () -> u8 => {
	   let a = identity(7)
	   let b = identity(true)
	   if b { u8(a) } else { 0 }
	 }`)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"define i64 @identity$i64(i64 ",
		"define i1 @identity$boolean(i1 ",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("expected IR to contain %q; got:\n%s", want, got)
		}
	}
	// The generic name itself is never defined: a type variable has no width, so
	// there is nothing to emit under it.
	if strings.Contains(got, "define i64 @identity(") || strings.Contains(got, "@identity(") {
		t.Errorf("the bare generic name should not be emitted:\n%s", got)
	}
	// Two call sites that solve to the same bindings share one specialization.
	same, err := emitSource(t, `let identity = (x: t) -> t => x
	 let main = () -> u8 => u8(identity(1) + identity(2))`)
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(same, "define i64 @identity$i64("); n != 1 {
		t.Errorf("expected 1 shared specialization, got %d:\n%s", n, same)
	}
}

// An unused generic function costs nothing: with no instantiation there is nothing
// to specialize, and the generic form is never emitted.
func TestEmit_UnusedGenericEmitsNothing(t *testing.T) {
	t.Parallel()
	got, err := emitSource(t, `let unused = (x: t) -> t => x
	 let main = () -> u8 => 0`)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(got, "unused") {
		t.Errorf("an uninstantiated generic function should emit nothing:\n%s", got)
	}
}

// A **managed** type argument is refused rather than miscompiled.
//
// This is the substitution approach's real limit. The ownership pass analyzes the
// generic body once, where a type variable is not managed, so it records no retain,
// release, or drop anywhere in it — and at `t = string` those decisions are wrong.
// Measured before the refusal went in: an ASan abort, with 2 allocations against 3
// releases. Running the ownership pass per instantiation is the fix and the next
// slice; until then the build fails loudly.
func TestEmit_GenericAtManagedTypeRefused(t *testing.T) {
	t.Parallel()
	_, err := emitSource(t, `let pick = (a: t, b: t, useFirst: bool) -> t => if useFirst { a } else { b }
	 let main = () -> u8 => {
	   let s = pick("a" ++ "b", "c" ++ "d", true)
	   if s == "ab" { 7 } else { 1 }
	 }`)
	if err == nil {
		t.Fatal("expected a generic instantiated at a managed type to be refused")
	}
	if !strings.Contains(err.Error(), "reference-counted type") {
		t.Errorf("expected the error to name the reason; got: %v", err)
	}
}
