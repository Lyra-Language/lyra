package collector_test

import "testing"

func TestCollector_RangeExpression(t *testing.T) {
	runGoldenTest(t, `0..<10`, "simple_range_expression")
}

func TestCollector_RangeExpressionWithStep(t *testing.T) {
	runGoldenTest(t, `0..=10:2`, "range_expression_with_step")
}

func TestCollector_RangeExpressionWithStartExpression(t *testing.T) {
	runGoldenTest(t, `start*2..=end-1`, "range_expression_with_start_expression")
}
