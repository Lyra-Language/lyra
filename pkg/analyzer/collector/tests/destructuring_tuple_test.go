package collector_test

import "testing"

func TestCollector_DestructuringTuple(t *testing.T) {
	runGoldenTest(t, `let (x, y, z) = ThreeInts(1, 2, 3)`, "destructuring_tuple")
}

func TestCollector_DestructuringTupleFromIdentifier(t *testing.T) {
	runGoldenTest(t, `let (x, y, z) = some_tuple`, "destructuring_tuple_from_identifier")
}

func TestCollector_DestructuringTupleWithRestAtBeginning(t *testing.T) {
	runGoldenTest(t, `let (...rest, x, y, z) = some_tuple`, "destructuring_tuple_with_rest_at_beginning")
}

func TestCollector_DestructuringTupleWithRestInMiddle(t *testing.T) {
	runGoldenTest(t, `let (x, y, ...rest, z) = some_tuple`, "destructuring_tuple_with_rest_in_middle")
}

func TestCollector_DestructuringTupleWithRestAtEnd(t *testing.T) {
	runGoldenTest(t, `let (x, y, z, ...rest) = some_tuple`, "destructuring_tuple_with_rest_at_end")
}
