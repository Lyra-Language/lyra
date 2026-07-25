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
	if !ok || p.Name != types.Int64 {
		t.Errorf("expected element type i64, got %s", sa.ElementType)
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

// ── element-width narrowing against the annotation (backend needs a concrete width) ──

// An annotated element width is pushed onto the literal's elements, and the
// literal is re-recorded with that concrete element type — so the backend builds
// `[N x u8]`, not `[N x i64]`. Without this an annotated narrow array lowered at
// i64 element width (a residual coerce saved a `let`, but a function return —
// which fixes the type from the signature — miscompiled).
func TestArrayLiteral_AnnotatedElementWidth_Narrows(t *testing.T) {
	typ := arrayLiteralType(t, `let xs: [3]u8 = [10, 20, 30]`)
	sa, ok := typ.(types.StaticArrayType)
	if !ok {
		t.Fatalf("expected StaticArrayType, got %T (%s)", typ, typ)
	}
	if p, ok := sa.ElementType.(types.PrimitiveType); !ok || p.Name != types.UInt8 {
		t.Errorf("expected element type u8, got %s", sa.ElementType)
	}
}

// A dynamic annotation must NOT be re-recorded as static: `dyn` is used as a
// dynamic array, and overwriting it with a static type would mask a later
// dynamic→static assignment error (regression guard for the static-only
// re-record).
func TestArrayLiteral_DynamicAnnotation_StaysDynamic(t *testing.T) {
	typ := arrayLiteralType(t, `let dyn: []i64 = [1, 2, 3]`)
	if _, ok := typ.(types.DynamicArrayType); !ok {
		t.Errorf("expected the value recorded as DynamicArrayType, got %T (%s)", typ, typ)
	}
}

// ── static array annotation: exact-match and size errors ─────────────────────

func TestArrayLiteral_StaticAnnotation_MatchingSize_Ok(t *testing.T) {
	res := parseCollectAndCheck(t, `let xs: [3]i64 = [1, 2, 3]`, false)
	assertNoErrors(t, res)
}

func TestArrayLiteral_StaticAnnotation_FewerElements_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `let xs: [3]i64 = [1, 2]`, false)
	assertErrorsAre(t, res, "xs: cannot assign StaticArray<integer literal, 2> to StaticArray<i64, 3>")
}

func TestArrayLiteral_StaticAnnotation_MoreElements_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `let xs: [3]i64 = [1, 2, 3, 4]`, false)
	assertErrorsAre(t, res, "xs: cannot assign StaticArray<integer literal, 4> to StaticArray<i64, 3>")
}

func TestArrayLiteral_StaticAnnotation_OneElement_Ok(t *testing.T) {
	res := parseCollectAndCheck(t, `let xs: [1]i64 = [99]`, false)
	assertNoErrors(t, res)
}

// ── dynamic array annotation: literal widens to DynamicArrayType ─────────────

func TestArrayLiteral_DynamicAnnotation_Ok(t *testing.T) {
	res := parseCollectAndCheck(t, `let xs: []i64 = [1, 2, 3]`, false)
	assertNoErrors(t, res)
}

func TestArrayLiteral_DynamicAnnotation_SingleElement_Ok(t *testing.T) {
	res := parseCollectAndCheck(t, `let xs: []string = ["hello"]`, false)
	assertNoErrors(t, res)
}

func TestArrayLiteral_DynamicAnnotation_Empty_Ok(t *testing.T) {
	// An empty literal [] has no element type; it is vacuously assignable to any
	// dynamic array type.
	res := parseCollectAndCheck(t, `let xs: []i64 = []`, false)
	assertNoErrors(t, res)
}

func TestArrayLiteral_DynamicAnnotation_UntypedIntWidensToAnnotatedElemType_Ok(t *testing.T) {
	// Untyped integer literals widen to the annotated element type (i32).
	res := parseCollectAndCheck(t, `let xs: []i32 = [1, 2, 3]`, false)
	assertNoErrors(t, res)
}

// ── you cannot assign a dynamic-annotated variable to a static one ───────────

func TestArrayLiteral_CannotAssignDynamicVarToStaticAnnotation_Error(t *testing.T) {
	// The variable `dyn` has type DynamicArrayType{i64}; `stat` expects
	// StaticArrayType{i64, 3}.  isAssignable only widens Static→Dynamic, never
	// Dynamic→Static, so this must be an error.
	res := parseCollectAndCheck(t, `
		let dyn: []i64 = [1, 2, 3]
		let stat: [3]i64 = dyn
	`, false)
	assertErrorsAre(t, res, "stat: cannot assign DynamicArray<i64> to StaticArray<i64, 3>")
}

func TestArrayLiteral_CannotAssignDynamicVarToDifferentSizeStaticAnnotation_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let dyn: []i64 = [1, 2, 3]
		let stat: [5]i64 = dyn
	`, false)
	assertErrorsAre(t, res, "stat: cannot assign DynamicArray<i64> to StaticArray<i64, 5>")
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
	res := parseCollectAndCheck(t, `let xs: [3]i64 = [1, "two", 3]`, false)
	// The mixed-type error fires first; no separate annotation error.
	assertErrorsAre(t, res,
		"array literal: element type string is not compatible with preceding element type integer literal")
}

func TestArrayLiteral_DynamicAnnotation_WrongElementType_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `let xs: []i64 = ["a", "b"]`, false)
	assertErrorsAre(t, res, "xs: cannot assign StaticArray<string, 2> to DynamicArray<i64>")
}

// ── static ↔ static size mismatch via variable ───────────────────────────────

func TestArrayLiteral_StaticToStaticSameSizeVar_Ok(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let a: [3]i64 = [1, 2, 3]
		let b: [3]i64 = a
	`, false)
	assertNoErrors(t, res)
}

func TestArrayLiteral_StaticToStaticDifferentSizeVar_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let a: [3]i64 = [1, 2, 3]
		let b: [4]i64 = a
	`, false)
	assertErrorsAre(t, res, "b: cannot assign StaticArray<i64, 3> to StaticArray<i64, 4>")
}

// ── unannotated: untyped integer elements promote to i64 ─────────────────────

func TestArrayLiteral_NoAnnotation_ElementsPromoteToDefaultInt(t *testing.T) {
	// Without an annotation the literal's element type goes through
	// promoteToDefault: UntypedInt → i64.
	typ := arrayLiteralType(t, `let xs = [10, 20, 30]`)
	sa, ok := typ.(types.StaticArrayType)
	if !ok {
		t.Fatalf("expected StaticArrayType, got %T", typ)
	}
	p, ok := sa.ElementType.(types.PrimitiveType)
	if !ok || p.Name != types.Int64 {
		t.Errorf("expected promoted element type i64, got %v", sa.ElementType)
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
