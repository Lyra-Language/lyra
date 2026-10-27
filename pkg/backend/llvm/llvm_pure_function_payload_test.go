package llvm

import (
	"strings"
	"testing"
)

// **A `pure` function wrapped in a construction, stored where a weaker bound is declared** —
// `Some(f)`, `f` a `pure (i64) -> string`, in a field of type `Maybe<(i64) -> string>`.
//
// A `pure` function is usable wherever an unbounded one is, and an annotated `let` always took
// it; a struct field did not. The construction's instantiation had been decided by its
// payload, and a decided one was never re-stamped from a context, so the field refused
// `Maybe<pure (i64) -> string>` — and in a generic struct built inside a function whose own
// type variable shared the struct's parameter's name (`Spec<e>` in `make<e>`), the field
// read as still mentioning an unsolved parameter, went unchecked, and the backend refused the
// store of one `Maybe` into the other (10/27, found by `ui`'s `Node.tears_off`). Both are
// fixed in the typechecker: a fresh construction is re-stamped where the context asks only for
// weaker effect bounds (types.WidensEffects), and the unsolved-parameter test asks about the
// parameters the substitution left unbound, of the declared type.
func TestExec_PureFunctionInAWeakerMaybe(t *testing.T) {
	t.Parallel()
	cases := []struct{ name, src, want string }{
		{
			name: "an annotated binding",
			src: `
let main = () -> void => {
  let f: pure (i64) -> string = (n: i64) -> string => "n${n}"
  let m: Maybe<(i64) -> string> = Some(f)
  if let Some(g) = m { println(g(1)) }
}`,
			want: "n1",
		},
		{
			name: "a struct literal's field",
			src: `
struct Spec {
  on: Maybe<(i64) -> string>,
}
let make = pure (f: pure (i64) -> string) -> Spec => Spec { on: Some(f) }
let main = () -> void => {
  let s = make((n: i64) -> string => "n${n}")
  if let Some(g) = s.on { println(g(2)) }
}`,
			want: "n2",
		},
		{
			name: "a record update's field",
			src: `
struct Spec {
  on: Maybe<(i64) -> string>,
}
let with_on = pure (s: Spec, f: pure (i64) -> string) -> Spec => Spec { s | on: Some(f) }
let main = () -> void => {
  let none: Maybe<(i64) -> string> = None
  let s = with_on(Spec { on: none }, (n: i64) -> string => "n${n}")
  if let Some(g) = s.on { println(g(3)) }
}`,
			want: "n3",
		},
		{
			// The struct's parameter and the function's are both `e`.
			name: "a generic struct built in a function whose variable shares its parameter's name",
			src: `
struct Spec<e> {
  on: Maybe<(i64) -> e>,
}
let make<e> = pure (f: pure (i64) -> e) -> Spec<e> => Spec { on: Some(f) }
let remake<e> = pure (s: Spec<e>, f: pure (i64) -> e) -> Spec<e> => Spec { s | on: Some(f) }
let main = () -> void => {
  let s = make((n: i64) -> string => "n${n}")
  if let Some(g) = s.on { println(g(4)) }
  let t = remake(s, (n: i64) -> string => "m${n}")
  if let Some(g) = t.on { println(g(5)) }
}`,
			want: "n4\nm5",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if got := strings.TrimSpace(buildAndRunWithPrelude(t, c.src, "")); got != c.want {
				t.Errorf("got %q; want %q", got, c.want)
			}
		})
	}
}

// The widening is for a construction alone: a value already held keeps its own type, so a
// `Maybe<pure F>` *binding* is still not a `Maybe<F>` — through a `shared` alias that would let
// an impure function be written where the other holder reads a pure one.
func TestCheck_AHeldPureFunctionMaybeIsNotWidened(t *testing.T) {
	t.Parallel()
	diags := checkWithPrelude(t, `
struct Spec {
  on: Maybe<(i64) -> string>,
}
let hold = pure (f: pure (i64) -> string) -> Spec => {
  let held: Maybe<pure (i64) -> string> = Some(f)
  Spec { on: held }
}
let main = () -> void => {
  if let Some(g) = hold((n: i64) -> string => "n${n}").on { println(g(1)) }
}`)
	if !strings.Contains(strings.Join(diags, "\n"), "Spec.on: cannot assign") {
		t.Errorf("want the held Maybe refused; got %q", diags)
	}
}
