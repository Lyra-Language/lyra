package collector

import (
	"path/filepath"
	"testing"
)

func TestCollector_SimpleAnonymousTupleLiteral(t *testing.T) {
	source := `let point: (int, int) = (1, 2)`
	program, _ := parseAndCollect(t, source)
	got := captureProgramPrint(program)
	checkGolden(t, got, filepath.Join("testdata", "simple_anonymous_tuple_literal.golden"))
}

func TestCollector_SimpleNamedTupleLiteral(t *testing.T) {
	source := `let point: Point = (1, 2)`
	program, _ := parseAndCollect(t, source)
	got := captureProgramPrint(program)
	checkGolden(t, got, filepath.Join("testdata", "simple_named_tuple_literal.golden"))
}

func TestCollector_SimpleNamedTupleLiteralWithGenericParameters(t *testing.T) {
	source := `let point = Point::<i32>(1, 2)`
	program, _ := parseAndCollect(t, source)
	got := captureProgramPrint(program)
	checkGolden(t, got, filepath.Join("testdata", "simple_named_tuple_literal_with_generic_parameters.golden"))
}
