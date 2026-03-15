package tests

import (
	"path/filepath"
	"testing"
)

func TestCollector_DestructuringTuple(t *testing.T) {
	source := `let (x, y, z) = ThreeInts(1, 2, 3)`
	program, _ := parseAndCollect(t, source)
	got := captureProgramPrint(program)
	checkGolden(t, got, filepath.Join("testdata", "destructuring_tuple.golden"))
}

func TestCollector_DestructuringTupleFromIdentifier(t *testing.T) {
	source := `let (x, y, z) = some_tuple`
	program, _ := parseAndCollect(t, source)
	got := captureProgramPrint(program)
	checkGolden(t, got, filepath.Join("testdata", "destructuring_tuple_from_identifier.golden"))
}

func TestCollector_DestructuringTupleWithRestAtBeginning(t *testing.T) {
	source := `let (...rest, x, y, z) = some_tuple`
	program, _ := parseAndCollect(t, source)
	got := captureProgramPrint(program)
	checkGolden(t, got, filepath.Join("testdata", "destructuring_tuple_with_rest_at_beginning.golden"))
}

func TestCollector_DestructuringTupleWithRestInMiddle(t *testing.T) {
	source := `let (x, y, ...rest, z) = some_tuple`
	program, _ := parseAndCollect(t, source)
	got := captureProgramPrint(program)
	checkGolden(t, got, filepath.Join("testdata", "destructuring_tuple_with_rest_in_middle.golden"))
}

func TestCollector_DestructuringTupleWithRestAtEnd(t *testing.T) {
	source := `let (x, y, z, ...rest) = some_tuple`
	program, _ := parseAndCollect(t, source)
	got := captureProgramPrint(program)
	checkGolden(t, got, filepath.Join("testdata", "destructuring_tuple_with_rest_at_end.golden"))
}
