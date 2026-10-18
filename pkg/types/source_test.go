package types

import "testing"

// Source is what a person reads — every diagnostic since 10/17 — and SourceSettled what a
// signature shows. Each row is a kind whose String() is not Lyra, or a case where the two
// spellings part.
func TestSource_SpellsTypesAsAProgramWritesThem(t *testing.T) {
	i64 := PrimitiveType{Name: Int64}
	u8 := PrimitiveType{Name: UInt8}
	lit := PrimitiveType{Name: UntypedInt}
	node := NamedStructType{Name: "Node", Allocation: Stack}
	cases := []struct {
		name            string
		t               Type
		source, settled string
	}{
		{"bool", PrimitiveType{Name: Boolean}, "bool", "bool"},
		{"an untyped literal", lit, "integer literal", "i64"},
		{"a dynamic array", DynamicArrayType{ElementType: u8}, "[]u8", "[]u8"},
		{"a fixed array", StaticArrayType{ElementType: i64, Size: 3}, "[3]i64", "[3]i64"},
		{"a const-sized array", StaticArrayType{ElementType: i64, SizeVar: "N"}, "[N]i64", "[N]i64"},
		{"an anonymous tuple", TupleType{Name: "?", Elements: []Type{u8, ParameterizedType{Name: "Maybe", TypeArguments: []Type{PrimitiveType{Name: Int16}}}}},
			"(u8, Maybe<i16>)", "(u8, Maybe<i16>)"},
		{"nesting", DynamicArrayType{ElementType: StaticArrayType{ElementType: lit, Size: 2}}, "[][2]integer literal", "[][2]i64"},
		{"an anonymous struct", AnonymousStructType{Fields: []StructField{{Name: "x", Type: i64}}}, "{ x: i64 }", "{ x: i64 }"},
		// The default flavor is never written in a diagnostic; `shared` always is.
		{"a stack struct", WeakType{Inner: node}, "weak Node", "weak stack Node"},
		{"a shared array", DynamicArrayType{ElementType: i64, Allocation: Shared}, "shared []i64", "shared []i64"},
		{"a function type's modes", &LambdaType{Parameters: []ParameterType{{Borrow: Mut, Type: SelfType{}}, {Type: i64}},
			ReturnType: ReturnType{Type: VoidType{}}}, "(mut Self, i64) -> void", "(mut Self, i64) -> void"},
		{"a pointer", RawPointerType{Pointee: u8, IsMut: true}, "^mut u8", "^mut u8"},
		{"nil", nil, "?", "?"},
		{"a nil function type", (*LambdaType)(nil), "?", "?"},
	}
	for _, c := range cases {
		if got := Source(c.t); got != c.source {
			t.Errorf("%s: Source = %q, want %q", c.name, got, c.source)
		}
		if got := SourceSettled(c.t); got != c.settled {
			t.Errorf("%s: SourceSettled = %q, want %q", c.name, got, c.settled)
		}
	}
}

// String() is an identity — mangled into symbols — so Source must leave it alone.
func TestSource_LeavesStringAsTheCompilersName(t *testing.T) {
	if got := (DynamicArrayType{ElementType: PrimitiveType{Name: UInt8}}).String(); got != "DynamicArray<u8>" {
		t.Errorf("String() = %q; symbols are keyed on it and must not change", got)
	}
}

// SourceAnnotation is what an editor inserts as an annotation: a literal at its default, as
// a signature writes it, but no `stack`, as a diagnostic writes it.
func TestSourceAnnotation_IsAnAnnotationThatCompiles(t *testing.T) {
	lit := PrimitiveType{Name: UntypedInt}
	cases := []struct {
		t    Type
		want string
	}{
		{DynamicArrayType{ElementType: lit}, "[]i64"},
		{WeakType{Inner: NamedStructType{Name: "Node", Allocation: Stack}}, "weak Node"},
		{DynamicArrayType{ElementType: PrimitiveType{Name: Boolean}, Allocation: Shared}, "shared []bool"},
	}
	for _, c := range cases {
		if got := SourceAnnotation(c.t); got != c.want {
			t.Errorf("SourceAnnotation = %q, want %q", got, c.want)
		}
	}
	p := ParameterType{Borrow: Mut, Name: "value", Type: PrimitiveType{Name: Int64}}
	if got := SourceParameter(p); got != "value: mut i64" {
		t.Errorf("SourceParameter = %q, want %q", got, "value: mut i64")
	}
}
