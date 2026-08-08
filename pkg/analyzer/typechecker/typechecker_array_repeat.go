package typechecker

import (
	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/types"
)

// The array-repeat literal, `[0; 5]` — five zeros, as a `[5]i64`.
//
// It parsed and collected from the beginning and **nothing downstream read it**: the
// typechecker reported `unknown expression type "[0; 5]"`, which is loud rather than
// silent, so it was an unimplemented feature rather than a phantom. Found 08/07 by the
// AST sweep (`ArrayRepeatExpr.Count` had no consumer at all) and implemented 08/08.
//
// **The count is a compile-time constant by construction**, not by a check here: the
// grammar admits only a number literal or a `const_identifier` in that position
// (`array_repeat_count`), so `[0; n]` for a runtime `n` is a syntax error. That is the
// right place for the restriction — the size is part of the *type*, and a type cannot
// depend on a value the compiler has not got.

// inferArrayRepeatType types `[v; n]` as `[n]T` where T is v's type.
//
// The element type is left **untyped** where v is an untyped literal, exactly as
// inferArrayLiteralType leaves its elements untyped, so `let g: [4]u8 = [0; 4]` narrows
// the 0 to u8 instead of lowering it at the i64 default and mismatching the annotation.
// propagateLiteralType has the matching arm.
func (tc *TypeChecker) inferArrayRepeatType(expr *ast.ArrayRepeatExpr) types.Type {
	elem := tc.inferExprType(expr.Value)
	count, ok := tc.arrayRepeatCount(expr)
	if !ok || elem == nil {
		return nil
	}
	return types.StaticArrayType{ElementType: elem, Size: count}
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
		if id, isIdent := count.(*ast.IdentifierExpr); isIdent {
			tc.addError(count.GetLocation(), SeverityError,
				"array repeat count %s must be a `const` whose value is a compile-time integer", id.Name)
		} else {
			tc.addError(count.GetLocation(), SeverityError,
				"array repeat count must be a compile-time constant")
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
