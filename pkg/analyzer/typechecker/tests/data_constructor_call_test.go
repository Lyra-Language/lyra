package typechecker_test

import "testing"

// Data construction uses call syntax (`Some(42)`) — juxtaposition application
// (`Some 42`) was removed. The applied form parses as a named tuple literal; the
// typechecker resolves a capitalized application whose name is a data-type
// constructor to its owning data type, so it type-checks against a `Maybe`/
// `Result` annotation.

func TestDataConstructorCall_AppliedResolvesToDataType(t *testing.T) {
	res := parseCollectAndCheck(t, `
  data Maybe<T> = None | Some T
  let wrap = (n: i64) -> Maybe<i64> => Some(n)
	`, false)
	assertNoErrors(t, res)
}

func TestDataConstructorCall_NullaryResolvesToDataType(t *testing.T) {
	res := parseCollectAndCheck(t, `
  data Maybe<T> = None | Some T
  let empty = () -> Maybe<i64> => None
	`, false)
	assertNoErrors(t, res)
}

func TestDataConstructorCall_ResultConstructor(t *testing.T) {
	res := parseCollectAndCheck(t, `
  data Result<T, E> = Ok T | Err E
  let ok = (n: i64) -> Result<i64, string> => Ok(n)
	`, false)
	assertNoErrors(t, res)
}

// The resolution is nominal: a constructor of a different data type is not
// assignable to the annotated data type (it is not "any data type passes").
func TestDataConstructorCall_WrongDataTypeRejected(t *testing.T) {
	res := parseCollectAndCheck(t, `
  data Maybe<T> = None | Some T
  data Color = Red | Green
  let bad = () -> Maybe<i64> => Red
	`, false)
	assertErrorsAre(t, res,
		"bad: return type mismatch: expected Maybe, got Color")
}
