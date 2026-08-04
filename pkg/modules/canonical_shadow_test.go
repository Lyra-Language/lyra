package modules_test

import (
	"strings"
	"testing"

	"github.com/Lyra-Language/lyra/pkg/driver"
)

// Shadowing the prelude's `Maybe`/`Result` and then using `?` on it used to report
// "`?` operand must be a Result or Maybe, got Maybe" — technically true (the marker
// gives the kind to the prelude's declaration, so a same-named unmarked one is an
// ordinary type) and useless, because it names the answer as the problem.
//
// The rule stays; the message now says which of the two mistakes was made. These use
// `markedPrelude` (type_identity_test.go), the shipped prelude's shape: without an
// `@builtin` marker the name-plus-shape fallback claims the kind and there is nothing
// to explain, which is why the typechecker's own prelude-less tests still assert the
// original wording.

func errorContaining(res *driver.Result, want string) bool {
	for _, d := range res.Errors() {
		if strings.Contains(d.Message, want) {
			return true
		}
	}
	return false
}

func TestCanonicalShadow_TryDiagnostic(t *testing.T) {
	cases := []struct {
		name string
		app  string
		want string
		// absent asserts the old, unhelpful wording is gone.
		absent string
	}{
		{
			// Re-declared the prelude's type, shape and all — almost always by not
			// knowing it is already in scope. Deleting it is what makes `?` work.
			"canonical shape: say to remove it",
			`data Maybe<t> = None | Some(t)
let f = (m: Maybe<i64>) -> Maybe<i64> => {
  let v = m?
  Some(v + 1)
}
let main = () -> u8 => 0`,
			"Remove it to use the prelude's Maybe",
			"must be a Result or Maybe, got Maybe",
		},
		{
			// A genuinely different type wearing the name: `?` was never going to
			// apply, and the shared name is what made that read as a contradiction.
			"different shape: say to rename it",
			`data Maybe<t> = Nothing | Just(t)
let f = (m: Maybe<i64>) -> Maybe<i64> => {
  let v = m?
  Just(v + 1)
}
let main = () -> u8 => 0`,
			"a different type that happens to share the name",
			"must be a Result or Maybe, got Maybe",
		},
		{
			"Result shadows the same way",
			`data Result<t, e> = Ok(t) | Err(e)
let f = (r: Result<i64, string>) -> Result<i64, string> => {
  let v = r?
  Ok(v + 1)
}
let main = () -> u8 => 0`,
			"Remove it to use the prelude's Result",
			"must be a Result or Maybe, got Result",
		},
		{
			// The message is only for a shadow. An operand that is simply the wrong
			// type keeps the original wording, which reads correctly there.
			"an unrelated operand type keeps the plain message",
			`data Foo = A | B
let f = (m: Foo) -> Maybe<i64> => {
  let v = m?
  Some(v)
}
let main = () -> u8 => 0`,
			"`?` operand must be a Result or Maybe, got Foo",
			"your own declaration",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			root := buildTree(t, map[string]string{
				"std/prelude.lyra": markedPrelude,
				"app.lyra":         c.app,
			})
			res := analyzeWithPrelude(t, root)
			if !errorContaining(res, c.want) {
				t.Errorf("expected an error containing %q; got %v", c.want, res.Errors())
			}
			if c.absent != "" && errorContaining(res, c.absent) {
				t.Errorf("expected %q to be gone; got %v", c.absent, res.Errors())
			}
		})
	}
}

// The advice has to work, which is the whole point of changing it — the wording this
// replaced would have suggested marking the shadow `@builtin(Maybe)`, and that is
// `lyra-E017` ("duplicate"), because the prelude already claims the kind. So: with
// the shadow removed, `?` resolves against the prelude's type and the program is
// clean.
func TestCanonicalShadow_AdviceResolvesIt(t *testing.T) {
	root := buildTree(t, map[string]string{
		"std/prelude.lyra": markedPrelude,
		"app.lyra": `let f = (m: Maybe<i64>) -> Maybe<i64> => {
  let v = m?
  Some(v + 1)
}
let main = () -> u8 => 0`,
	})
	res := analyzeWithPrelude(t, root)
	if errs := res.Errors(); len(errs) != 0 {
		t.Errorf("removing the shadow should leave a clean program; got %v", errs)
	}
}

// And the advice never says to add a marker, because doing so is a hard error. This
// pins the reason rather than the wording: a program that follows the tempting fix
// gets lyra-E017, so no message may recommend it.
func TestCanonicalShadow_MarkingTheShadowIsAnError(t *testing.T) {
	root := buildTree(t, map[string]string{
		"std/prelude.lyra": markedPrelude,
		"app.lyra": `@builtin(Maybe)
data Maybe<t> = None | Some(t)
let main = () -> u8 => 0`,
	})
	res := analyzeWithPrelude(t, root)
	if !errorContaining(res, "duplicate `@builtin(Maybe)`") {
		t.Errorf("marking a shadow should be a duplicate-claim error; got %v", res.Errors())
	}
}
