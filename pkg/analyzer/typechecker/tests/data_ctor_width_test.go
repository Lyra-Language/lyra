package typechecker_test

import (
	"testing"

	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/types"
)

// A data constructor's argument takes its declared payload-field width
// (context-directed literal-width propagation), the same treatment struct fields,
// tuple elements, and call arguments get — instead of promoting to the i64
// default. Without this a narrow payload's literal would lower at i64 and mismatch
// the variant's field type in the backend's tagged-union construction.
func TestDataConstructor_ArgTakesDeclaredFieldWidth(t *testing.T) {
	res := parseCollectAndCheck(t, `
data Byte = Zero | Wrap(u8)
let b = Wrap(200)
`, false)
	assertNoErrors(t, res)

	// Find the `200` constructor argument and check it recorded as u8.
	var arg ast.Expression
	for _, s := range res.program.Statements {
		vd, ok := s.(*ast.VarDeclStmt)
		if !ok || vd.Name != "b" {
			continue
		}
		if lit, ok := vd.Value.(*ast.TupleLiteralExpr); ok && len(lit.Elements) == 1 {
			arg = lit.Elements[0]
		}
	}
	if arg == nil {
		t.Fatal("could not find the constructor argument expression")
	}
	got, ok := res.typeTable.Get(arg)
	if !ok {
		t.Fatal("constructor argument has no recorded type")
	}
	p, ok := got.(types.PrimitiveType)
	if !ok || p.Name != types.UInt8 {
		t.Errorf("constructor argument recorded as %v; want u8", got)
	}
}
