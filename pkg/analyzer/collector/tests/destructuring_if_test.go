package collector_test

import (
	"path/filepath"
	"testing"
)

func TestCollectDestructuringIf(t *testing.T) {
	source := `
	if let [a, b, c] = some_array {
		println("#{a}, #{b}, #{c}")
	} else {
		println("No array found")
	}`
	program, _ := parseAndCollect(t, source)
	got := captureProgramPrint(program)
	checkGolden(t, got, filepath.Join("testdata", "destructuring_if_statement.golden"))
}
