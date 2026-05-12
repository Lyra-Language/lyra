package collector_test

import "testing"

func TestCollectDestructuringStruct(t *testing.T) {
	runGoldenTest(t, `let {a, b, c} = some_struct`, "destructuring_struct")
}

func TestCollectDestructuringStructWithRename(t *testing.T) {
	runGoldenTest(t, `let {a: foo, b: bar, c: baz} = some_struct`, "destructuring_struct_with_rename")
}

func TestCollectDestructuringStructWithRest(t *testing.T) {
	runGoldenTest(t, `let {a, b, ...rest} = some_struct`, "destructuring_struct_with_rest")
}

func TestCollectDestructuringStructWithSubPatterns(t *testing.T) {
	runGoldenTest(t, `let {a: [x, y, z], b: (a, b), c: {d, e, f}} = some_struct`, "destructuring_struct_with_sub_patterns")
}
