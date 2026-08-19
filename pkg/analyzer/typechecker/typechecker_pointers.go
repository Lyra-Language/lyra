package typechecker

import (
	"github.com/Lyra-Language/lyra/pkg/ast"
	diag "github.com/Lyra-Language/lyra/pkg/diagnostic"
	"github.com/Lyra-Language/lyra/pkg/types"
)

// Raw pointers: `&x`, `&mut x`, `p^`, `p^ = v`, and the `unsafe { … }` block they must
// sit inside.
//
// The type (`types.RawPointerType`), the grammar, the collector and E011's unsafe-context
// policy all predate this by months; what was missing was the two ends — nothing inferred
// these expressions and nothing lowered them, so all four forms were refused at the
// expression as lyra-E051. This file is the inference end.
//
// # Mutability is the pointer's, and it is checked where the pointer is taken
//
// `&x` yields `^T` and `&mut x` yields `^mut T`, and only the second permits a write
// through it. That is checked twice, deliberately, because the two checks answer different
// questions at different places: taking `&mut x` requires **x** to be mutable (the binding
// rule every interior mutation obeys), and writing `p^ = v` requires **p** to be `^mut`
// (the pointer's own type). Neither implies the other — a `^mut T` can be copied into a
// `let`, and a `^T` can be taken of a `var` — so a program that skipped either would be
// able to write through a pointer it was never allowed to take.
//
// # What is deliberately *not* here
//
// Pointer arithmetic, comparison, null, and any way to make a pointer other than `&`.
// A raw pointer in Lyra today addresses a binding that exists; producing one from an
// integer is a separate feature with its own safety story, and adding it silently as a
// consequence of `^T` being a type would be exactly the phantom surface this file's
// history is about.

// inferAddressOf types `&x` / `&mut x`.
func (tc *TypeChecker) inferAddressOf(e *ast.AddressOfExpr) types.Type {
	if e.Operand == nil {
		return nil
	}
	// **Only storage has an address.** `&f()` would name a temporary that stops existing
	// at the end of the statement, so the pointer would dangle immediately — which is a
	// compile-time fact, not a thing to leave to the reader.
	if !isAddressable(e.Operand) {
		tc.addErrorCode(e.GetLocation(), SeverityError, diag.CodeNotAddressable,
			"cannot take the address of a temporary — `&` needs a binding, a field or an "+
				"element, and this expression names none")
		return nil
	}
	operand := tc.inferExprType(e.Operand)
	if operand == nil {
		return nil
	}
	if e.IsMut {
		tc.requireMutableRoot(e.Operand, "&mut")
	}
	return types.RawPointerType{Pointee: operand, IsMut: e.IsMut}
}

// inferDeref types `p^`.
func (tc *TypeChecker) inferDeref(e *ast.DerefExpr) types.Type {
	if e.Operand == nil {
		return nil
	}
	operand := tc.inferExprType(e.Operand)
	if operand == nil {
		return nil
	}
	ptr, ok := types.StripNewtype(operand).(types.RawPointerType)
	if !ok {
		tc.addErrorCode(e.GetLocation(), SeverityError, diag.CodeNotAPointer,
			"cannot dereference %s with `^`: it is not a raw pointer", operand)
		return nil
	}
	return ptr.Pointee
}

// checkDerefWrite type-checks `p^ = v`.
//
// The target is the pointer, not the place it points at, which is why the mutability
// question here is about the *pointer's type* rather than about a binding: a `^T` names a
// place it may read and not write, however the binding holding it was declared.
func (tc *TypeChecker) checkDerefWrite(stmt *ast.DerefAssignmentStmt) {
	target := tc.inferExprType(stmt.Target.Operand)
	if target == nil {
		return
	}
	ptr, ok := types.StripNewtype(target).(types.RawPointerType)
	if !ok {
		tc.addErrorCode(stmt.GetLocation(), SeverityError, diag.CodeNotAPointer,
			"cannot write through %s with `^`: it is not a raw pointer", target)
		return
	}
	if !ptr.IsMut {
		tc.addErrorCode(stmt.GetLocation(), SeverityError, diag.CodeImmutablePointerWrite,
			"cannot write through %s: it is a read-only pointer — take it with `&mut` to "+
				"write through it", target)
		return
	}
	value := tc.inferExprType(stmt.Value)
	if value == nil {
		return
	}
	if !tc.assignableValue(stmt.Value, value, ptr.Pointee) {
		tc.addError(stmt.GetLocation(), SeverityError,
			"cannot assign %s through a pointer to %s", value, ptr.Pointee)
		return
	}
	tc.propagateLiteralType(stmt.Value, ptr.Pointee)
}

// isAddressable reports whether e names storage rather than a temporary — an identifier,
// or a field/element path rooted at one. The same shape the backend's isLValuePath tests,
// and the same one rootIdentifier walks.
func isAddressable(e ast.Expression) bool {
	return rootIdentifier(e) != nil
}

// requireMutableRoot enforces that `&mut x` is taken of something the program may mutate,
// reusing the binding rule every interior mutation obeys — a `var` or a `let mut`, a
// `mut`/`own` parameter, never a `const`.
//
// Written against the same three cases checkLValueAssignment walks, and in the same order,
// because "may this be mutated" must have one answer: a `&mut` that outran the assignment
// rule would be a way to mutate a `let` by taking a pointer to it first.
func (tc *TypeChecker) requireMutableRoot(target ast.Expression, what string) {
	root := rootIdentifier(target)
	if root == nil {
		return
	}
	if root.IsConst {
		tc.addImmutableBindingError(root.GetLocation(), root.Name, ast.BindingConst)
		return
	}
	if mod, ok := tc.paramMods[root.Name]; ok {
		if !paramAllowsInteriorMutation(mod) {
			tc.addParamImmutableError(root.GetLocation(), root.Name, mod)
		}
		return
	}
	if sym, ok := tc.scope.Lookup(root.Name); ok {
		if decl, ok := sym.(*ast.VarDeclStmt); ok && !decl.CanMutateInterior() {
			tc.addInteriorImmutableError(root.GetLocation(), root.Name, decl.BindingKind)
		}
	}
}
