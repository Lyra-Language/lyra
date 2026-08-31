package ast_test

import (
	"testing"

	"github.com/Lyra-Language/lyra/pkg/ast"
)

// The whole point of TrueNil is that `expr != nil` lies about a typed nil, so the test has
// to assert against the thing that lies. `want` is what a later pass's `if x == nil` sees.
func TestTrueNilMapsATypedNilToANilInterface(t *testing.T) {
	t.Parallel()

	// The shape the compiler crashed on: a collector reporting an error and returning a nil
	// concrete pointer, which Go converts to a *non-nil* interface at the return.
	var missing *ast.CharacterLiteralExpr
	var asInterface ast.Expression = missing
	if asInterface == nil {
		t.Fatal("a typed nil should compare non-nil; if it does not, this test proves nothing")
	}
	if ast.TrueNil(asInterface) != nil {
		t.Error("TrueNil should map a typed nil to a nil interface")
	}

	// The statement and pattern boundaries are the same conversion.
	var stmt ast.Statement = (*ast.ImportStmt)(nil)
	if ast.TrueNil(stmt) != nil {
		t.Error("TrueNil should map a typed nil statement to a nil interface")
	}
	var pat ast.Pattern = (*ast.IdentifierPattern)(nil)
	if ast.TrueNil(pat) != nil {
		t.Error("TrueNil should map a typed nil pattern to a nil interface")
	}
}

// A real node must come back untouched — a guard that swallowed its input would empty every
// program it was asked about, and every test above would still pass.
func TestTrueNilPassesRealNodesThrough(t *testing.T) {
	t.Parallel()
	node := &ast.CharacterLiteralExpr{Value: 'x'}
	var expr ast.Expression = node
	got := ast.TrueNil(expr)
	if got == nil {
		t.Fatal("TrueNil dropped a real node")
	}
	if got.(*ast.CharacterLiteralExpr) != node {
		t.Error("TrueNil should return the same node it was given")
	}
	// A nil interface going in is already the answer, and must not panic on the way through.
	var empty ast.Expression
	if ast.TrueNil(empty) != nil {
		t.Error("a nil interface should stay nil")
	}
}
