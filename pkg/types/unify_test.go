package types_test

import (
	"testing"

	"github.com/Lyra-Language/lyra/pkg/types"
)

// FunctionType is stored as *types.FunctionType everywhere in the codebase
// (ast.FunctionDefStmt.Signature, ast.TraitMethod.Signature, the collector's
// parseFunctionType). TypesEqual must therefore compare pointer-typed
// FunctionTypes correctly rather than silently returning false because the
// switch only matched the value form.
func TestTypesEqualFunctionTypePointerConvention(t *testing.T) {
	intT := types.PrimitiveType{Name: types.Int64}
	floatT := types.PrimitiveType{Name: types.Float64}

	makeSig := func(ret types.ReturnType, params ...types.Type) *types.LambdaType {
		ps := make([]types.ParameterType, 0, len(params))
		for _, p := range params {
			ps = append(ps, types.ParameterType{Type: p})
		}
		return &types.LambdaType{Parameters: ps, ReturnType: ret}
	}

	t.Run("identical signatures are equal", func(t *testing.T) {
		a := makeSig(types.ReturnType{Type: intT}, intT, intT)
		b := makeSig(types.ReturnType{Type: intT}, intT, intT)
		if !types.TypesEqual(a, b) {
			t.Fatalf("expected identical *FunctionType values to be equal: %s vs %s", a, b)
		}
	})

	t.Run("different return types are unequal", func(t *testing.T) {
		a := makeSig(types.ReturnType{Type: intT}, intT)
		b := makeSig(types.ReturnType{Type: floatT}, intT)
		if types.TypesEqual(a, b) {
			t.Fatalf("expected different return types to be unequal")
		}
	})

	t.Run("different parameter arity is unequal", func(t *testing.T) {
		a := makeSig(types.ReturnType{Type: intT}, intT)
		b := makeSig(types.ReturnType{Type: intT}, intT, intT)
		if types.TypesEqual(a, b) {
			t.Fatalf("expected different parameter arities to be unequal")
		}
	})

	t.Run("different parameter types are unequal", func(t *testing.T) {
		a := makeSig(types.ReturnType{Type: intT}, intT, intT)
		b := makeSig(types.ReturnType{Type: intT}, intT, floatT)
		if types.TypesEqual(a, b) {
			t.Fatalf("expected differing parameter types to be unequal")
		}
	})

	t.Run("nil pointers are equal to themselves but not to populated signatures", func(t *testing.T) {
		var a *types.LambdaType
		var b *types.LambdaType
		if !types.TypesEqual(a, b) {
			t.Fatalf("expected two nil *LambdaType to compare equal")
		}
		if types.TypesEqual(a, makeSig(types.ReturnType{Type: intT})) {
			t.Fatalf("expected nil *LambdaType and a real signature to be unequal")
		}
	})
}

// ConstrainedType is constructed exclusively as *types.ConstrainedType
// (Collector.parseConstrainedType, typedecls.collectConstrainedTypeDeclaration).
// Before the fix, TypesEqual had no case for it at all and every comparison
// silently returned false.
func TestTypesEqualConstrainedTypePointerConvention(t *testing.T) {
	intT := types.PrimitiveType{Name: types.Int64}

	t.Run("same declared name is equal (nominal equality)", func(t *testing.T) {
		a := &types.ConstrainedType{Name: "Angle", Type: intT}
		b := &types.ConstrainedType{Name: "Angle", Type: intT}
		if !types.TypesEqual(a, b) {
			t.Fatalf("expected two *ConstrainedType with the same name to be equal")
		}
	})

	t.Run("different declared names are unequal", func(t *testing.T) {
		a := &types.ConstrainedType{Name: "Angle", Type: intT}
		b := &types.ConstrainedType{Name: "Radian", Type: intT}
		if types.TypesEqual(a, b) {
			t.Fatalf("expected different constrained type names to be unequal")
		}
	})

	t.Run("nil pointer handling", func(t *testing.T) {
		var a *types.ConstrainedType
		var b *types.ConstrainedType
		if !types.TypesEqual(a, b) {
			t.Fatalf("expected two nil *ConstrainedType to compare equal")
		}
		if types.TypesEqual(a, &types.ConstrainedType{Name: "Angle"}) {
			t.Fatalf("expected nil *ConstrainedType and a real one to be unequal")
		}
	})
}

// Cross-type comparisons should never report equality, even when the stored
// concrete types live on different sides of the value/pointer convention.
func TestTypesEqualMixedConventions(t *testing.T) {
	intT := types.PrimitiveType{Name: types.Int64}
	fn := &types.LambdaType{ReturnType: types.ReturnType{Type: intT}}
	ct := &types.ConstrainedType{Name: "Angle", Type: intT}

	cases := []struct {
		name string
		a, b types.Type
	}{
		{"*LambdaType vs PrimitiveType", fn, intT},
		{"PrimitiveType vs *LambdaType", intT, fn},
		{"*ConstrainedType vs PrimitiveType", ct, intT},
		{"*LambdaType vs *ConstrainedType", fn, ct},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if types.TypesEqual(tc.a, tc.b) {
				t.Fatalf("expected %s and %s to be unequal", tc.a, tc.b)
			}
		})
	}
}

func TestTypesEqualNamedStruct(t *testing.T) {
	a := types.NamedStructType{Name: "Point"}
	b := types.NamedStructType{Name: "Point"}
	if !types.TypesEqual(a, b) {
		t.Fatalf("expected %s and %s to be equal", a, b)
	}
}

func TestTypesNotEqualNamedStruct(t *testing.T) {
	a := types.NamedStructType{Name: "Point"}
	b := types.NamedStructType{Name: "Line"}
	if types.TypesEqual(a, b) {
		t.Fatalf("expected %s and %s to be unequal", a, b)
	}
}

func TestTypesEqualAnonymousStruct(t *testing.T) {
	a := types.AnonymousStructType{
		Fields: []types.StructField{
			{Name: "x", Type: types.PrimitiveType{Name: types.Int64}},
			{Name: "y", Type: types.PrimitiveType{Name: types.Int64}},
		},
	}
	b := types.AnonymousStructType{
		Fields: []types.StructField{
			{Name: "x", Type: types.PrimitiveType{Name: types.Int64}},
			{Name: "y", Type: types.PrimitiveType{Name: types.Int64}},
		},
	}
	if !types.TypesEqual(a, b) {
		t.Fatalf("expected %s and %s to be equal", a, b)
	}
}

func TestTypesNotEqualAnonymousStruct(t *testing.T) {
	a := types.AnonymousStructType{
		Fields: []types.StructField{
			{Name: "x", Type: types.PrimitiveType{Name: types.Int64}},
			{Name: "y", Type: types.PrimitiveType{Name: types.Int64}},
		},
	}
	b := types.AnonymousStructType{
		Fields: []types.StructField{
			{Name: "x", Type: types.PrimitiveType{Name: types.Int64}},
			{Name: "z", Type: types.PrimitiveType{Name: types.Int64}},
		},
	}
	if types.TypesEqual(a, b) {
		t.Fatalf("expected %s and %s to be unequal", a, b)
	}
}

// A named tuple (`tuple Point(i32, i32)`) is nominal — Pit-of-Success #8
// (todo.md) decided named tuples are "positional nominal", matching
// NamedStructType's name-only equality above. Before this fix, TypeType's case
// ignored Name entirely and compared purely by element shape.
func TestTypesEqualNamedTuple(t *testing.T) {
	a := types.TupleType{Name: "Point", Elements: []types.Type{types.PrimitiveType{Name: types.Int32}, types.PrimitiveType{Name: types.Int32}}}
	b := types.TupleType{Name: "Point", Elements: []types.Type{types.PrimitiveType{Name: types.Int32}, types.PrimitiveType{Name: types.Int32}}}
	if !types.TypesEqual(a, b) {
		t.Fatalf("expected %s and %s to be equal", a, b)
	}
}

// The motivating case: two named tuples with identical element shapes but
// different names must be unequal — the whole point of making them nominal.
func TestTypesNotEqualNamedTupleDifferentNames(t *testing.T) {
	shape := []types.Type{types.PrimitiveType{Name: types.Int32}, types.PrimitiveType{Name: types.Int32}}
	a := types.TupleType{Name: "Point", Elements: shape}
	b := types.TupleType{Name: "Vector", Elements: shape}
	if types.TypesEqual(a, b) {
		t.Fatalf("expected %s and %s to be unequal (same shape, different name)", a, b)
	}
}

// A named tuple is never equal to an anonymous tuple, even of the same shape
// — nominal identity, not structural.
func TestTypesNotEqualNamedTupleVsAnonymous(t *testing.T) {
	shape := []types.Type{types.PrimitiveType{Name: types.Int32}, types.PrimitiveType{Name: types.Int32}}
	named := types.TupleType{Name: "Point", Elements: shape}
	for _, anonName := range []string{"", "?"} {
		anon := types.TupleType{Name: anonName, Elements: shape}
		if types.TypesEqual(named, anon) {
			t.Fatalf("expected %s and %s (anonName=%q) to be unequal", named, anon, anonName)
		}
	}
}

// Anonymous tuples (Name == "" or "?", IsAnonymousTupleName's two sentinels)
// stay structural: same element types, different Name sentinel, still equal.
func TestTypesEqualAnonymousTupleStructural(t *testing.T) {
	a := types.TupleType{Name: "?", Elements: []types.Type{types.PrimitiveType{Name: types.Int64}, types.PrimitiveType{Name: types.String}}}
	b := types.TupleType{Name: "", Elements: []types.Type{types.PrimitiveType{Name: types.Int64}, types.PrimitiveType{Name: types.String}}}
	if !types.TypesEqual(a, b) {
		t.Fatalf("expected two anonymous tuples of the same shape to be equal regardless of Name sentinel: %s vs %s", a, b)
	}
}

func TestTypesNotEqualAnonymousTupleDifferentShape(t *testing.T) {
	a := types.TupleType{Name: "?", Elements: []types.Type{types.PrimitiveType{Name: types.Int64}, types.PrimitiveType{Name: types.Int64}}}
	b := types.TupleType{Name: "?", Elements: []types.Type{types.PrimitiveType{Name: types.Int64}, types.PrimitiveType{Name: types.String}}}
	if types.TypesEqual(a, b) {
		t.Fatalf("expected anonymous tuples of different shapes to be unequal: %s vs %s", a, b)
	}
}
