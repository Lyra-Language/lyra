package typechecker_test

import (
	"strings"
	"testing"
)

// A partly solved construction is checked against the *context's* type arguments.
//
// This is the safety half of context-directed instantiation. A construction that
// cannot solve every parameter itself — `None` solves nothing, `Ok(v)` solves `t` and
// not `e` — stays the bare declaration, and a bare declaration was assignable to any
// instantiation of itself, so nothing verified the payload: `let r: Result<i64, string>
// = Ok("x")` passed the front end and was caught only by the backend refusing to store
// a string fat pointer into an i64 payload. A type error found by the code generator is
// a type error found in the wrong place, and it survived only because the value could
// not lower at all; making these lower is exactly what would have turned it silent.
func TestGenericContext_PayloadIsCheckedAgainstTheContext(t *testing.T) {
	const decls = "data Result<t, e> = Ok(t) | Err(e)\ndata Maybe<t> = None | Some(t)\n"
	for _, c := range []struct{ name, src, want string }{
		{
			name: "return position, first parameter",
			src:  decls + `let f = () -> Result<i64, string> => Ok("x")`,
			want: "Ok: cannot assign string to i64",
		},
		{
			name: "return position, second parameter",
			src:  decls + `let f = () -> Result<i64, string> => Err(7)`,
			want: "Err: cannot assign integer literal to string",
		},
		{
			name: "annotated let",
			src:  decls + `let r: Result<i64, string> = Ok("x")`,
			want: "Ok: cannot assign string to i64",
		},
		{
			// Each branch is checked separately — the annotation stamp never
			// reached inside one.
			name: "one branch of an if",
			src:  decls + `let f = (n: i64) -> Result<i64, string> => if n > 0 { Ok(n) } else { Err(0) }`,
			want: "Err: cannot assign integer literal to string",
		},
		{
			name: "a match arm",
			src:  decls + `let f = (n: i64) -> Result<i64, string> => match n { 0 => Err("z"), _ => Ok("nope"), }`,
			want: "Ok: cannot assign string to i64",
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			res := parseCollectAndCheck(t, c.src, false)
			if !hasError(res, c.want) {
				t.Errorf("expected %q; got %v", c.want, res.errors)
			}
		})
	}
}

// The context supplies the payload's *width*, not just its type argument.
//
// Solving promotes an untyped literal to its default in order to unify, so `Ok(42)`
// fixes `t = i64` locally. Recording that width would pre-empt a context that says
// `Result<u8, string>` and turn a valid program into "cannot assign i64 to u8", so an
// untyped leaf is left untyped whenever the payload alone did not pin down every
// parameter — the same rule an unannotated literal follows everywhere else.
func TestGenericContext_PayloadNarrowsToTheContextWidth(t *testing.T) {
	res := parseCollectAndCheck(t, `
data Result<t, e> = Ok(t) | Err(e)
let f = () -> Result<u8, string> => Ok(42)
`, false)
	assertNoErrors(t, res)
}

// A construction that solves itself is left alone: the context does not get to
// override an instantiation the value genuinely determined, or a real mismatch would
// be stamped away rather than reported.
func TestGenericContext_FullySolvedMismatchStillReported(t *testing.T) {
	res := parseCollectAndCheck(t, `
data Maybe<t> = None | Some(t)
let f = () -> Maybe<i64> => Some("x")
`, false)
	// Reported by the ordinary return-type check against the instantiation the value
	// determined for itself — not by the payload check, which correctly declines to
	// touch an already-solved construction.
	if !hasError(res, "expected Maybe<i64>, got Maybe<string>") {
		t.Errorf("a fully solved construction must still be checked against the context; got %v", res.errors)
	}
}

func hasError(res checkResult, want string) bool {
	for _, e := range res.errors {
		if strings.Contains(e.Message, want) {
			return true
		}
	}
	return false
}

// The struct and named-tuple halves of the same rule: the payload is checked against the
// context, and reported exactly once.
//
// Both used to produce *two* diagnostics for one mistake — the precise one from the
// payload check plus a coarse "return type mismatch … got Tagged" from the caller, since
// a bare aggregate is not assignable to its own instantiation. The propagation now tells
// the caller it already reported.
func TestGenericContext_AggregatePayloadCheckedOnce(t *testing.T) {
	for _, c := range []struct{ name, src, want string }{
		{
			name: "struct field",
			src: `struct Tagged<t, u> { value: t }
let f = () -> Tagged<i64, bool> => Tagged { value: "x" }`,
			want: "Tagged.value: cannot assign string to i64",
		},
		{
			name: "named tuple element",
			src: `tuple Pair<t, u>(t, t)
let f = () -> Pair<i64, bool> => Pair(40, "x")`,
			want: "Pair: cannot assign string to i64",
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			res := parseCollectAndCheck(t, c.src, false)
			if !hasError(res, c.want) {
				t.Fatalf("expected %q; got %v", c.want, res.errors)
			}
			var errs int
			for _, e := range res.errors {
				if strings.Contains(e.Message, "cannot assign") || strings.Contains(e.Message, "mismatch") {
					errs++
				}
			}
			if errs != 1 {
				t.Errorf("one mistake should give one diagnostic; got %d: %v", errs, res.errors)
			}
		})
	}
}

// A genuinely wrong instantiation is still rejected — the context does not get to stamp
// its way over a value that determined a different one for itself.
func TestGenericContext_WrongAggregateInstantiationStillReported(t *testing.T) {
	res := parseCollectAndCheck(t, `
struct Box<t> { value: t }
let f = () -> Box<bool> => Box { value: 42 }
`, false)
	if !hasError(res, "expected Box<boolean>, got Box<i64>") {
		t.Errorf("expected a return-type mismatch; got %v", res.errors)
	}
}

// A fully solvable named tuple is still checked without any context at all: deferring to
// the context applies only when the elements left a parameter unsolved.
func TestGenericContext_FullySolvableTupleStillCheckedWithoutContext(t *testing.T) {
	res := parseCollectAndCheck(t, `
tuple Trip<t>(t, t)
let p = Trip(1, "x")
`, false)
	if !hasError(res, "Trip: element 2: cannot assign string to i64") {
		t.Errorf("expected the element check to fire with no context; got %v", res.errors)
	}
}

// A field whose declared type merely *mentions* a solved parameter is checked against the
// substituted type, not skipped.
//
// The struct's field types were substituted by looking the field's type *name* up in the
// solution, which only ever rewrote a field declared as a bare parameter. A field declared
// `Opt<t>` kept its raw variable, so the "still generic, check leniently" guard swallowed
// it and a wrong value went unreported — silently, since the surrounding instantiation was
// solved by the other field and looked complete.
func TestGenericContext_CompoundFieldIsCheckedAgainstTheSubstitution(t *testing.T) {
	res := parseCollectAndCheck(t, `
data Opt<t> = Nil | Just(t)
struct Holder<t> { tag: t, inner: Opt<t> }
let w = Holder { tag: 1, inner: Just("x") }
`, false)
	if !hasError(res, "Holder.inner: cannot assign Opt<string> to Opt<i64>") {
		t.Errorf("a compound field's mismatch must be reported; got %v", res.errors)
	}
}

// Unifying through a function type must still *reject*: binding `t` from a callback is
// only sound if an inconsistent or ill-shaped one is refused.
func TestGenericContext_FunctionArgumentUnificationStillRejects(t *testing.T) {
	for _, c := range []struct{ name, src, want string }{
		{
			name: "two arguments imply different bindings",
			src: `let apply = (f: (t) -> t, x: t) -> t => f(x)
let dbl = (n: i64) -> i64 => n * 2
let bad = apply(dbl, "x")`,
			want: "cannot infer type variable t",
		},
		{
			name: "callback has the wrong arity",
			src: `let g = (h: () -> t) -> t => h()
let two = (a: i64, b: i64) -> i64 => a
let bad = g(two)`,
			want: "cannot infer type variable t",
		},
		{
			name: "the solved return type is enforced at the use site",
			src: `let g = (h: () -> t) -> t => h()
let mk = () -> i64 => 42
let s: string = g(mk)`,
			want: "cannot assign i64 to string",
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			res := parseCollectAndCheck(t, c.src, false)
			if !hasError(res, c.want) {
				t.Errorf("expected %q; got %v", c.want, res.errors)
			}
		})
	}
}
