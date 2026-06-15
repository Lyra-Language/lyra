package collector_test

import (
	"testing"

	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/types"
)

// `let mut x` records IsMut on the VarDeclStmt: the name is frozen but the
// value's interior may be mutated.
func TestCollect_LetMut_SetsIsMut(t *testing.T) {
	program, _, _, _ := parseAndCollect(t, `let mut x = 1`)
	decl := program.Statements[0].(*ast.VarDeclStmt)
	if decl.BindingKind != ast.BindingLet {
		t.Fatalf("expected BindingLet, got %v", decl.BindingKind)
	}
	if !decl.IsMut {
		t.Errorf("expected IsMut=true for `let mut`")
	}
	if decl.CanMutateInterior() != true || decl.IsMutable() != false {
		t.Errorf("`let mut`: want CanMutateInterior=true, IsMutable=false; got %v, %v",
			decl.CanMutateInterior(), decl.IsMutable())
	}
}

// A plain `let` is deeply immutable: neither the name nor the interior may change.
func TestCollect_Let_PlainIsImmutable(t *testing.T) {
	program, _, _, _ := parseAndCollect(t, `let x = 1`)
	decl := program.Statements[0].(*ast.VarDeclStmt)
	if decl.IsMut || decl.CanMutateInterior() {
		t.Errorf("plain `let`: want IsMut=false and CanMutateInterior=false; got %v, %v",
			decl.IsMut, decl.CanMutateInterior())
	}
}

// `var mut` is redundant (a `var` is already interior-mutable) and warns.
func TestCollect_VarMut_Redundant_Warns(t *testing.T) {
	errors := parseAndCollectErrors(t, `var mut x = 1`)
	assertCollectorErrorContains(t, errors, "`var mut` is redundant")
}

// A struct field marked `readonly` is frozen; an unmarked field is mutable.
func TestCollect_StructField_ReadonlyMarker(t *testing.T) {
	_, table, _, _ := parseAndCollect(t, `struct Entity { readonly id: u64, pos: i64 }`)
	decl, ok := table.Types["Entity"]
	if !ok {
		t.Fatal("Entity type not collected")
	}
	st, ok := decl.Type.(types.NamedStructType)
	if !ok {
		t.Fatalf("Entity is not a NamedStructType, got %T", decl.Type)
	}
	frozen := map[string]bool{}
	for _, f := range st.Fields {
		frozen[f.Name] = f.Frozen
	}
	if !frozen["id"] {
		t.Errorf("field `readonly id` should be Frozen")
	}
	if frozen["pos"] {
		t.Errorf("unmarked field `pos` should not be Frozen")
	}
}
