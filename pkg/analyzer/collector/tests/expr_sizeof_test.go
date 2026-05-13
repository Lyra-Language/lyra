package collector_test

import "testing"

func TestCollect_SizeofPrimitive(t *testing.T) {
	source := `let x = @sizeof(i32)`
	runGoldenTest(t, source, "expr_sizeof_primitive")
}

func TestCollect_SizeofUserDefinedType(t *testing.T) {
	source := `let x = @sizeof(Player)`
	runGoldenTest(t, source, "expr_sizeof_user_defined")
}

func TestCollect_SizeofPointer(t *testing.T) {
	source := `let x = @sizeof(^i32)`
	runGoldenTest(t, source, "expr_sizeof_pointer")
}
