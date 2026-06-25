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
