package collector_test

import "testing"

func TestCollectNullCoalescingExpression(t *testing.T) {
	runGoldenTest(t, `let result = optional ?? default`, "null_coalescing_expression")
}
