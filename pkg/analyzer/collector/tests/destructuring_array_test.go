package collector_test

import (
	"path/filepath"
	"testing"
)

func TestCollectDestructuringArray(t *testing.T) {
	source := `let [a, b, c] = some_array`
	program, _ := parseAndCollect(t, source)
	got := captureProgramPrint(program)
	checkGolden(t, got, filepath.Join("testdata", "destructuring_array.golden"))
}

func TestCollectDestructuringArrayWithRestAtStart(t *testing.T) {
	source := `let [...head, a, b, c] = some_array`
	program, _ := parseAndCollect(t, source)
	got := captureProgramPrint(program)
	checkGolden(t, got, filepath.Join("testdata", "destructuring_array_with_rest_at_start.golden"))
}

func TestCollectDestructuringArrayWithRestInMiddle(t *testing.T) {
	source := `let [a, ...middle, c] = some_array`
	program, _ := parseAndCollect(t, source)
	got := captureProgramPrint(program)
	checkGolden(t, got, filepath.Join("testdata", "destructuring_array_with_rest_in_middle.golden"))
}

func TestCollectDestructuringArrayWithRestAtEnd(t *testing.T) {
	source := `let [a, b, c, ...tail] = some_array`
	program, _ := parseAndCollect(t, source)
	got := captureProgramPrint(program)
	checkGolden(t, got, filepath.Join("testdata", "destructuring_array_with_rest_at_end.golden"))
}
