package typechecker_test

import (
	"testing"

	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/types"
)

// ── helpers ──────────────────────────────────────────────────────────────────

// arrayLiteralType parses source, type-checks it, and returns the TypeTable
// entry for the initializer of the first VarDeclStmt. Fails the test if the
// entry is absent or the statement isn't a var declaration.
func arrayLiteralType(t *testing.T, source string) types.Type {
	t.Helper()
	res := parseCollectAndCheck(t, source, false)
	if len(res.program.Statements) == 0 {
		t.Fatal("expected at least one statement")
	}
	decl, ok := res.program.Statements[0].(*ast.VarDeclStmt)
	if !ok {
		t.Fatalf("expected VarDeclStmt, got %T", res.program.Statements[0])
	}
	typ, ok := res.typeTable.Get(decl.Value)
	if !ok {
		t.Fatal("no TypeTable entry for the array literal initializer")
	}
	return typ
}

// ── unannotated literals: inferred as StaticArrayType ────────────────────────

func TestArrayLiteral_NoAnnotation_InferredAsStaticArray(t *testing.T) {
	res := parseCollectAndCheck(t, `let xs = [1, 2, 3]`, false)
	assertNoErrors(t, res)
	typ := arrayLiteralType(t, `let xs = [1, 2, 3]`)
	sa, ok := typ.(types.StaticArrayType)
	if !ok {
		t.Fatalf("expected StaticArrayType, got %T (%s)", typ, typ)
	}
	if sa.Size != 3 {
		t.Errorf("expected size 3, got %d", sa.Size)
	}
	p, ok := sa.ElementType.(types.PrimitiveType)
	if !ok || p.Name != types.Int {
		t.Errorf("expected element type int, got %s", sa.ElementType)
	}
}

func TestArrayLiteral_NoAnnotation_StringElements(t *testing.T) {
	res := parseCollectAndCheck(t, `let xs = ["a", "b"]`, false)
	assertNoErrors(t, res)
	typ := arrayLiteralType(t, `let xs = ["a", "b"]`)
	sa, ok := typ.(types.StaticArrayType)
	if !ok {
		t.Fatalf("expected StaticArrayType, got %T", typ)
	}
	if sa.Size != 2 {
		t.Errorf("expected size 2, got %d", sa.Size)
	}
	p, ok := sa.ElementType.(types.PrimitiveType)
	if !ok || p.Name != types.String {
		t.Errorf("expected element type string, got %s", sa.ElementType)
	}
}

func TestArrayLiteral_NoAnnotation_BoolElements(t *testing.T) {
	res := parseCollectAndCheck(t, `let xs = [true, false, true]`, false)
	assertNoErrors(t, res)
	typ := arrayLiteralType(t, `let xs = [true, false, true]`)
	sa, ok := typ.(types.StaticArrayType)
	if !ok {
		t.Fatalf("expected StaticArrayType, got %T", typ)
	}
	if sa.Size != 3 {
		t.Errorf("expected size 3, got %d", sa.Size)
	}
	p, ok := sa.ElementType.(types.PrimitiveType)
	if !ok || p.Name != types.Boolean {
		t.Errorf("expected element type boolean, got %s", sa.ElementType)
	}
}

func TestArrayLiteral_NoAnnotation_SingleElement(t *testing.T) {
	res := parseCollectAndCheck(t, `let xs = [42]`, false)
	assertNoErrors(t, res)
	typ := arrayLiteralType(t, `let xs = [42]`)
	sa, ok := typ.(types.StaticArrayType)
	if !ok {
		t.Fatalf("expected StaticArrayType, got %T", typ)
	}
	if sa.Size != 1 {
		t.Errorf("expected size 1, got %d", sa.Size)
	}
}

// ── static array annotation: exact-match and size errors ─────────────────────

func TestArrayLiteral_StaticAnnotation_MatchingSize_Ok(t *testing.T) {
	res := parseCollectAndCheck(t, `let xs: [3]int = [1, 2, 3]`, false)
	assertNoErrors(t, res)
}

func TestArrayLiteral_StaticAnnotation_FewerElements_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `let xs: [3]int = [1, 2]`, false)
	assertErrorsAre(t, res, "xs: cannot assign StaticArray<integer literal, 2> to StaticArray<int, 3>")
}

func TestArrayLiteral_StaticAnnotation_MoreElements_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `let xs: [3]int = [1, 2, 3, 4]`, false)
	assertErrorsAre(t, res, "xs: cannot assign StaticArray<integer literal, 4> to StaticArray<int, 3>")
}

func TestArrayLiteral_StaticAnnotation_OneElement_Ok(t *testing.T) {
	res := parseCollectAndCheck(t, `let xs: [1]int = [99]`, false)
	assertNoErrors(t, res)
}

// ── dynamic array annotation: literal widens to DynamicArrayType ─────────────

func TestArrayLiteral_DynamicAnnotation_Ok(t *testing.T) {
	res := parseCollectAndCheck(t, `let xs: []int = [1, 2, 3]`, false)
	assertNoErrors(t, res)
}

func TestArrayLiteral_DynamicAnnotation_SingleElement_Ok(t *testing.T) {
	res := parseCollectAndCheck(t, `let xs: []string = ["hello"]`, false)
	assertNoErrors(t, res)
}

func TestArrayLiteral_DynamicAnnotation_Empty_Ok(t *testing.T) {
	// An empty literal [] has no element type; it is vacuously assignable to any
	// dynamic array type.
	res := parseCollectAndCheck(t, `let xs: []int = []`, false)
	assertNoErrors(t, res)
}

func TestArrayLiteral_DynamicAnnotation_UntypedIntWidensToAnnotatedElemType_Ok(t *testing.T) {
	// Untyped integer literals widen to the annotated element type (i32).
	res := parseCollectAndCheck(t, `let xs: []i32 = [1, 2, 3]`, false)
	assertNoErrors(t, res)
}

// ── you cannot assign a dynamic-annotated variable to a static one ───────────

func TestArrayLiteral_CannotAssignDynamicVarToStaticAnnotation_Error(t *testing.T) {
	// The variable `dyn` has type DynamicArrayType{int}; `stat` expects
	// StaticArrayType{int, 3}.  isAssignable only widens Static→Dynamic, never
	// Dynamic→Static, so this must be an error.
	res := parseCollectAndCheck(t, `
		let dyn: []int = [1, 2, 3]
		let stat: [3]int = dyn
	`, false)
	assertErrorsAre(t, res, "stat: cannot assign DynamicArray<int> to StaticArray<int, 3>")
}

func TestArrayLiteral_CannotAssignDynamicVarToDifferentSizeStaticAnnotation_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let dyn: []int = [1, 2, 3]
		let stat: [5]int = dyn
	`, false)
	assertErrorsAre(t, res, "stat: cannot assign DynamicArray<int> to StaticArray<int, 5>")
}

// ── element type mismatches ───────────────────────────────────────────────────

func TestArrayLiteral_MixedIntAndString_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `let xs = [1, "two", 3]`, false)
	// "integer literal" is the display name for untyped integer literals, matching
	// the convention used in all other type-mismatch errors (e.g. "cannot assign
	// integer literal to string").
	assertErrorsAre(t, res,
		"array literal: element type string is not compatible with preceding element type integer literal")
}

func TestArrayLiteral_MixedIntAndBool_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `let xs = [1, true]`, false)
	assertErrorsAre(t, res,
		"array literal: element type boolean is not compatible with preceding element type integer literal")
}

func TestArrayLiteral_StaticAnnotation_WrongElementType_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `let xs: [3]int = [1, "two", 3]`, false)
	// The mixed-type error fires first; no separate annotation error.
	assertErrorsAre(t, res,
		"array literal: element type string is not compatible with preceding element type integer literal")
}

func TestArrayLiteral_DynamicAnnotation_WrongElementType_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `let xs: []int = ["a", "b"]`, false)
	assertErrorsAre(t, res, "xs: cannot assign StaticArray<string, 2> to DynamicArray<int>")
}

// ── static ↔ static size mismatch via variable ───────────────────────────────

func TestArrayLiteral_StaticToStaticSameSizeVar_Ok(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let a: [3]int = [1, 2, 3]
		let b: [3]int = a
	`, false)
	assertNoErrors(t, res)
}

func TestArrayLiteral_StaticToStaticDifferentSizeVar_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let a: [3]int = [1, 2, 3]
		let b: [4]int = a
	`, false)
	assertErrorsAre(t, res, "b: cannot assign StaticArray<int, 3> to StaticArray<int, 4>")
}

// ── unannotated: untyped integer elements promote to int ─────────────────────

func TestArrayLiteral_NoAnnotation_ElementsPromoteToDefaultInt(t *testing.T) {
	// Without an annotation the literal's element type goes through
	// promoteToDefault: UntypedInt → int.
	typ := arrayLiteralType(t, `let xs = [10, 20, 30]`)
	sa, ok := typ.(types.StaticArrayType)
	if !ok {
		t.Fatalf("expected StaticArrayType, got %T", typ)
	}
	p, ok := sa.ElementType.(types.PrimitiveType)
	if !ok || p.Name != types.Int {
		t.Errorf("expected promoted element type int, got %v", sa.ElementType)
	}
}

func TestArrayLiteral_NoAnnotation_FloatElementsPromoteToF64(t *testing.T) {
	typ := arrayLiteralType(t, `let xs = [1.0, 2.5, 3.14]`)
	sa, ok := typ.(types.StaticArrayType)
	if !ok {
		t.Fatalf("expected StaticArrayType, got %T", typ)
	}
	p, ok := sa.ElementType.(types.PrimitiveType)
	if !ok || p.Name != types.Float64 {
		t.Errorf("expected promoted element type f64, got %v", sa.ElementType)
	}
}
