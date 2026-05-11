package collector_test

import (
	"path/filepath"
	"testing"
)

func TestCollector_RangeExpression(t *testing.T) {
	source := `0..<10`
	program, _ := parseAndCollect(t, source)
	got := captureProgramPrint(program)
	checkGolden(t, got, filepath.Join("testdata", "simple_range_expression.golden"))
}

func TestCollector_RangeExpressionWithStep(t *testing.T) {
	source := `0..=10:2`
	program, _ := parseAndCollect(t, source)
	got := captureProgramPrint(program)
	checkGolden(t, got, filepath.Join("testdata", "range_expression_with_step.golden"))
}

func TestCollector_RangeExpressionWithStartExpression(t *testing.T) {
	source := `start*2..=end-1`
	program, _ := parseAndCollect(t, source)
	got := captureProgramPrint(program)
	checkGolden(t, got, filepath.Join("testdata", "range_expression_with_start_expression.golden"))
}
