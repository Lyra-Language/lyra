package typechecker_test

import "testing"

// A construction whose payload is another construction that left a parameter open —
// `Wrap(Pick(5))`, the shape of `Some(Ok(5))` — infers to `Box<Either>` with the inner
// type bare, and until 09/28 no context accepted it, annotation included. The context now
// matches it argument-wise and narrows the inner construction.

const nestedTypes = `
  data Box<t> = Wrap(t) | Empty
  data Either<a, b> = Pick(a) | Other(b)
  type Meters = i64
`

func TestNestedConstruction_TakesItsContext(t *testing.T) {
	res := parseCollectAndCheck(t, nestedTypes+`
  let a: Box<Either<i64, string>> = Wrap(Pick(5))
  let b = () -> Box<Either<u8, string>> => Wrap(Pick(200))
  let c: Box<Box<Either<i64, bool>>> = Wrap(Wrap(Other(true)))
	`, false)
	assertNoErrors(t, res)
}

// The context still checks what it settles: a wrong inner payload and an overflowing
// literal are reported at the inner construction.
func TestNestedConstruction_InnerPayloadIsChecked(t *testing.T) {
	res := parseCollectAndCheck(t, nestedTypes+`
  let a: Box<Either<i64, string>> = Wrap(Pick("x"))
  let b: Box<Either<u8, string>> = Wrap(Pick(300))
	`, false)
	assertErrorsAre(t, res,
		"Pick: cannot assign string to i64",
		"Pick: literal value 300 overflows u8")
}

// Only a bare *generic* data type is open; the argument-wise match must not admit the
// type-level widenings isAssignable allows, such as a base into its newtype.
func TestNestedConstruction_ArgumentsStayInvariant(t *testing.T) {
	res := parseCollectAndCheck(t, nestedTypes+`
  let f = (m: Box<i64>) -> Box<Meters> => m
	`, false)
	assertErrorsAre(t, res, "f: return type mismatch: expected Box<Meters>, got Box<i64>")
}

// A branch as a construction's payload takes the context too (10/13): `Wrap(if …)` whose
// arms leave an empty array's element open, or default a literal's width, is decided by the
// annotation as `Wrap(Pick([]))` and `Wrap([1])` are. Found in Vega's `poll_game_build`, which
// had to name its empty array's type. An arm the program settled still settles the join.
func TestNestedConstruction_ABranchPayloadTakesItsContext(t *testing.T) {
	res := parseCollectAndCheck(t, nestedTypes+`
  let a = (c: bool) -> Box<Either<[]u8, string>> => Wrap(if c { Pick([]) } else { Other("no") })
  let b = (c: bool) -> Box<Either<[]u8, string>> => Wrap(match c { true => Pick([]), false => Other("no") })
  let d = (c: bool) -> Box<[]u8> => Wrap(if c { [] } else { [1] })
  let e = (c: bool, x: i64) -> Box<u8> => Wrap(if c { x } else { 2 })
	`, false)
	assertErrorsAre(t, res, "e: return type mismatch: expected Box<u8>, got Box<i64>")
}

// A chain's context reaches its last call alone (10/13): under `-> Box<string>`, the first
// `then` of `b.then(f).then(g)` solves its result from `f`, a `Box<i64>`, not from the
// function's return — a receiver is not the call's value. It was refused at `f`'s lambda,
// "expected Box<string>, got Box<i64>", where the chain split at a `let` was typed. A
// field read's object is outside the context too.
func TestNestedConstruction_AChainsContextReachesItsLastCall(t *testing.T) {
	res := parseCollectAndCheck(t, nestedTypes+`
  impl Box<t> {
    pub let then<t, u> = pure (self, f: (t) -> Box<u>) -> Box<u> {
      (Wrap v, f) => f(v),
      (Empty, _) => Empty,
    }
  }
  struct Pair { left: Box<i64>, right: i64 }
  let chained = (b: Box<i64>) -> Box<string> => b.then((n) => Wrap(n * 2)).then((k) => Wrap("${k}"))
  let field = (p: Pair) -> Box<string> => p.left.then((n) => Wrap("${n}"))
	`, false)
	assertNoErrors(t, res)
}
