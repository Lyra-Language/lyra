package collector_test

import (
	"path/filepath"
	"testing"
)

func TestCollectMathAssignOpExpr(t *testing.T) {
	source := `
	var x = 5
	x += 3
	x -= 1
	x *= 3
	x /= 3
	`
	program, _ := parseAndCollect(t, source)
	got := captureProgramPrint(program)
	checkGolden(t, got, filepath.Join("testdata", "math_assign_op_expr.golden"))
}
