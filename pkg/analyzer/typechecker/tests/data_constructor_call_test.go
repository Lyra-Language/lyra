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
		"bad: return type mismatch: expected Maybe<i64>, got Color")
}

// **A constructor resolves as the asking file sees it, and resolves the same way twice.**
//
// The lookup iterated SymbolTable.Types directly, which is rule 4's forbidden shape twice
// over. It saw every module's *private* declarations, so naming another module's private
// constructor found a data type the file has no right to name — the caller then looked the
// name up properly, correctly got nothing back, and dereferenced it, taking `lyrac` down
// with a SIGSEGV (and the LSP with it, on every keystroke). And it iterated a Go map, so two
// data types sharing a constructor name resolved to a different one on each compile.
//
// Here a program declares its own `Some` beside the prelude's. The local declaration must
// win — a module's own declaration shadows an ambient one for every other kind of name —
// and it must win every time, which is why this runs the check repeatedly.
func TestDataConstructor_LocalDeclarationShadowsThePrelude(t *testing.T) {
	const src = `
data Opt = Some(i64) | None
let main = () -> void => {
  let m: Opt = Some(1)
  println(match m { Some(x) => x, None => 0 })
}`
	for i := 0; i < 8; i++ {
		res := parseCollectAndCheck(t, src, false)
		assertNoErrors(t, res)
	}
}

// A constructor of an unshadowed local data type still resolves.
func TestDataConstructor_UnshadowedStillResolves(t *testing.T) {
	res := parseCollectAndCheck(t, `
data Other = Alpha(i64) | Beta
let f = (o: Other) -> i64 => match o { Alpha(n) => n, Beta => 0 }`, false)
	assertNoErrors(t, res)
}
