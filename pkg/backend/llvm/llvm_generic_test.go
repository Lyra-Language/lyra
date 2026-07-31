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
		"define i64 @lyra.identity$i64(i64 ",
		"define i1 @lyra.identity$boolean(i1 ",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("expected IR to contain %q; got:\n%s", want, got)
		}
	}
	// The generic name itself is never defined: a type variable has no width, so
	// there is nothing to emit under it.
	if strings.Contains(got, "define i64 @lyra.identity(") || strings.Contains(got, "@identity(") {
		t.Errorf("the bare generic name should not be emitted:\n%s", got)
	}
	// Two call sites that solve to the same bindings share one specialization.
	same, err := emitSource(t, `let identity = (x: t) -> t => x
	 let main = () -> u8 => u8(identity(1) + identity(2))`)
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(same, "define i64 @lyra.identity$i64("); n != 1 {
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

// A **managed** type argument works, because the ownership pass runs once *per
// instantiation* rather than once on the generic body.
//
// This is the case that miscompiled before: analyzed generically, a type variable
// is not reference-counted, so the pass recorded no retain on a returned value and
// no release for the caller's temporaries — correct at `t = i64`, a double free at
// `t = string` (measured: an ASan abort, 2 allocations against 3 releases). Each
// specialization now consults the table computed under its own bindings.
func TestExec_GenericAtManagedType(t *testing.T) {
	t.Parallel()
	clang := lookClang(t)
	cases := []struct {
		name string
		src  string
		want int
	}{
		{
			// The shape that aborted: a generic returning one of two managed arguments,
			// so the result needs a retain and both temporaries need releases.
			"a generic returning one of two managed arguments",
			`let pick = (a: t, b: t, useFirst: bool) -> t => if useFirst { a } else { b }
			 let main = () -> u8 => {
			   let s = pick("a" ++ "b", "c" ++ "d", true)
			   if s == "ab" { 7 } else { 1 }
			 }`,
			7,
		},
		{
			"identity at a string",
			`let identity = (x: t) -> t => x
			 let main = () -> u8 => {
			   let s = identity("a" ++ "b")
			   if s == "ab" { 7 } else { 1 }
			 }`,
			7,
		},
		{
			// A managed *container* type argument: the element drops are the box's, and
			// the specialization must not double them.
			"identity at a dynamic array of strings",
			`let identity = (x: t) -> t => x
			 let main = () -> u8 => {
			   let xs = identity(["a" ++ "1", "b" ++ "2"])
			   let s = xs[1]
			   if s == "b2" { 7 } else { 1 }
			 }`,
			7,
		},
		{
			// One generic function instantiated at a managed type *and* a scalar in the
			// same program: the two specializations get opposite ownership decisions
			// from the same body, which is exactly what one shared table could not
			// express.
			"managed and scalar instantiations side by side",
			`let identity = (x: t) -> t => x
			 let main = () -> u8 => {
			   let a = identity("x" ++ "y")
			   let b = identity(7)
			   if a == "xy" { u8(b) } else { 0 }
			 }`,
			7,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if got := buildAndRun(t, c.src); got != c.want {
				t.Errorf("%s: exited %d; want %d", c.name, got, c.want)
			}
			if got := buildAndRunASan(t, clang, c.src); got != c.want {
				t.Errorf("%s: ASan run exited %d; want %d", c.name, got, c.want)
			}
		})
	}
}

// The scalar instantiation must stay free of refcount traffic: its table was
// computed at `t = i64`, where nothing is managed, so a retain appearing there
// would mean the managed specialization's decisions had leaked across.
func TestEmit_GenericScalarSpecializationHasNoRefcounting(t *testing.T) {
	t.Parallel()
	got, err := emitSource(t, `let identity = (x: t) -> t => x
	 let main = () -> u8 => {
	   let a = identity("x" ++ "y")
	   let b = identity(7)
	   if a == "xy" { u8(b) } else { 0 }
	 }`)
	if err != nil {
		t.Fatal(err)
	}
	body := funcBody(got, "@lyra.identity$i64")
	if body == "" {
		t.Fatalf("expected an i64 specialization:\n%s", got)
	}
	if strings.Contains(body, "lyra_rc_") {
		t.Errorf("the i64 specialization should have no refcount traffic:\n%s", body)
	}
}

// funcBody returns the text of one function definition, so an assertion can be
// about a single specialization rather than the whole module.
func funcBody(ir, name string) string {
	start := strings.Index(ir, "define ")
	for start >= 0 {
		rest := ir[start:]
		end := strings.Index(rest, "\n}")
		if end < 0 {
			return ""
		}
		body := rest[:end]
		if strings.Contains(strings.SplitN(body, "(", 2)[0], name) {
			return body
		}
		next := strings.Index(ir[start+1:], "\ndefine ")
		if next < 0 {
			return ""
		}
		start += next + 1
	}
	return ""
}

// A generic function solves its type variables from a *function-typed* argument.
//
// `unifyGenericTarget` had no LambdaType case, so `() -> t` against a supplied
// `() -> i64` bound nothing and the call reported "cannot infer type variable t from
// these arguments"; `substituteGenerics` had the same omission, so even once `t` was
// solved the declared parameter stayed `() -> t` and the argument was rejected as
// "cannot assign () -> i64 to () -> t". Both halves are needed, and between them they
// are what makes any callback-taking combinator — `unwrap_or_else`, `map`, `and_then` —
// expressible at all.
func TestExec_GenericSolvedFromFunctionArgument(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		src  string
		want int
	}{
		{
			name: "variable in the callback's return type",
			src: `let g = (h: () -> t) -> t => h()
let mk = () -> i64 => 42
let main = () -> u8 => u8(g(mk))`,
			want: 42,
		},
		{
			name: "variable in the callback's parameter type",
			src: `let apply = (f: (t) -> t, x: t) -> t => f(x)
let dbl = (n: i64) -> i64 => n * 2
let main = () -> u8 => u8(apply(dbl, 21))`,
			want: 42,
		},
		{
			// The shape the prelude's unwrap_or_else has: one aggregate argument and
			// one callback, each carrying the same variable.
			name: "aggregate and callback agree on the variable",
			src: `data Opt<t> = Nil | Just(t)
let or_else = (m: Opt<t>, f: () -> t) -> t => match m { Just(v) => v, Nil => f(), }
let fortyTwo = () -> i64 => 42
let main = () -> u8 => u8(or_else(Nil, fortyTwo))`,
			want: 42,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if got := buildAndRun(t, c.src); got != c.want {
				t.Errorf("exit = %d, want %d", got, c.want)
			}
		})
	}
}

// A function is generic when its signature mentions a type variable *anywhere*, including
// where the variable appears only inside a composite — `Maybe<t>`, or a callback's own
// signature — and never as a parameter of its own.
//
// mentionsTypeVar had no case for either, so isGenericLambda answered "not generic",
// forEachUserFunction stopped skipping the function, and the backend tried to emit it
// under its bare name — failing with "cannot lay out data type Opt yet", a message about
// the *type* for a bug about the *function*. A bare `t` parameter is what hid it: it hits
// the GenericType case directly, and every generic function written before the standard
// library happened to have one. `is_some<t> = (m: Maybe<t>) -> bool` does not, so once the
// prelude declared it no program could build at all — the prelude is implicitly imported,
// so this took down programs that never mentioned Maybe.
//
// The data cases are the ones that fail without the fix, and the uncalled one is the
// sharpest: with no call there is no instantiation, so nothing should be emitted for that
// function whatsoever. The callback cases pass either way today — a boxed closure is a
// pointer, so `() -> t` needs no layout — and are here as guards on the classification
// itself, which is what release-tier closure lowering will change out from under.
func TestExec_GenericVariableOnlyInsideAComposite(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		src  string
		want int
	}{
		{
			name: "variable only inside a data-typed parameter, never called",
			src: `data Opt<t> = Nil | Just(t)
let is_just = (m: Opt<t>) -> bool => match m { Just(_) => true, Nil => false, }
let main = () -> u8 => 7`,
			want: 7,
		},
		{
			name: "variable only inside a data-typed parameter, called",
			src: `data Opt<t> = Nil | Just(t)
let is_just = (m: Opt<t>) -> bool => match m { Just(_) => true, Nil => false, }
let main = () -> u8 => {
  let m: Opt<i64> = Just(1)
  if is_just(m) { 7 } else { 0 }
}`,
			want: 7,
		},
		{
			name: "variable only inside a callback's signature, never called",
			src: `let ignores = (g: () -> e) -> bool => true
let main = () -> u8 => 7`,
			want: 7,
		},
		{
			name: "variable only inside a callback's signature, called",
			src: `let ignores = (g: () -> e) -> bool => true
let mk = () -> i64 => 1
let main = () -> u8 => if ignores(mk) { 7 } else { 0 }`,
			want: 7,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if got := buildAndRun(t, c.src); got != c.want {
				t.Errorf("exit = %d, want %d", got, c.want)
			}
		})
	}
}
