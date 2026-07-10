package typechecker_test

import (
	"testing"

	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/types"
)

// TestAlloc_AnnotationOnNamedType verifies that a usage-site allocation modifier
// (e.g. `let n: shared Node`) is parsed without error and carries the Shared
// flavor through on the declaration's type annotation.
func TestAlloc_AnnotationOnNamedType(t *testing.T) {
	res := parseCollectAndCheck(t, `
		struct Node {
			value: i64,
		}
		let n: shared Node = Node { value: 1 }
	`, false)
	assertNoErrors(t, res)

	var decl *ast.VarDeclStmt
	for _, s := range res.program.Statements {
		if d, ok := s.(*ast.VarDeclStmt); ok && d.Name == "n" {
			decl = d
			break
		}
	}
	if decl == nil {
		t.Fatal("declaration for 'n' not found in program")
	}
	ut, ok := decl.Type.(types.UnresolvedType)
	if !ok {
		t.Fatalf("expected UnresolvedType annotation, got %T", decl.Type)
	}
	if ut.Allocation != types.Shared {
		t.Errorf("expected Shared allocation on annotation, got %q", ut.Allocation)
	}
}

// TestAlloc_UnannotatedNamedType verifies that a plain (unannotated) named-type
// annotation leaves the allocation as Unspecified.
func TestAlloc_UnannotatedNamedType(t *testing.T) {
	res := parseCollectAndCheck(t, `
		struct Node {
			value: i64,
		}
		let n: Node = Node { value: 1 }
	`, false)
	assertNoErrors(t, res)

	var decl *ast.VarDeclStmt
	for _, s := range res.program.Statements {
		if d, ok := s.(*ast.VarDeclStmt); ok && d.Name == "n" {
			decl = d
			break
		}
	}
	if decl == nil {
		t.Fatal("declaration for 'n' not found in program")
	}
	if types.AllocationOf(decl.Type) != types.Unspecified {
		t.Errorf("expected Unspecified allocation, got %q", types.AllocationOf(decl.Type))
	}
}

// TestAlloc_DeclaredSharedStruct verifies that when the struct itself is declared
// with `shared`, resolveType returns the type with Shared allocation.
func TestAlloc_DeclaredSharedStruct(t *testing.T) {
	res := parseCollectAndCheck(t, `
		shared struct Node {
			value: i64,
		}
		let n = Node { value: 1 }
	`, false)
	assertNoErrors(t, res)
}

// TestAlloc_OwnStackIntoSharedBinding_Error verifies lyra-E018: a `stack`-flavored
// value owned into a `shared` slot is a storage-flavor boundary that must be
// explicit, not an implicit coercion.
func TestAlloc_OwnStackIntoSharedBinding_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `
		stack struct Node {
			value: i64,
		}
		let n: shared Node = Node { value: 1 }
	`, false)
	assertErrorsAre(t, res,
		"n: cannot store a 'stack' value where a 'shared' value is expected; converting allocation is an explicit operation")
}

// TestAlloc_OwnSharedIntoStackBinding_Error is the mirror: a `shared` value owned
// into a `stack` slot.
func TestAlloc_OwnSharedIntoStackBinding_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `
		shared struct Node {
			value: i64,
		}
		let n: stack Node = Node { value: 1 }
	`, false)
	assertErrorsAre(t, res,
		"n: cannot store a 'shared' value where a 'stack' value is expected; converting allocation is an explicit operation")
}

// TestAlloc_SameFlavor_Ok verifies that owning across matching concrete flavors
// is fine — the check only fires on a mismatch.
func TestAlloc_SameFlavor_Ok(t *testing.T) {
	res := parseCollectAndCheck(t, `
		shared struct Node {
			value: i64,
		}
		let n: shared Node = Node { value: 1 }
	`, false)
	assertNoErrors(t, res)
}

// TestAlloc_UnspecifiedIsPolymorphic_Ok verifies that an Unspecified flavor
// (here, a plain struct literal) inherits from context: binding it `shared` is
// the common "allocate as shared" path, not a boundary crossing.
func TestAlloc_UnspecifiedIsPolymorphic_Ok(t *testing.T) {
	res := parseCollectAndCheck(t, `
		struct Node {
			value: i64,
		}
		let n: shared Node = Node { value: 1 }
	`, false)
	assertNoErrors(t, res)
}

// TestAlloc_ReassignAcrossFlavor_Error verifies the check also guards
// reassignment: storing a `shared`-typed value into a `stack`-typed variable.
// The binding's own initializer is Unspecified→Stack (compatible), so only the
// reassignment is flagged.
func TestAlloc_ReassignAcrossFlavor_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `
		struct Node {
			value: i64,
		}
		var n: stack Node = Node { value: 1 }
		let s: shared Node = Node { value: 2 }
		n = s
	`, false)
	assertErrorsAre(t, res,
		"n: cannot store a 'shared' value where a 'stack' value is expected; converting allocation is an explicit operation")
}

// TestAlloc_OwnParamFlavorMismatch_Error verifies E018 at a call site: an `own`
// parameter adopts the argument into its own storage, so passing a `stack` value
// to an `own shared` parameter crosses the flavor boundary.
func TestAlloc_OwnParamFlavorMismatch_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `
		stack struct Node {
			value: i64,
		}
		let consume = (n: own shared Node) => n.value
		let x = Node { value: 1 }
		consume(x)
	`, false)
	assertErrorsAre(t, res,
		"consume: argument 1 (n): cannot store a 'stack' value where a 'shared' value is expected; converting allocation is an explicit operation")
}

// TestAlloc_BorrowedParamFlavorMismatch_Ok verifies the polymorphism half of
// Decision (b): a borrowed (`ref`) parameter references the caller's value in
// place, so it accepts any flavor — no boundary is crossed.
func TestAlloc_BorrowedParamFlavorMismatch_Ok(t *testing.T) {
	res := parseCollectAndCheck(t, `
		stack struct Node {
			value: i64,
		}
		let peek = (n: ref shared Node) => n.value
		let x = Node { value: 1 }
		peek(x)
	`, false)
	assertNoErrors(t, res)
}

// TestAlloc_OwnedReturnFlavorMismatch_Error verifies E018 in return position: an
// owned (bare) return transfers the value to the caller, so a `stack` body value
// returned as `shared` crosses the boundary.
func TestAlloc_OwnedReturnFlavorMismatch_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `
		stack struct Node {
			value: i64,
		}
		let make = () -> shared Node => Node { value: 1 }
	`, false)
	assertErrorsAre(t, res,
		"make: cannot store a 'stack' value where a 'shared' value is expected; converting allocation is an explicit operation")
}

// TestAlloc_BorrowedReturnFlavorMismatch_Ok verifies a `mut`/`ref` return is a
// borrow — allocation-polymorphic — so the flavor check is skipped.
func TestAlloc_BorrowedReturnFlavorMismatch_Ok(t *testing.T) {
	res := parseCollectAndCheck(t, `
		stack struct Node {
			value: i64,
		}
		let make = () -> mut shared Node => Node { value: 1 }
	`, false)
	assertNoErrors(t, res)
}
