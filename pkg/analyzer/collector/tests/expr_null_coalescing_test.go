package collector_test

import (
	"path/filepath"
	"testing"
)

func TestCollectNullCoalescingExpression(t *testing.T) {
	source := `let result = optional ?? default`
	program, _ := parseAndCollect(t, source)
	got := captureProgramPrint(program)
	checkGolden(t, got, filepath.Join("testdata", "null_coalescing_expression.golden"))
}