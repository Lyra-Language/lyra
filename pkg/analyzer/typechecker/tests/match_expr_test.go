package typechecker_test

import (
	"testing"
)

func TestMatchExpr_ArmsHaveSameType(t *testing.T) {
	res := parseCollectAndCheck(t, `
	let num = 2
	let numName = match num {
		1 => "one",
		2 => "two",
		3 => "three",
		_ => "other",
	}`, false)
	assertNoErrors(t, res)
}

func TestMatchExpr_ArmsHaveSameTypeWithBlocks(t *testing.T) {
	res := parseCollectAndCheck(t, `
	let num = 2
	let numName = match num {
		1 => { "one" },
		2 => { "two" },
		3 => { "three" },
		_ => { "other" },
	}`, false)
	assertNoErrors(t, res)
}

func TestMatchExpr_ArmsHaveDifferentTypes(t *testing.T) {
	res := parseCollectAndCheck(t, `
	let num = 2
	let numName = match num {
		1 => "one",
		2 => 2,
		3 => "three",
		_ => "other",
	}`, false)
	assertErrorsAre(t, res, "match arms have incompatible types: string vs int")
}

func TestMatchExpr_ArmsHaveDifferentTypesWithBlocks(t *testing.T) {
	res := parseCollectAndCheck(t, `
	let num = 2
	let numName = match num {
		1 => { "one" },
		2 => { false },
		3 => { "three" },
		_ => { "other" },
	}`, false)
	assertErrorsAre(t, res, "match arms have incompatible types: string vs boolean")
}
