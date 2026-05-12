package collector_test

import "testing"

func TestCollectDestructuringSimpleData(t *testing.T) {
	runGoldenTest(t, `let Some x = some_data`, "destructuring_simple_data")
}

func TestCollectDestructuringDataWithTuplePattern(t *testing.T) {
	runGoldenTest(t, `let Some (x, y) = some_data`, "destructuring_data_with_tuple_pattern")
}

func TestCollectDestructuringDataWithArrayPattern(t *testing.T) {
	runGoldenTest(t, `let Some [x, y, z] = some_data`, "destructuring_data_with_array_pattern")
}

func TestCollectDestructuringDataWithStructPattern(t *testing.T) {
	runGoldenTest(t, `let Some {x, y, z} = some_data`, "destructuring_data_with_struct_pattern")
}

func TestCollectDestructuringDataWithDataPattern(t *testing.T) {
	runGoldenTest(t, `let Some AnotherData(x, y) = some_data`, "destructuring_data_with_data_pattern")
}
