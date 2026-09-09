package typechecker

import (
	"github.com/Lyra-Language/lyra/pkg/ast"
	diag "github.com/Lyra-Language/lyra/pkg/diagnostic"
	"github.com/Lyra-Language/lyra/pkg/types"
)

// `nullptr` is an untyped literal whose context supplies the pointee, and this is the
// pass that reports the ones no context reached.
//
// **A sweep rather than a check at each site**, which is the opposite of how the
// integer literals' width is decided, and for a reason that only applies here. An
// integer literal has a default, so every site can settle its own leaf and be done; a
// `nullptr` is settled by *whichever* of several contexts happens to reach it, and no
// one site knows whether another already did. Asking after the statement is finished is
// the only point at which "unpinned" means what it says.

// checkUnpinnedNullPtrs reports every `nullptr` in stmt that propagateExpected left
// without a pointer type (lyra-E069).
func (tc *TypeChecker) checkUnpinnedNullPtrs(node ast.AstNode) {
	onExpr := func(e ast.Expression) bool {
		np, ok := e.(*ast.NullPtrExpr)
		if !ok {
			return true
		}
		if tc.nullPtrIsPinned(np) {
			return true
		}
		tc.addErrorCode(np.GetLocation(), SeverityError, diag.CodeUnpinnedNullPtr,
			"nullptr: cannot tell what this points at. Write the pointer type "+
				"(`let p: ^u8 = nullptr`), or compare against a value that has one "+
				"(`p == nullptr`)")
		return true
	}
	// A top-level statement list is `[]ast.AstNode`, so the walk entry is whichever of
	// the two kinds this node actually is.
	switch n := node.(type) {
	case ast.Statement:
		ast.WalkStmt(n, nil, onExpr)
	case ast.Expression:
		ast.WalkExpr(n, nil, onExpr)
	}
}

// nullPtrIsPinned reports whether a context recorded a pointer type on this literal.
//
// A newtype over a pointer is stripped first: a `newtype Window = ^u8` context records
// the wrapper on the leaf (propagateExpected puts it back on the root, as it does for
// every base), and the pointee is underneath it. Refusing that would refuse the shape
// `std.ffi` recommends for a C handle.
func (tc *TypeChecker) nullPtrIsPinned(np *ast.NullPtrExpr) bool {
	t, ok := tc.typeTable.Get(np)
	if !ok {
		return false
	}
	resolved := tc.resolveTypeIfKnown(t, np.GetLocation())
	_, isPtr := types.StripNewtype(resolved).(types.RawPointerType)
	return isPtr
}
