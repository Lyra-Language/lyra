package typechecker_test

import "testing"

func TestSizeof_PrimitiveType_NoError(t *testing.T) {
	res := parseCollectAndCheck(t, `let x = @sizeof(i32)`, false)
	assertNoErrors(t, res)
}

func TestSizeof_PointerType_NoError(t *testing.T) {
	res := parseCollectAndCheck(t, `let x = @sizeof(^i64)`, false)
	assertNoErrors(t, res)
}

func TestSizeof_KnownStructType_NoError(t *testing.T) {
	res := parseCollectAndCheck(t, `
		struct Point { x: i64, y: i64 }
		let x = @sizeof(Point)
	`, false)
	assertNoErrors(t, res)
}

func TestSizeof_UnknownType_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `let x = @sizeof(Player)`, false)
	assertErrorsAre(t, res, `unknown type "Player"`)
}
