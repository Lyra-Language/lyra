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

// TestAlloc_OwnStackIntoSharedBinding_Error verifies lyra-E018: a `stack`-flavored
// value owned into a `shared` slot is a storage-flavor boundary that must be
// explicit, not an implicit coercion. The value's concrete `stack` flavor comes
// from an annotated binding (there is no declaration-level flavor): a bare
// construction is Unspecified/polymorphic, so it must be pinned to `stack` first.
func TestAlloc_OwnStackIntoSharedBinding_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `
		struct Node {
			value: i64,
		}
		let s: stack Node = Node { value: 1 }
		let n: shared Node = s
	`, false)
	assertErrorsAre(t, res,
		"n: cannot store a 'stack' value where a 'shared' value is expected; converting allocation is an explicit operation")
}

// TestAlloc_OwnSharedIntoStackBinding_Error is the mirror: a `shared` value owned
// into a `stack` slot.
func TestAlloc_OwnSharedIntoStackBinding_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `
		struct Node {
			value: i64,
		}
		let s: shared Node = Node { value: 1 }
		let n: stack Node = s
	`, false)
	assertErrorsAre(t, res,
		"n: cannot store a 'shared' value where a 'stack' value is expected; converting allocation is an explicit operation")
}

// TestAlloc_SameFlavor_Ok verifies that owning across matching concrete flavors
// is fine — the check only fires on a mismatch.
func TestAlloc_SameFlavor_Ok(t *testing.T) {
	res := parseCollectAndCheck(t, `
		struct Node {
			value: i64,
		}
		let s: shared Node = Node { value: 1 }
		let n: shared Node = s
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
		struct Node {
			value: i64,
		}
		let consume = (n: own shared Node) => n.value
		let x: stack Node = Node { value: 1 }
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
		struct Node {
			value: i64,
		}
		let peek = (n: ref shared Node) => n.value
		let x: stack Node = Node { value: 1 }
		peek(x)
	`, false)
	assertNoErrors(t, res)
}

// TestAlloc_OwnedReturnFlavorMismatch_Error verifies E018 in return position: an
// owned (bare) return transfers the value to the caller, so a `stack` body value
// returned as `shared` crosses the boundary.
func TestAlloc_OwnedReturnFlavorMismatch_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `
		struct Node {
			value: i64,
		}
		let make = (n: stack Node) -> shared Node => n
	`, false)
	assertErrorsAre(t, res,
		"make: cannot store a 'stack' value where a 'shared' value is expected; converting allocation is an explicit operation")
}

// TestAlloc_BorrowedReturnFlavorMismatch_Ok verifies a `mut`/`ref` return is a
// borrow — allocation-polymorphic — so the flavor check is skipped.
func TestAlloc_BorrowedReturnFlavorMismatch_Ok(t *testing.T) {
	res := parseCollectAndCheck(t, `
		struct Node {
			value: i64,
		}
		let make = (n: stack Node) -> mut shared Node => n
	`, false)
	assertNoErrors(t, res)
}

// TestResolve_NamedTupleElementType_Ok is a regression test for the resolveType
// gap this slice fixed: a named struct/data used as a tuple element type in an
// annotation was never resolved, so even an allocation-free `(Node, Node)`
// annotation failed assignability with a confusing "cannot assign ?(Node, Node)
// to ?(Node, Node)". Element types now resolve, so it type-checks.
func TestResolve_NamedTupleElementType_Ok(t *testing.T) {
	res := parseCollectAndCheck(t, `
		struct Node {
			value: i64,
		}
		let p: (Node, Node) = (Node { value: 1 }, Node { value: 2 })
	`, false)
	assertNoErrors(t, res)
}

// TestResolve_NamedArrayElementType_Ok is the array counterpart of the same
// resolveType fix.
func TestResolve_NamedArrayElementType_Ok(t *testing.T) {
	res := parseCollectAndCheck(t, `
		struct Node {
			value: i64,
		}
		let xs: [2]Node = [Node { value: 1 }, Node { value: 2 }]
	`, false)
	assertNoErrors(t, res)
}

// TestAlloc_TupleElementFlavorMismatch_Error verifies element-level flavor
// checking: the tuple's own flavor matches (both unspecified), but its first
// element is owned across a boundary (a `stack` value into a `shared` element
// slot). The recursion drills to the element and names the element flavors.
func TestAlloc_TupleElementFlavorMismatch_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `
		struct Node {
			value: i64,
		}
		let src: (stack Node, Node) = (Node { value: 1 }, Node { value: 2 })
		let p: (shared Node, Node) = src
	`, false)
	assertErrorsAre(t, res,
		"p: cannot store a 'stack' value where a 'shared' value is expected; converting allocation is an explicit operation")
}

// TestAlloc_ArrayElementFlavorMismatch_Error is the array counterpart of the tuple
// case above, and until 08/03 it could not be written at all: `array_type`'s element
// was `_non_allocated_type`, so `[2]shared Node` was a parse error. The *checking*
// was there the whole time — `firstAllocationMismatch` recurses into array elements
// and its comment cites "a `stack` element assigned into a `[N]shared` slot" as the
// case it exists for — so this closes the gap between a rule and the syntax needed
// to reach it.
func TestAlloc_ArrayElementFlavorMismatch_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `
		struct Node {
			value: i64,
		}
		let src: [2]stack Node = [Node { value: 1 }, Node { value: 2 }]
		let xs: [2]shared Node = src
	`, false)
	assertErrorsAre(t, res,
		"xs: cannot store a 'stack' value where a 'shared' value is expected; converting allocation is an explicit operation")
}

// The dynamic-array counterpart: the element flavor is checked through `[]T` too,
// not only the fixed-size form.
func TestAlloc_DynamicArrayElementFlavorMismatch_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `
		struct Node {
			value: i64,
		}
		let src: []stack Node = [Node { value: 1 }]
		let xs: []shared Node = src
	`, false)
	assertErrorsAre(t, res,
		"xs: cannot store a 'stack' value where a 'shared' value is expected; converting allocation is an explicit operation")
}

// An array element carrying a modifier resolves and checks like any other type: a
// matching flavor is accepted, and `weak` is a legal element type. `[]shared Node`
// is the shape the change was made for — a tree's children — and `[]weak T` the
// observer-list shape that goes with it.
func TestAlloc_ArrayElementModifier_Ok(t *testing.T) {
	for _, source := range []string{
		// Matching flavors, fixed size.
		`struct Node { value: i64 }
let a: shared Node = Node { value: 1 }
let xs: [1]shared Node = [a]`,
		// Dynamic array of shared elements — the motivating shape.
		`struct Node { value: i64, kids: []shared Node }
let leaf: shared Node = Node { value: 1, kids: [] }
let root: shared Node = Node { value: 2, kids: [leaf] }`,
		// An array of weak references.
		`struct Node { value: i64 }
let a: shared Node = Node { value: 1 }
let xs: [1]weak Node = [a.weak()]`,
		// Nesting composes in both directions: a modifier inside an array of arrays,
		// and an array-of-weak inside an outer allocation modifier.
		`struct Node { value: i64 }
let xs: [][]shared Node = [[]]
let a: shared Node = Node { value: 1 }
let ys: stack [1]weak Node = [a.weak()]`,
	} {
		res := parseCollectAndCheck(t, source, false)
		assertNoErrors(t, res)
	}
}
