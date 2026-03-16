package collector_test

import (
	"path/filepath"
	"testing"
)

func TestCollectSimpleIfBlockExpr(t *testing.T) {
	source := `
	if x == 3 {
		println("x is 3")
	}`
	program, _ := parseAndCollect(t, source)
	got := captureProgramPrint(program)
	checkGolden(t, got, filepath.Join("testdata", "simple_if_block_expr.golden"))
}

func TestCollectIfBlockExprWithElse(t *testing.T) {
	source := `
	if x == 3 {
		println("x is 3")
	} else {
		println("x is not 3")
	}`
	program, _ := parseAndCollect(t, source)
	got := captureProgramPrint(program)
	checkGolden(t, got, filepath.Join("testdata", "if_block_expr_with_else.golden"))
}

func TestCollectIfBlockExprWithElseIf(t *testing.T) {
	source := `
	if x == 3 {
		println("x is 3")
	} else if x == 4 {
		println("x is 4")
	} else {
		println("x is not 3 or 4")
	}`
	program, _ := parseAndCollect(t, source)
	got := captureProgramPrint(program)
	checkGolden(t, got, filepath.Join("testdata", "if_block_expr_with_else_if.golden"))
}

func TestCollectNestedIfBlockExpr(t *testing.T) {
	source := `
	if x == 0 {
		if y == 0 {
			println("At Origin")
		} else {
			println("On Vertical Axis")
		}
	} else {
		if y == 0 {
			println("On Horizontal Axis")
		} else {
			println("At #{x},#{y}")
		}
	}`
	program, _ := parseAndCollect(t, source)
	got := captureProgramPrint(program)
	checkGolden(t, got, filepath.Join("testdata", "nested_if_block_expr.golden"))
}

func TestIfThenExpr(t *testing.T) {
	source := `let foo = if x == 0 then "zero" else "not zero" end`
	program, _ := parseAndCollect(t, source)
	got := captureProgramPrint(program)
	checkGolden(t, got, filepath.Join("testdata", "if_then_expr.golden"))
}

func TestIfThenExprWithElseIf(t *testing.T) {
	source := `let foo = if x == 0 then "zero" else if x == 1 then "one" else "not zero or one" end`
	program, _ := parseAndCollect(t, source)
	got := captureProgramPrint(program)
	checkGolden(t, got, filepath.Join("testdata", "if_then_expr_with_else_if.golden"))
}

func TestIfThenExprMultipleLines(t *testing.T) {
	source := `let foo = if x == 0 then
		"zero"
	else if x == 1 then
		"one"
	else
		"not zero or one"
	end`
	program, _ := parseAndCollect(t, source)
	got := captureProgramPrint(program)
	checkGolden(t, got, filepath.Join("testdata", "if_then_expr_multiple_lines.golden"))
}
