package collector_test

import (
	"testing"

	"github.com/Lyra-Language/lyra/pkg/ast"
)

// A trailing comment inside a block is a named CST child that collects to nil;
// it must be skipped, not appended as a nil statement (which would become the
// block's value and miscompile). Regression guard.
func TestCollect_Block_TrailingComment_NoNilStatement(t *testing.T) {
	src := `let f = () -> u8 => {
  let a: u8 = 40
  let b: u8 = 2
  a + b // trailing comment
}`
	program, _, _, errs := parseAndCollect(t, src)
	if len(errs) > 0 {
		t.Fatalf("collector errors: %v", errs)
	}
	block := program.Statements[0].(*ast.VarDeclStmt).Value.(*ast.LambdaExpr).Body.(*ast.BlockExpr)
	if got := len(block.Statements); got != 3 {
		t.Fatalf("expected 3 statements (2 lets + the value), got %d", got)
	}
	for i, s := range block.Statements {
		if s == nil {
			t.Fatalf("statement %d is nil", i)
		}
	}
	// The block's value is the final expression, not a dropped/nil statement.
	last, ok := block.Statements[2].(*ast.ExpressionStmt)
	if !ok {
		t.Fatalf("expected the last statement to be the value expression, got %T", block.Statements[2])
	}
	if _, ok := last.Expression.(*ast.MathBinaryOpExpr); !ok {
		t.Errorf("expected the block value to be `a + b`, got %T", last.Expression)
	}
}
