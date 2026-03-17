package collector_test

import (
	"path/filepath"
	"testing"
)

func TestCollectDestructuringSimpleData(t *testing.T) {
	source := `let Some x = some_data`
	program, _ := parseAndCollect(t, source)
	got := captureProgramPrint(program)
	checkGolden(t, got, filepath.Join("testdata", "destructuring_simple_data.golden"))
}

func TestCollectDestructuringDataWithTuplePattern(t *testing.T) {
	source := `let Some (x, y) = some_data`
	program, _ := parseAndCollect(t, source)
	got := captureProgramPrint(program)
	checkGolden(t, got, filepath.Join("testdata", "destructuring_data_with_tuple_pattern.golden"))
}

func TestCollectDestructuringDataWithArrayPattern(t *testing.T) {
	source := `let Some [x, y, z] = some_data`
	program, _ := parseAndCollect(t, source)
	got := captureProgramPrint(program)
	checkGolden(t, got, filepath.Join("testdata", "destructuring_data_with_array_pattern.golden"))
}

func TestCollectDestructuringDataWithStructPattern(t *testing.T) {
	source := `let Some {x, y, z} = some_data`
	program, _ := parseAndCollect(t, source)
	got := captureProgramPrint(program)
	checkGolden(t, got, filepath.Join("testdata", "destructuring_data_with_struct_pattern.golden"))
}

func TestCollectDestructuringDataWithDataPattern(t *testing.T) {
	source := `let Some AnotherData(x, y) = some_data`
	program, _ := parseAndCollect(t, source)
	got := captureProgramPrint(program)
	checkGolden(t, got, filepath.Join("testdata", "destructuring_data_with_data_pattern.golden"))
}
