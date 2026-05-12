package collector_test

import "testing"

func TestCollectDestructuringArray(t *testing.T) {
	runGoldenTest(t, `let [a, b, c] = some_array`, "destructuring_array")
}

func TestCollectDestructuringArrayWithVar(t *testing.T) {
	runGoldenTest(t, `var [a, b, c] = some_array`, "destructuring_array_with_var")
}

func TestCollectDestructuringArrayWithRestAtStart(t *testing.T) {
	runGoldenTest(t, `let [...head, a, b, c] = some_array`, "destructuring_array_with_rest_at_start")
}

func TestCollectDestructuringArrayWithRestInMiddle(t *testing.T) {
	runGoldenTest(t, `let [a, ...middle, c] = some_array`, "destructuring_array_with_rest_in_middle")
}

func TestCollectDestructuringArrayWithRestAtEnd(t *testing.T) {
	runGoldenTest(t, `let [a, b, c, ...tail] = some_array`, "destructuring_array_with_rest_at_end")
}
