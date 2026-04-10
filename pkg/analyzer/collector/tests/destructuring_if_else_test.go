package collector_test

import (
	"path/filepath"
	"testing"
)

func TestCollectDestructuringIfStatement(t *testing.T) {
	source := `
	if let [a, b, c] = some_array {
		println("#{a}, #{b}, #{c}")
	}`
	program, _ := parseAndCollect(t, source)
	got := captureProgramPrint(program)
	checkGolden(t, got, filepath.Join("testdata", "destructuring_if_statement.golden"))
}

func TestCollectDestructuringIfElseStatement(t *testing.T) {
	source := `
	if let [a, b, c] = some_array {
		println("#{a}, #{b}, #{c}")
	} else {
		println("No array found")
	}`
	program, _ := parseAndCollect(t, source)
	got := captureProgramPrint(program)
	checkGolden(t, got, filepath.Join("testdata", "destructuring_if_else_statement.golden"))
}

func TestCollectDestructuringElseStatement(t *testing.T) {
	source := `
	let { foo, bar } = some_struct else {
		println("No struct found")
	}
	println("We got the struct: #{foo}, #{bar}")`
	program, _ := parseAndCollect(t, source)
	got := captureProgramPrint(program)
	checkGolden(t, got, filepath.Join("testdata", "destructuring_else_statement.golden"))
}
