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
