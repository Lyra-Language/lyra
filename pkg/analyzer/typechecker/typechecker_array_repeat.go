package typechecker

import (
	"github.com/Lyra-Language/lyra/pkg/ast"
	diag "github.com/Lyra-Language/lyra/pkg/diagnostic"
	"github.com/Lyra-Language/lyra/pkg/types"
)

// The array-repeat literal, `[0; 5]` — five zeros, as a `[5]i64`.
//
// It parsed and collected from the beginning and **nothing downstream read it**: the
// typechecker reported `unknown expression type "[0; 5]"`, which is loud rather than
// silent, so it was an unimplemented feature rather than a phantom. Found 08/07 by the
// AST sweep (`ArrayRepeatExpr.Count` had no consumer at all) and implemented 08/08.
//
// **The count is a compile-time constant only where the *type* needs it to be** (08/14).
// A fixed array carries its size in its type, so `[3]T` cannot depend on a value the
// compiler has not got — but a `[]T` carries its length at run time and needs nothing
// static. The grammar used to admit only a literal or a `const_identifier` here, which was
// right for the first and inherited rather than reasoned for the second: a buffer sized by
// a window resize (`let buf: []u32 = [0; n]`) was a *syntax* error. The grammar now accepts
// any expression and the rule lives here, where the two cases can be told apart.

// inferArrayRepeatType types `[v; n]` as `[n]T` where T is v's type — or as `[]T` when the
// count is not a compile-time constant.
//
// **A runtime count has exactly one sound answer**, which is why the fall-through is not a
// silent choice between two things: no fixed type can describe it, so `[]T` is not a
// preference but the only inhabitable option. That parallels `[1, 2, 3]` inferring `[3]T`
// with no context — each form infers the one type it can have, and an annotation may still
// widen a fixed one to dynamic.
//
// The element type is left **untyped** where v is an untyped literal, exactly as
// inferArrayLiteralType leaves its elements untyped, so `let g: [4]u8 = [0; 4]` narrows
// the 0 to u8 instead of lowering it at the i64 default and mismatching the annotation.
// propagateLiteralType has the matching arm.
func (tc *TypeChecker) inferArrayRepeatType(expr *ast.ArrayRepeatExpr) types.Type {
	elem := tc.inferExprType(expr.Value)
	if elem == nil {
		return nil
	}
	if !tc.arrayRepeatCountIsConstant(expr) {
		if !tc.checkRuntimeRepeatCount(expr) {
			return nil
		}
		return types.DynamicArrayType{ElementType: elem}
	}
	count, ok := tc.arrayRepeatCount(expr)
	if !ok {
		return nil
	}
	return types.StaticArrayType{ElementType: elem, Size: count}
}

// arrayRepeatCountIsConstant reports whether the count folds, without reporting anything.
// The quiet twin of arrayRepeatCount, so inference can *choose* between the fixed and
// dynamic readings instead of erroring on the way to one of them.
func (tc *TypeChecker) arrayRepeatCountIsConstant(expr *ast.ArrayRepeatExpr) bool {
	if expr.Count == nil {
		return false
	}
	_, ok := ast.ArrayRepeatCount(expr.Count, tc.constInitializer)
	return ok
}

// checkRuntimeRepeatCount verifies that a non-constant count is at least an integer.
//
// Nothing else does: the constant path proves it by folding, and without this a
// `[0; "three"]` would reach the backend as a dynamic array whose length is a string.
func (tc *TypeChecker) checkRuntimeRepeatCount(expr *ast.ArrayRepeatExpr) bool {
	countType := tc.inferExprType(expr.Count)
	if countType == nil {
		return false
	}
	// Untyped included: `[0; n]` where n came from an unannotated literal is an
	// integer that has not been pinned yet, and refusing it here would refuse the
	// commonest spelling.
	if p, ok := types.StripNewtype(countType).(types.PrimitiveType); ok &&
		(isAnyConcreteInt(p.Name) || p.Name == types.UntypedInt || p.Name == types.UntypedSignedInt) {
		return true
	}
	tc.addError(expr.Count.GetLocation(), SeverityError,
		"array repeat count must be an integer, got %s", countType)
	return false
}

// arrayRepeatCount folds the count to a non-negative int **and rewrites the AST**,
// replacing a `const_identifier` count with the literal it folded to.
//
// Two forms reach it, because two are what the grammar admits: a number literal, and a
// `const_identifier` resolved through its binding. The rewrite is what keeps the second
// from becoming everyone's problem — the backend needs the same number, and without it
// codegen would need its own const lookup, i.e. a second copy of "what does this name
// mean" living where the symbol table is least at hand. Rule 10's advice, applied
// upward: one rewrite here beats teaching every later pass the same resolution, and it
// is what `desugarUFCSCall` does for a receiver.
//
// Idempotent, because inference and propagation both ask: a count that is already a
// literal is left alone.
func (tc *TypeChecker) arrayRepeatCount(expr *ast.ArrayRepeatExpr) (int, bool) {
	count := expr.Count
	if count == nil {
		return 0, false
	}
	n, ok := ast.ArrayRepeatCount(count, tc.constInitializer)
	if !ok {
		// Only a **fixed** array reaches here now: inference reads a non-constant count
		// as `[]T`, so this fires when such a literal is used where a size is part of
		// the type. Naming the dynamic spelling is the whole message — the author has
		// written something with a perfectly good meaning, in the one position that
		// cannot hold it.
		if id, isIdent := count.(*ast.IdentifierExpr); isIdent {
			tc.addErrorCode(count.GetLocation(), SeverityError, diag.CodeNonConstantArraySize,
				"a fixed-size array's length is part of its type, so the count must be a compile-time constant; %s is not a `const` — annotate the binding as `[]T` for an array sized at run time",
				id.Name)
		} else {
			tc.addErrorCode(count.GetLocation(), SeverityError, diag.CodeNonConstantArraySize,
				"a fixed-size array's length is part of its type, so the count must be a compile-time constant — annotate the binding as `[]T` for an array sized at run time")
		}
		return 0, false
	}
	if n < 0 {
		tc.addError(count.GetLocation(), SeverityError,
			"array repeat count must not be negative, got %d", n)
		return 0, false
	}
	// A static array's size is an `int` field and the backend emits one element per
	// count, so an absurd size is a compile that never finishes rather than a program
	// that fails. Refused with a number rather than left to the machine.
	const maxRepeat = 1 << 20
	if n > maxRepeat {
		tc.addError(count.GetLocation(), SeverityError,
			"array repeat count %d is too large (the limit is %d elements)", n, maxRepeat)
		return 0, false
	}
	if _, isLit := count.(*ast.IntegerLiteralExpr); !isLit {
		expr.Count = &ast.IntegerLiteralExpr{
			ExprBase: ast.ExprBase{AstBase: ast.AstBase{Location: count.GetLocation()}},
			Value:    n,
			Base:     ast.IntegerBase10,
		}
	}
	return int(n), true
}

// constInitializer resolves a `const` binding's initializer, for ast.ArrayRepeatCount.
// It confirms the binding really is a `const`: a SCREAMING_CASE name that is not one
// must not silently become a size.
func (tc *TypeChecker) constInitializer(name string) (ast.Expression, bool) {
	sym, found := tc.scope.Lookup(name)
	if !found {
		return nil, false
	}
	decl, isVar := sym.(*ast.VarDeclStmt)
	if !isVar || !decl.IsConstant() {
		return nil, false
	}
	return decl.Value, true
}

// reportRuntimeRepeatInFixedContext reports lyra-E056 for `[v; n]` with a runtime count
// used where a **fixed** array is wanted, and says whether it did.
//
// Inference reads such a literal as `[]T` — the only type it can have — so without this
// the mismatch is an ordinary assignability failure naming two types, neither of which is
// the problem. The count is.
func (tc *TypeChecker) reportRuntimeRepeatInFixedContext(expr ast.Expression, want types.Type) bool {
	ar, isRepeat := expr.(*ast.ArrayRepeatExpr)
	if !isRepeat {
		return false
	}
	if _, wantsFixed := tc.resolveTypeIfKnown(want, expr.GetLocation()).(types.StaticArrayType); !wantsFixed {
		return false
	}
	if tc.arrayRepeatCountIsConstant(ar) {
		return false
	}
	_, ok := tc.arrayRepeatCount(ar) // emits lyra-E056
	return !ok
}
