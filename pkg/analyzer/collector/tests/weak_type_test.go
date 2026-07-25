package collector_test

import (
	"testing"

	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/types"
)

// `weak T` collects to a types.WeakType wrapping the inner type. Previously the
// collector's parseType had no `weak_type` case and hard-errored
// ("unknown type node kind: weak_type"), making `weak` unusable in any program.

func structFieldType(t *testing.T, src, field string) types.Type {
	t.Helper()
	program, _, _, errs := parseAndCollect(t, src)
	for _, e := range errs {
		t.Fatalf("unexpected collector error: %v", e)
	}
	decl := program.Statements[0].(*ast.TypeDeclStmt)
	st, ok := decl.Type.(types.NamedStructType)
	if !ok {
		t.Fatalf("expected a NamedStructType, got %T", decl.Type)
	}
	for _, f := range st.Fields {
		if f.Name == field {
			return f.Type
		}
	}
	t.Fatalf("field %q not found", field)
	return nil
}

func TestCollect_WeakField_NamedInner(t *testing.T) {
	ft := structFieldType(t, `struct Node { value: i64, parent: weak Node }`, "parent")
	weak, ok := ft.(types.WeakType)
	if !ok {
		t.Fatalf("expected field type WeakType, got %T", ft)
	}
	inner, ok := weak.Inner.(types.UnresolvedType)
	if !ok {
		t.Fatalf("expected weak inner UnresolvedType, got %T", weak.Inner)
	}
	if inner.Name != "Node" {
		t.Errorf("weak inner name = %q, want %q", inner.Name, "Node")
	}
	if got := ft.String(); got != "weak Node" {
		t.Errorf("weak type String() = %q, want %q", got, "weak Node")
	}
}

func TestCollect_WeakField_ParameterizedInner(t *testing.T) {
	ft := structFieldType(t, `struct Cell { prev: weak Maybe<Node> }`, "prev")
	weak, ok := ft.(types.WeakType)
	if !ok {
		t.Fatalf("expected field type WeakType, got %T", ft)
	}
	if _, ok := weak.Inner.(types.ParameterizedType); !ok {
		t.Errorf("expected weak inner ParameterizedType, got %T", weak.Inner)
	}
}
