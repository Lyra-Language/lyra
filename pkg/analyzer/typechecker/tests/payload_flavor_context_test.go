package typechecker_test

import (
	"strings"
	"testing"
)

// A literal payload's **flavor** is a guess a context may override, like an untyped
// leaf's width.
//
// `let m: Maybe<[]i64> = Some([1])` was refused as "cannot assign Maybe<StaticArray<i64, 1>>
// to Maybe<DynamicArray<i64>>" — in every position, not just an annotation — because
// `Some([1])` solved `t` completely and a complete solve read as settled, while `Ok([1])`
// against a `Result<[]i64, e>` worked only because `Ok` solves too little to settle. And an
// array literal's elements were joined before the annotation was consulted, so
// `[[1], [2, 3]]` under `[][]i64` was refused as two incompatible element types (09/13).

const payloadFlavorPrelude = `
data Maybe<t> = None | Some(t)
data Box<t> = Empty | Full(t)
struct Holder { m: Maybe<[]i64> }
`

func TestPayloadFlavor_EveryPosition(t *testing.T) {
	res := parseCollectAndCheck(t, payloadFlavorPrelude+`
let take = (m: Maybe<[]i64>) -> i64 => 0
let give = () -> Maybe<[]i64> => Some([4, 5, 6])
let main = () -> void => {
  let a: Maybe<[]i64> = Some([1])
  let b: []Maybe<[]i64> = [Some([1]), None]
  let h = Holder { m: Some([1, 2]) }
  var r: Maybe<[]i64> = None
  r = Some([7, 8])
  let x: Box<[]i64> = Full([9])
  let n = take(Some([1, 2]))
  let p: Maybe<(u8, u8)> = Some((1, 2))
  let q: Maybe<([]i64, i64)> = Some(([1], 2))
}
`, false)
	assertNoErrors(t, res)
}

// Elements take the context before they are joined, at any depth and in any flavor of
// outer array, and a construction's payloads may differ in length.
func TestPayloadFlavor_ElementsJoinUnderTheirContext(t *testing.T) {
	res := parseCollectAndCheck(t, payloadFlavorPrelude+`
let main = () -> void => {
  let a: [][]u8 = [[1], [2, 3], []]
  let b: [][][]i64 = [[[1]], [[2], [3, 4]]]
  let c: [2][]i64 = [[1], [2, 3]]
  let d: []Maybe<[]i64> = [Some([1]), Some([2, 3]), None]
}
`, false)
	assertNoErrors(t, res)
}

// What must still be refused: a literal with no context to choose its flavor, and a
// fixed-array *binding*, which is stack storage rather than a literal.
func TestPayloadFlavor_NoContextOrABindingIsStillRefused(t *testing.T) {
	res := parseCollectAndCheck(t, `
let main = () -> void => {
  let g = [[1], [2, 3]]
}
`, false)
	assertHasErrorContaining(t, res, "is not compatible with preceding element type")

	res = parseCollectAndCheck(t, `
let main = () -> void => {
  let a: [1]i64 = [1]
  let b: [2]i64 = [2, 3]
  let g: [][]i64 = [a, b]
}
`, false)
	assertHasErrorContaining(t, res, "is not compatible with preceding element type")
}

// Narrowing is where a literal meets the width it must fit, so an overflow found there is
// reported — once — rather than lost. Discarding it during development compiled the program.
func TestPayloadFlavor_AnOverflowInsideIsReportedOnce(t *testing.T) {
	res := parseCollectAndCheck(t, payloadFlavorPrelude+`
let f = () -> []Maybe<[]u8> => [None, Some([300, 1])]
let main = () -> void => {
  let g: []Maybe<[]u8> = [Some([300])]
}
`, false)
	assertErrorsAre(t, res,
		"element 1: literal value 300 overflows u8",
		"element 1: literal value 300 overflows u8")
}

// A payload wrong for its context is one mistake: not restated by the sibling join, and
// not again by the return site's second inference.
func TestPayloadFlavor_AWrongPayloadIsReportedOnce(t *testing.T) {
	for _, src := range []string{
		`let f = () -> []Maybe<string> => [None, Some(5)]`,
		`let main = () -> void => { let g: []Maybe<string> = [Some([1]), None] }`,
	} {
		res := parseCollectAndCheck(t, payloadFlavorPrelude+src, false)
		if len(res.errors) != 1 || !strings.Contains(res.errors[0].Message, "Some: cannot assign") {
			t.Errorf("%s\nwant one \"Some: cannot assign\" error, got %v", src, res.errors)
		}
	}
}
