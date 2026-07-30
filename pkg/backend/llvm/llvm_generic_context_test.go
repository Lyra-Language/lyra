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
