package llvm

import "testing"

// **A context's allocation must reach a construction's payload even when its type is
// settled**, because allocation is not part of type identity.
//
// `Maybe<shared Expr>` and `Maybe<Expr>` are the same type to `TypesEqual`, which ignores
// the flavor by design. The type-directed push exploits that to decline re-stamping a
// payload whose type the *program* decided rather than this expression's defaults — right
// for types, since re-typing a settled payload clobbers a real choice. The flavor rode
// along with it and stopped at the same guard, so the payload lowered inline while the
// field's slot held a pointer, and the backend refused the store (rule 5):
//
//	llvm: aggregate element type mismatch: cannot store %Expr into { i64, i64, %Expr }*
//
// That left an **optional recursive child with no spelling at all** — `Maybe<Expr>` is
// refused as infinitely sized and `Maybe<shared Expr>` did not lower — which is how the
// bootstrap's AST found it (09/23): a `Lambda` with an optional body is the first program
// here to need one.
//
// The fix stamps the payload *and* the construction's own instantiation, the latter
// because the payload slot is laid out from the type arguments recorded on that node: a
// shared payload under a `Maybe<Expr>` node is the same mismatch one level out.
func TestExec_SharedGenericArgumentReachesThePayload(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		src  string
		want int
	}{
		{
			// The payload is a construction, so its type is settled and the guard fires.
			"optional recursive child, present",
			`data Expr = Lit(i64) | Lam(Lambda)
struct Lambda { body: Maybe<shared Expr> }
let main = () -> u8 => {
  let l = Lambda { body: Some(Lit(7)) }
  match l.body {
    Some(inner) => match inner {
      Lit(n) => u8(n),
      Lam(_) => 0,
    },
    None => 0,
  }
}`,
			7,
		},
		{
			// The absent case has no payload to stamp, and must keep working.
			"optional recursive child, absent",
			`data Expr = Lit(i64) | Lam(Lambda)
struct Lambda { body: Maybe<shared Expr> }
let main = () -> u8 => {
  let l = Lambda { body: None }
  match l.body {
    Some(_) => 1,
    None => 9,
  }
}`,
			9,
		},
		{
			// Nested: the flavor has to reach through one instantiation into the next,
			// which is why the stamp recurses rather than descending one level.
			"a shared child of a shared child",
			`data Expr = Lit(i64) | Lam(Lambda)
struct Lambda { body: Maybe<shared Expr> }
let main = () -> u8 => {
  let inner = Lambda { body: Some(Lit(4)) }
  let outer = Lambda { body: Some(Lam(inner)) }
  match outer.body {
    Some(e) => match e {
      Lam(l) => match l.body {
        Some(deep) => match deep {
          Lit(n) => u8(n),
          Lam(_) => 0,
        },
        None => 0,
      },
      Lit(_) => 0,
    },
    None => 0,
  }
}`,
			4,
		},
		{
			// **The construction's own instantiation, not just its payload.** A `return`
			// whose declared type supplies the context is where the second half shows:
			// the node's payload slot is laid out from the type arguments recorded on
			// *that node*, so a payload stamped shared under a node still reading
			// `Maybe<Expr>` puts a pointer in an inline slot — the same mismatch one
			// level out. This is the shape the bootstrap's collector hit first.
			"a declared return type supplies the flavor",
			`data Expr = Lit(i64) | Lam(Lambda)
struct Lambda { body: Maybe<shared Expr> }
let wrap = (n: i64) -> Maybe<shared Expr> => Some(Lit(n))
let main = () -> u8 => {
  let l = Lambda { body: wrap(5) }
  match l.body {
    Some(inner) => match inner {
      Lit(n) => u8(n),
      Lam(_) => 0,
    },
    None => 0,
  }
}`,
			5,
		},
		{
			// A payload that *is* a guess still narrows as it always did: the fix adds a
			// flavor push on the path the type push declines, and must not disturb it.
			"an untyped payload still narrows under a context",
			`let main = () -> u8 => {
  let m: Maybe<u8> = Some(200)
  match m {
    Some(n) => n,
    None => 0,
  }
}`,
			200,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if got := buildAndRunASanWithPrelude(t, c.src); got != c.want {
				t.Errorf("exited %d; want %d", got, c.want)
			}
		})
	}
}
