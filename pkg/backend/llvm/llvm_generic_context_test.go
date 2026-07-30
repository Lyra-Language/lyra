package llvm

import (
	"testing"
)

// A construction that cannot solve all of its own type parameters takes them from the
// context, and lowers.
//
// A generic construction only evaluates to an instantiation when it solves *every*
// parameter itself: `Some(v)` fixes `t`, but `None` fixes nothing and `Ok(v)` fixes `t`
// and not `e`. Those stay the bare declaration on purpose — inventing an instantiation
// from a partial substitution would claim precision the construction did not supply.
// The cost was that they could not lower *anywhere* the context was not an annotated
// `let`, which is the only site that stamped its type onto the value: returning `None`
// failed the build with `unknown named type "Maybe"`, and `Result` was unusable
// outright, since neither of its constructors determines both parameters.
//
// The prelude is what made this urgent — `std/prelude.lyra` exports exactly these two
// types — but nothing here uses the prelude, so the test stays a single self-contained
// source string like the rest of the package.
func TestExec_ContextSuppliesGenericInstantiation(t *testing.T) {
	t.Parallel()
	const decls = `data Maybe<t> = None | Some(t)
data Result<t, e> = Ok(t) | Err(e)
let unwrap_or = (m: Maybe<i64>, fallback: i64) -> i64 => match m {
  Some(v) => v,
  None => fallback,
}
`
	cases := []struct {
		name string
		src  string
		want int
	}{
		{
			// The headline case: a nullary constructor solves nothing at all.
			name: "None in return position",
			src: decls + `let f = () -> Maybe<i64> => None
let main = () -> u8 => u8(unwrap_or(f(), 42))`,
			want: 42,
		},
		{
			name: "None as a call argument",
			src:  decls + `let main = () -> u8 => u8(unwrap_or(None, 42))`,
			want: 42,
		},
		{
			// Ok fixes t and leaves e unsolved, so a *payload-carrying* constructor
			// needs the context too whenever the type has more than one parameter.
			name: "Ok in return position",
			src: decls + `let f = (n: i64) -> Result<i64, string> => Ok(n)
let main = () -> u8 => match f(42) { Ok(v) => u8(v), Err(e) => 1, }`,
			want: 42,
		},
		{
			name: "Err in return position",
			src: decls + `let f = () -> Result<i64, string> => Err("nope")
let main = () -> u8 => match f() { Ok(v) => u8(v), Err(e) => 42, }`,
			want: 42,
		},
		{
			// Both arms are partly solved, and each needs the context separately —
			// the wholesale annotation stamp never reached inside a branch.
			name: "if/else arms",
			src: decls + `let half = (n: i64) -> Result<i64, string> => if n %% 2 == 0 { Ok(n / 2) } else { Err("odd") }
let main = () -> u8 => match half(84) { Ok(v) => u8(v), Err(e) => 1, }`,
			want: 42,
		},
		{
			name: "match arms inside an annotated let",
			src: decls + `let main = () -> u8 => {
  let r: Result<i64, string> = match 1 { 1 => Ok(42), _ => Err("no"), }
  match r { Ok(v) => u8(v), Err(e) => 1, }
}`,
			want: 42,
		},
		{
			// The context supplies the *width* as well as the type argument, so the
			// payload is built at u8 rather than the i64 literal default.
			name: "narrow payload from the context",
			src: `data Result<t, e> = Ok(t) | Err(e)
let f = () -> Result<u8, string> => Ok(42)
let main = () -> u8 => match f() { Ok(v) => v, Err(e) => 1, }`,
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

// The same context-directed instantiation for a generic **struct** and **named tuple**.
//
// They fail differently from a data constructor, which is why they needed a second pass.
// A bare `DataType` is assignable to any instantiation of itself, so a partly solved data
// construction sailed through the front end and died in the backend; a bare
// `NamedStructType`/`TupleType` is not, so a partly solved one was rejected outright with
// "return type mismatch: expected Tagged<i64, boolean>, got Tagged" — a spurious error on
// correct code. Fixing it meant propagating *before* the assignability check rather than
// after it, which is why every context site now goes through `contextualType`.
//
// Two distinct causes are covered here. A **phantom** parameter appears in no field at
// all, so nothing but the context can ever supply it. A parameter that appears only
// *inside* another type (`inner: Opt<t>`, `items: [2]t`) used to be unsolvable for a
// different reason — field inference matched only a field declared as a bare parameter —
// and is now unified structurally, so those solve themselves and need no context.
func TestExec_ContextSuppliesAggregateInstantiation(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		src  string
		want int
	}{
		{
			name: "struct with a phantom parameter",
			src: `struct Tagged<t, u> { value: t }
let f = () -> Tagged<i64, bool> => Tagged { value: 42 }
let main = () -> u8 => u8(f().value)`,
			want: 42,
		},
		{
			name: "struct with a phantom parameter, annotated let",
			src: `struct Tagged<t, u> { value: t }
let main = () -> u8 => {
  let x: Tagged<i64, bool> = Tagged { value: 42 }
  u8(x.value)
}`,
			want: 42,
		},
		{
			name: "struct with a phantom parameter, call argument",
			src: `struct Tagged<t, u> { value: t }
let take = (x: Tagged<i64, bool>) -> i64 => x.value
let main = () -> u8 => u8(take(Tagged { value: 42 }))`,
			want: 42,
		},
		{
			name: "named tuple with a phantom parameter",
			src: `tuple Pair<t, u>(t, t)
let f = () -> Pair<i64, bool> => Pair(40, 2)
let main = () -> u8 => u8(f().0 + f().1)`,
			want: 42,
		},
		{
			// Solved structurally now, so this needs no context at all — but it is
			// the case a user actually writes, and it reported "cannot assign
			// Opt<i64> to Opt<t>" until field inference started unifying.
			name: "parameter only under a nested generic",
			src: `data Opt<t> = Nil | Just(t)
struct Wrapper<t> { inner: Opt<t> }
let f = () -> Wrapper<i64> => Wrapper { inner: Just(42) }
let main = () -> u8 => match f().inner { Just(v) => u8(v), Nil => 1, }`,
			want: 42,
		},
		{
			// No annotation anywhere: the fields alone must yield a *complete*
			// instantiation, or `w.inner` reads back as the type variable. This is
			// what structural field substitution buys over the old name lookup —
			// with the context available the propagation would paper over it.
			name: "nested generic, no context at all",
			src: `data Opt<t> = Nil | Just(t)
struct Wrapper<t> { inner: Opt<t> }
let main = () -> u8 => {
  let w = Wrapper { inner: Just(42) }
  match w.inner { Just(v) => u8(v), Nil => 1, }
}`,
			want: 42,
		},
		{
			name: "parameter only under an array field",
			src: `struct Holder<t> { items: [2]t }
let f = () -> Holder<i64> => Holder { items: [40, 2] }
let main = () -> u8 => u8(f().items[0] + f().items[1])`,
			want: 42,
		},
		{
			name: "parameter only under a tuple field",
			src: `struct Holder<t> { pair: (t, t) }
let f = () -> Holder<i64> => Holder { pair: (40, 2) }
let main = () -> u8 => u8(f().pair.0 + f().pair.1)`,
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
