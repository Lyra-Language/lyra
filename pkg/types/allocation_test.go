package types_test

import (
	"testing"

	"github.com/Lyra-Language/lyra/pkg/types"
)

// AllocationOf reads the flavor off the types that carry one, and reports
// Unspecified (the zero value) for an unset field or a type that can't carry
// an allocation modifier.
func TestAllocationOf(t *testing.T) {
	cases := []struct {
		name string
		typ  types.Type
		want types.AllocationModifier
	}{
		{"shared struct", types.NamedStructType{Name: "Node", Allocation: types.Shared}, types.Shared},
		{"stack struct", types.NamedStructType{Name: "Pos", Allocation: types.Stack}, types.Stack},
		{"unset struct", types.NamedStructType{Name: "Bare"}, types.Unspecified},
		{"shared data", types.DataType{Name: "Tree", Allocation: types.Shared}, types.Shared},
		{"shared static array", types.StaticArrayType{Size: 4, Allocation: types.Shared}, types.Shared},
		{"unset dynamic array", types.DynamicArrayType{}, types.Unspecified},
		// Types that cannot carry a flavor read as Unspecified.
		{"primitive", types.PrimitiveType{Name: types.Int64}, types.Unspecified},
		// A named tuple's modifier lives on the declaration, not the TupleType,
		// so AllocationOf can't see it (documented limitation).
		{"tuple", types.TupleType{Name: "Foo"}, types.Unspecified},
	}
	for _, c := range cases {
		if got := types.AllocationOf(c.typ); got != c.want {
			t.Errorf("%s: AllocationOf = %q, want %q", c.name, got, c.want)
		}
	}
}
