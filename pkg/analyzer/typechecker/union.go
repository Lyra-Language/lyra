package typechecker

import (
	"github.com/Lyra-Language/lyra/pkg/ast"
	diag "github.com/Lyra-Language/lyra/pkg/diagnostic"
	"github.com/Lyra-Language/lyra/pkg/types"
)

// A C union: one block of storage its members read several ways, declared for the FFI.
//
// The typechecker owes three things a struct does not need, and each is the same fact
// seen from a different side — **a union does not record which member is live**:
//
//   - a literal names exactly **one** member, since that is how many a union holds;
//   - reading a member is **`unsafe`**, since any member but the one written is a
//     reinterpretation of bytes;
//   - every member must be **FFI-safe**, which is what a union is for and what makes it
//     sound for the ownership walk to say a union owns nothing.

// inferUnionInstanceExpr checks `Ev { kind: 7 }`.
//
// **Exactly one member, and that is the rule rather than a restriction.** Naming two
// would be writing two values to one place and keeping the second, so the literal would
// quietly discard whichever the reader wrote first; naming none leaves nothing to say
// what the bytes mean. The rest of the storage is zeroed by the backend, so a union is
// deterministic from the moment it exists — C leaves it indeterminate, and inheriting
// that would make a program's behaviour depend on stack residue.
func (tc *TypeChecker) inferUnionInstanceExpr(expr *ast.StructInstanceExpr, ut types.UnionType) types.Type {
	if expr.BaseStruct != nil {
		tc.addErrorCode(expr.GetLocation(), SeverityError, diag.CodeMalformedUnion,
			"union %s: there is no update form for a union — an update copies the "+
				"fields it is not given, and a union has no other member to copy",
			ut.Name)
		return nil
	}
	if len(expr.Fields) != 1 {
		tc.addErrorCode(expr.GetLocation(), SeverityError, diag.CodeMalformedUnion,
			"union %s: a union literal names exactly one member, got %d — a union "+
				"holds one member at a time, so naming several would write them to the "+
				"same bytes and keep only the last",
			ut.Name, len(expr.Fields))
		return nil
	}

	field := expr.Fields[0]
	member, ok := ut.MemberByName(field.Name)
	if !ok {
		tc.addError(expr.GetLocation(), SeverityError,
			"union %s has no member %q", ut.Name, field.Name)
		return nil
	}
	declared := tc.resolveType(member.Type, expr.GetLocation())
	if valueType := tc.inferExprType(field.Value); valueType != nil {
		tc.propagateExpectedType(field.Value, declared)
		if !isAssignable(tc.inferExprType(field.Value), declared) {
			tc.addError(field.Value.GetLocation(), SeverityError,
				"union %s: member %q is %s, got %s",
				ut.Name, field.Name, declared, valueType)
		}
	}
	tc.typeTable.Set(expr, ut)
	return ut
}

// checkUnionMembersAreFFISafe holds a union's members to the rule an `extern`
// signature's types are held to (lyra-E063's predicate, reported as lyra-E071).
//
// **Two reasons, and the second is a soundness one.** A union exists to name a C type,
// so a member with no C spelling names nothing. And every pass that walks a value's
// components relies on it: `eachComponent` yields *nothing* for a union, because its
// members alias one another and releasing a member that was never written would free
// whatever bytes happen to be there. That is only safe while no member can be managed,
// which is exactly what FFI-safety guarantees — no FFI-safe type is refcounted.
func (tc *TypeChecker) checkUnionMembersAreFFISafe(decl *ast.TypeDeclStmt, ut types.UnionType) {
	for _, m := range ut.Members {
		resolved := tc.resolveTypeIfKnown(m.Type, decl.GetLocation())
		if tc.hasCLayout(resolved, map[string]bool{ut.Name: true}) {
			continue
		}
		tc.addErrorCode(decl.NameLocation, SeverityError, diag.CodeNotFFISafeUnion,
			"union %s: member %q is %s, which has no C representation. A union names a "+
				"C type, so its members are held to the same rule an `extern` signature "+
				"is: the scalars, a raw pointer, `void`, and arrays, structs and unions "+
				"of those",
			ut.Name, m.Name, resolved)
	}
}

// inferUnionMemberRead types `e.kind` where `e` is a union, and requires the enclosing
// `unsafe` that a struct field read does not.
//
// **This is the reason a union is its own declaration rather than an attribute on
// `struct`.** The read is unchecked in a way no other member access in the language is:
// nothing records which member was last written, so reading any other one reinterprets
// whatever bytes are there — an `i64` read of a union last written as a `u32` picks up
// four bytes of neighbouring storage. Behind `@union` this would have been spelled
// exactly like a safe struct field access, and the language has already refused that
// trade once, when it kept pointer arithmetic a named method rather than `p[i]`.
//
// It is lyra-E011, the same diagnostic `&x` and a deref draw, because it is the same
// claim: *the compiler cannot check this and you are asserting it holds.*
func (tc *TypeChecker) inferUnionMemberRead(m *ast.MemberExpr, ut types.UnionType, name string) types.Type {
	member, ok := ut.MemberByName(name)
	if !ok {
		tc.addError(m.GetLocation(), SeverityError, "union %s has no member %q", ut.Name, name)
		return nil
	}
	if !tc.inUnsafe {
		tc.addErrorCode(m.GetLocation(), SeverityError, diag.CodeUnsafeOutsideUnsafe,
			"reading union member %q requires an `unsafe` block or function: a union "+
				"does not record which member is live, so reading one that was not "+
				"written reinterprets whatever bytes are there",
			name)
	}
	t := tc.resolveTypeIfKnown(member.Type, m.GetLocation())
	tc.typeTable.Set(m, t)
	return t
}

// hasCLayout reports whether a type has a C representation *as storage* — which is a
// wider question than lyra-E063's, and the difference is worth being precise about.
//
// **lyra-E063 asks whether a type can cross a function boundary by value**, and refuses
// every aggregate because that needs a per-target calling-convention classifier LLVM
// does not supply (todo.md, Struct-by-value). **This asks whether a type has a layout**,
// and the answer for aggregates is yes: `TestExec_FFIFixture_StructLayoutMatchesC` proves
// Lyra's structs agree with C's `sizeof` and every `offsetof`, and that proof is what a
// union member rests on.
//
// So a union member may be an array or a struct — which it must be, since that is what a
// C union is made of: `SDL_Event` is a `Uint32`, several structs, and a `Uint8[128]`.
// What is still refused is anything with no C storage at all: a `string` or dynamic array
// (a refcounted box with a header), a closure (`{code, env}`), a `data` type (a tag Lyra
// invented), and anything `shared` or `weak`.
//
// `seen` breaks a cycle. A union containing itself has no finite size, and the recursive-
// type checker reports that with its own message; answering here rather than recursing
// forever is what lets it get that far.
func (tc *TypeChecker) hasCLayout(t types.Type, seen map[string]bool) bool {
	switch v := types.StripNewtype(t).(type) {
	case types.RawPointerType:
		return true
	case types.PrimitiveType:
		// Deliberately not `isFFISafe`'s exact set: `void` is a return type, not
		// storage, so a `void` member is refused here and admitted there.
		return isAnyConcreteInt(v.Name) || isAnyConcreteFloat(v.Name) ||
			v.Name == types.Rune || v.Name == types.Boolean
	case types.StaticArrayType:
		return tc.hasCLayout(tc.resolveTypeIfKnown(v.ElementType, ast.Location{}), seen)
	case types.NamedStructType:
		if seen[v.Name] {
			return true
		}
		seen[v.Name] = true
		for _, f := range v.Fields {
			if !tc.hasCLayout(tc.resolveTypeIfKnown(f.Type, ast.Location{}), seen) {
				return false
			}
		}
		return true
	case types.UnionType:
		if seen[v.Name] {
			return true
		}
		seen[v.Name] = true
		for _, m := range v.Members {
			if !tc.hasCLayout(tc.resolveTypeIfKnown(m.Type, ast.Location{}), seen) {
				return false
			}
		}
		return true
	}
	return false
}

// unionOperand returns the union among two operand types, or nil if neither is one.
// Either side is enough: a union compared with anything is refused, and naming the union
// is what makes the message say why.
func unionOperand(a, b types.Type) *types.UnionType {
	if ut, ok := types.StripNewtype(a).(types.UnionType); ok {
		return &ut
	}
	if ut, ok := types.StripNewtype(b).(types.UnionType); ok {
		return &ut
	}
	return nil
}
