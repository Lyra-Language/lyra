package typechecker_test

import (
	"testing"

	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/types"
)

// A `shared [N]T` is a fixed-size array carrying the `shared` storage flavor (a
// heap-boxed, ref-counted array in the backend). The front-end accepts it in the
// usual positions and stamps the flavor onto the array-literal construction node so
// the backend knows to box it.

func TestTypeCheck_SharedArray_Accepted(t *testing.T) {
	cases := []string{
		`let xs: shared [3]i64 = [1, 2, 3]`,
		`let xs: shared [3]u8 = [10, 20, 30]
let y: u8 = xs[1]`,
		`let third = (xs: shared [3]i64) -> i64 => xs[2]`,
		`let make = () -> shared [3]i64 => [7, 8, 9]`,
		`let xs: shared [2]string = ["a", "b"]`,
	}
	for _, src := range cases {
		res := parseCollectAndCheck(t, src, false)
		assertNoErrors(t, res)
	}
}

// The `shared` flavor from the annotation is stamped onto the array-literal
// construction node (propagateExpectedType), which is what tells the backend to
// heap-box it rather than build an inline value.
func TestTypeCheck_SharedArray_StampsConstructionFlavor(t *testing.T) {
	res := parseCollectAndCheck(t, `let xs: shared [3]i64 = [1, 2, 3]`, false)
	assertNoErrors(t, res)

	var arrType types.Type
	onExpr := func(e ast.Expression) bool {
		if _, ok := e.(*ast.ArrayLiteralExpr); ok {
			if ty, ok := res.typeTable.Get(e); ok {
				arrType = ty
			}
		}
		return true
	}
	for _, s := range res.program.Statements {
		if st, ok := s.(ast.Statement); ok {
			ast.WalkStmt(st, func(ast.Statement) bool { return true }, onExpr)
		}
	}
	if arrType == nil {
		t.Fatal("no recorded type for the array literal")
	}
	if !types.IsStaticArray(arrType) {
		t.Fatalf("expected a static array type, got %s", arrType)
	}
	if got := types.AllocationOf(arrType); got != types.Shared {
		t.Errorf("expected the array literal stamped `shared`, got alloc=%q (type %s)", got, arrType)
	}
}
