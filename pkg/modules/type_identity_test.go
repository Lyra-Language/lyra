package modules_test

import (
	"testing"
)

// Per-module type and trait identity (todo.md, Modules).
//
// Functions got this on 07/30 via FunctionKey: a private declaration is keyed by
// module, so two modules may each declare `helper`, and a declaration taking a prelude
// name is keyed by module whatever its visibility, so the prelude keeps the bare key
// for every module that did not shadow it. Types and traits were left keyed by bare
// name — these pin the three consequences.

// markedPrelude is the shipped prelude's shape: `Maybe` carries the `@builtin` marker
// that confers canonical identity. testPrelude (collision_test.go) has no marker, so a
// user's same-named `Maybe` is re-canonicalized by the name fallback and the shadow is
// invisible — which is exactly why the shadow bug needs the marked form to reproduce.
const markedPrelude = `module std.prelude

@builtin(Maybe)
pub data Maybe<t> = None | Some(t)

@builtin(Result)
pub data Result<t, e> = Ok(t) | Err(e)
`

// Two modules may each declare a *private* type of the same name, exactly as they may
// each declare a private `helper`. A private name is in no other module's namespace, so
// there is nothing for it to collide with.
func TestTypeIdentity_PrivateTypesInTwoModulesDoNotCollide(t *testing.T) {
	root := buildTree(t, map[string]string{
		"app.lyra": "import one\nimport two\nlet main = () -> u8 => u8(one.getOne() + two.getTwo())",
		"one.lyra": `module one
struct Point { x: i64 }
pub let getOne = () -> i64 => {
  let p = Point { x: 1 }
  p.x
}`,
		"two.lyra": `module two
struct Point { y: i64 }
pub let getTwo = () -> i64 => {
  let p = Point { y: 2 }
  p.y
}`,
	})
	res := analyze(t, root)
	if errs := res.Errors(); len(errs) != 0 {
		t.Errorf("two private Points must not collide; got %v", errs)
	}
}

// The same for traits.
func TestTypeIdentity_PrivateTraitsInTwoModulesDoNotCollide(t *testing.T) {
	root := buildTree(t, map[string]string{
		"app.lyra": "import one\nimport two\nlet main = () -> u8 => u8(one.getOne() + two.getTwo())",
		"one.lyra": "module one\ntrait Shape { area: (Self) -> i64 }\npub let getOne = () -> i64 => 1",
		"two.lyra": "module two\ntrait Shape { size: (Self) -> i64 }\npub let getTwo = () -> i64 => 2",
	})
	res := analyze(t, root)
	if errs := res.Errors(); len(errs) != 0 {
		t.Errorf("two private Shapes must not collide; got %v", errs)
	}
}

// A module shadowing the prelude's `Maybe` keeps its own; every other module still
// reaches the prelude's.
//
// Today the shadow is program-wide, so a module that never mentioned `Maybe` loses the
// canonical one and its `?` reports the indefensible "`?` operand must be a Result or
// Maybe, got Maybe" against a type it never declared.
func TestTypeIdentity_PreludeTypeShadowIsConfinedToItsModule(t *testing.T) {
	root := buildTree(t, map[string]string{
		"std/prelude.lyra": markedPrelude,
		// `other` never declares a Maybe, so `?` must keep resolving to the prelude's.
		"other.lyra": `module other
pub let pick = (m: Maybe<i64>) -> Maybe<i64> => {
  let v = m?
  Some(v + 1)
}`,
		"app.lyra": `import other
data Maybe<t> = None | Some(t)
let main = () -> u8 => 0`,
	})
	res := analyzeWithPrelude(t, root)
	if errs := res.Errors(); len(errs) != 0 {
		t.Errorf("a prelude type shadow must not reach another module; got %v", errs)
	}
}

// The shadowing module itself gets its own type, and that is not an error — the warning
// (lyra-W012) is the whole report. Pinned alongside the case above so a fix that simply
// stopped withdrawing the prelude's entry (making the shadow a no-op) fails here.
func TestTypeIdentity_ShadowingModuleGetsItsOwnType(t *testing.T) {
	root := buildTree(t, map[string]string{
		"std/prelude.lyra": markedPrelude,
		"app.lyra": `data Maybe<t> = None | Some(t) | Both(t, t)
let main = () -> u8 => {
  let m: Maybe<i64> = Both(1, 2)
  0
}`,
	})
	res := analyzeWithPrelude(t, root)
	if errs := res.Errors(); len(errs) != 0 {
		t.Fatalf("the shadowing module must get its own Maybe; got %v", errs)
	}
	if !warningsContaining(res, "Maybe") {
		t.Errorf("expected a shadowing warning naming Maybe; got %v", res.Diagnostics)
	}
}
