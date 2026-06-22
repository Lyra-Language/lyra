package typechecker_test

import "testing"

// Data construction uses call syntax (`Some(42)`) — juxtaposition application
// (`Some 42`) was removed. The applied form parses as a named tuple literal; the
// typechecker resolves a capitalized application whose name is a data-type
// constructor to its owning data type, so it type-checks against a `Maybe`/
// `Result` annotation.

// A data constructor whose payload is an inline record (`data Tree = … | Node
// { … }`) is built with struct-literal syntax and type-checks like a struct
// literal, evaluating to the owning data type.
func TestInlineRecordConstructor_Ok(t *testing.T) {
	res := parseCollectAndCheck(t, `
  data Tree = Nil | Node { left: i64, value: i64 }
  let n = Node { left: 1, value: 2 }
	`, false)
	assertNoErrors(t, res)
}

func TestInlineRecordConstructor_BadFieldType_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `
  data Tree = Nil | Node { left: i64, value: i64 }
  let n = Node { left: 1, value: "x" }
	`, false)
	assertErrorsAre(t, res, "Node.value: cannot assign string to i64")
}

func TestInlineRecordConstructor_MissingField_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `
  data Tree = Nil | Node { left: i64, value: i64 }
  let n = Node { left: 1 }
	`, false)
	assertErrorsAre(t, res, "Node: missing field \"value\"")
}

// The data type's generic parameter is inferred from the record field values,
// the same as for a generic struct literal.
func TestInlineRecordConstructor_GenericInferred_Ok(t *testing.T) {
	res := parseCollectAndCheck(t, `
  data Box<t> = Empty | Wrap { value: t }
  let b = Wrap { value: 42 }
	`, false)
	assertNoErrors(t, res)
}

func TestDataConstructorCall_AppliedResolvesToDataType(t *testing.T) {
	res := parseCollectAndCheck(t, `
  data Maybe<t> = None | Some t
  let wrap = (n: i64) -> Maybe<i64> => Some(n)
	`, false)
	assertNoErrors(t, res)
}

func TestDataConstructorCall_NullaryResolvesToDataType(t *testing.T) {
	res := parseCollectAndCheck(t, `
  data Maybe<t> = None | Some t
  let empty = () -> Maybe<i64> => None
	`, false)
	assertNoErrors(t, res)
}

func TestDataConstructorCall_ResultConstructor(t *testing.T) {
	res := parseCollectAndCheck(t, `
  data Result<t, e> = Ok t | Err e
  let ok = (n: i64) -> Result<i64, string> => Ok(n)
	`, false)
	assertNoErrors(t, res)
}

// The resolution is nominal: a constructor of a different data type is not
// assignable to the annotated data type (it is not "any data type passes").
func TestDataConstructorCall_WrongDataTypeRejected(t *testing.T) {
	res := parseCollectAndCheck(t, `
  data Maybe<t> = None | Some t
  data Color = Red | Green
  let bad = () -> Maybe<i64> => Red
	`, false)
	assertErrorsAre(t, res,
		"bad: return type mismatch: expected Maybe, got Color")
}
