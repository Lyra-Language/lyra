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
