package collector_test

import "testing"

func TestCollectDestructuringIfStatement(t *testing.T) {
	source := `
	if let [a, b, c] = some_array {
		println("${a}, ${b}, ${c}")
	}`
	runGoldenTest(t, source, "destructuring_if_statement")
}

func TestCollectDestructuringIfElseStatement(t *testing.T) {
	source := `
	if let [a, b, c] = some_array {
		println("${a}, ${b}, ${c}")
	} else {
		println("No array found")
	}`
	runGoldenTest(t, source, "destructuring_if_else_statement")
}

func TestCollectDestructuringElseStatement(t *testing.T) {
	source := `
	let { foo, bar } = some_struct else {
		println("No struct found")
	}
	println("We got the struct: ${foo}, ${bar}")`
	runGoldenTest(t, source, "destructuring_else_statement")
}
