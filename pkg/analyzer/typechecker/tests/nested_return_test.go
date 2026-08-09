package typechecker_test

import "testing"

// A `return` nested inside an `if`, a loop or a match arm is checked against the
// declared return type, like one written at the body block's top level.
//
// Until 08/08 nothing checked it at all. checkBlockReturn walks `block.Statements`
// and handles the ReturnStmts it finds there; a nested one reaches checkNode, which
// had no case for it — so the value was never compared against the declared return,
// and never given it as *context*.
//
// The second consequence is the one that bit: a data constructor needs its expected
// type to instantiate, so an early `return None` reached the backend untyped and died
// with `no type recorded for data constructor "None"` on a program the front end had
// passed — rule 5 inverted. Scalars hid it, since a literal lowers from its own
// intrinsic type and needs no context, which is why guard clauses looked like they
// worked right up until one returned a `Maybe`.
func TestTypeCheck_NestedReturnIsChecked(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		source string
		errors []string
	}{
		{
			"early return of a data constructor",
			`data Maybe<t> = None | Some t
let g = (n: i64) -> Maybe<i64> => {
  if n < 0 { return None }
  Some(n)
}`,
			nil,
		},
		{
			"return from inside a loop",
			`data Maybe<t> = None | Some t
let g = (n: i64) -> Maybe<i64> => {
  var i = 0;
  for i < 3 {
    if i == n { return Some(i) }
    i = i + 1;
  }
  None
}`,
			nil,
		},
		{
			// The half that was silently unchecked rather than merely uncontextualized.
			"a nested return of the wrong type is reported",
			`data Maybe<t> = None | Some t
let g = (n: i64) -> Maybe<i64> => {
  if n < 0 { return "nope" }
  Some(n)
}`,
			[]string{`g: return type mismatch: expected Maybe<i64>, got string`},
		},
		{
			// The context does more than admit the value: it narrows untyped literal
			// leaves, exactly as a top-level return does.
			//
			// It does not range-*check* them — `return 300` for a `-> u8` is accepted
			// here, and so is the plain `() -> u8 => 300` beside it. That gap is
			// pre-existing, common to every return position, and separate from this.
			"an early return narrows an untyped literal",
			`let g = (n: u8) -> u8 => {
  if n < 3 { return 200 }
  n
}`,
			nil,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			res := parseCollectAndCheck(t, c.source, false)
			if len(c.errors) == 0 {
				assertNoErrors(t, res)
				return
			}
			for _, want := range c.errors {
				assertHasErrorContaining(t, res, want)
			}
		})
	}
}
