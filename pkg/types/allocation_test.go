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
		{"shared parameterized", types.ParameterizedType{Name: "Box", Allocation: types.Shared}, types.Shared},
		{"unset parameterized", types.ParameterizedType{Name: "Maybe"}, types.Unspecified},
		{"shared unresolved", types.UnresolvedType{Name: "Node", Allocation: types.Shared}, types.Shared},
		{"unset unresolved", types.UnresolvedType{Name: "Node"}, types.Unspecified},
		// A tuple carries a use-site flavor like the other nominal types.
		{"shared tuple", types.TupleType{Name: "Foo", Allocation: types.Shared}, types.Shared},
		{"unset tuple", types.TupleType{Name: "Foo"}, types.Unspecified},
		// Types that cannot carry a flavor read as Unspecified.
		{"primitive", types.PrimitiveType{Name: types.Int64}, types.Unspecified},
	}
	for _, c := range cases {
		if got := types.AllocationOf(c.typ); got != c.want {
			t.Errorf("%s: AllocationOf = %q, want %q", c.name, got, c.want)
		}
	}
}

// WithAllocation returns a copy of the type with the modifier overridden, or
// the original when mod is Unspecified (no override).
func TestWithAllocation(t *testing.T) {
	cases := []struct {
		name string
		base types.Type
		mod  types.AllocationModifier
		want types.AllocationModifier
	}{
		// Overrides on types that carry allocation.
		{"struct unspecified→shared", types.NamedStructType{Name: "N"}, types.Shared, types.Shared},
		{"struct shared→stack", types.NamedStructType{Name: "N", Allocation: types.Shared}, types.Stack, types.Stack},
		{"data unspecified→shared", types.DataType{Name: "T"}, types.Shared, types.Shared},
		{"static array→stack", types.StaticArrayType{Size: 4}, types.Stack, types.Stack},
		{"dynamic array→shared", types.DynamicArrayType{}, types.Shared, types.Shared},
		{"parameterized→shared", types.ParameterizedType{Name: "Box"}, types.Shared, types.Shared},
		{"unresolved→shared", types.UnresolvedType{Name: "N"}, types.Shared, types.Shared},
		{"tuple→shared", types.TupleType{Name: "Foo"}, types.Shared, types.Shared},
		// Unspecified mod is a no-op — returns the original allocation.
		{"struct no override", types.NamedStructType{Name: "N", Allocation: types.Stack}, types.Unspecified, types.Stack},
		{"data no override", types.DataType{Name: "T", Allocation: types.Shared}, types.Unspecified, types.Shared},
		{"tuple no override", types.TupleType{Name: "Foo", Allocation: types.Stack}, types.Unspecified, types.Stack},
		// Types that can't carry a flavor are returned unchanged.
		{"primitive unchanged", types.PrimitiveType{Name: types.Int64}, types.Shared, types.Unspecified},
	}
	for _, c := range cases {
		got := types.AllocationOf(types.WithAllocation(c.base, c.mod))
		if got != c.want {
			t.Errorf("%s: AllocationOf(WithAllocation(%T, %q)) = %q, want %q",
				c.name, c.base, c.mod, got, c.want)
		}
	}
}
