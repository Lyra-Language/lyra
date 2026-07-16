package typechecker_test

import (
	"testing"

	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/types"
)

// A struct literal's field value gets the declared field's width pushed onto it
// (context-directed literal-width propagation), the same treatment a named-tuple
// element and a call argument get. Without this a narrow field's literal stays
// untyped_int and a width-sensitive consumer (the LLVM backend) would lower it at
// the i64 default and mismatch the struct's field type.
func TestStructInstance_FieldLiteralTakesDeclaredWidth(t *testing.T) {
	res := parseCollectAndCheck(t, `
struct Node {
  value: u8,
}
let n = Node { value: 3 }
`, false)
	assertNoErrors(t, res)

	// Find the `3` field-value literal and check its recorded type is u8, not the
	// untyped_int / i64 default.
	var lit ast.Expression
	for _, s := range res.program.Statements {
		vd, ok := s.(*ast.VarDeclStmt)
		if !ok {
			continue
		}
		si, ok := vd.Value.(*ast.StructInstanceExpr)
		if !ok {
			continue
		}
		for _, f := range si.Fields {
			if f.Name == "value" {
				lit = f.Value
			}
		}
	}
	if lit == nil {
		t.Fatal("could not find the struct field value expression")
	}
	got, ok := res.typeTable.Get(lit)
	if !ok {
		t.Fatal("field value has no recorded type")
	}
	p, ok := got.(types.PrimitiveType)
	if !ok || p.Name != types.UInt8 {
		t.Errorf("field value recorded as %v; want u8", got)
	}
}
