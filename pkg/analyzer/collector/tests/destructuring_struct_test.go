package collector_test

import (
	"path/filepath"
	"testing"
)

func TestCollectDestructuringStruct(t *testing.T) {
	source := `let {a, b, c} = some_struct`
	program, _ := parseAndCollect(t, source)
	got := captureProgramPrint(program)
	checkGolden(t, got, filepath.Join("testdata", "destructuring_struct.golden"))
}

func TestCollectDestructuringStructWithRename(t *testing.T) {
	source := `let {a: foo, b: bar, c: baz} = some_struct`
	program, _ := parseAndCollect(t, source)
	got := captureProgramPrint(program)
	checkGolden(t, got, filepath.Join("testdata", "destructuring_struct_with_rename.golden"))
}

func TestCollectDestructuringStructWithRest(t *testing.T) {
	source := `let {a, b, ...rest} = some_struct`
	program, _ := parseAndCollect(t, source)
	got := captureProgramPrint(program)
	checkGolden(t, got, filepath.Join("testdata", "destructuring_struct_with_rest.golden"))
}

func TestCollectDestructuringStructWithSubPatterns(t *testing.T) {
	source := `let {a: [x, y, z], b: (a, b), c: {d, e, f}} = some_struct`
	program, _ := parseAndCollect(t, source)
	got := captureProgramPrint(program)
	checkGolden(t, got, filepath.Join("testdata", "destructuring_struct_with_sub_patterns.golden"))
}
