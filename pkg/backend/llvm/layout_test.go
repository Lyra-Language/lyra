package llvm

import (
	"testing"

	lltypes "github.com/llir/llvm/ir/types"

	"github.com/Lyra-Language/lyra/pkg/types"
)

func prim(n types.PrimitiveTypeName) types.PrimitiveType { return types.PrimitiveType{Name: n} }

func TestLLVMPrimitive(t *testing.T) {
	cases := map[types.PrimitiveTypeName]string{
		types.Int8: "i8", types.UInt8: "i8",
		types.Int64: "i64", types.UInt64: "i64",
		types.Float16: "half", types.Float32: "float", types.Float64: "double",
		types.Boolean: "i1", types.Char: "i32",
	}
	for name, want := range cases {
		got, ok := LLVMPrimitive(name)
		if !ok || got.String() != want {
			t.Errorf("LLVMPrimitive(%s) = %v,%v; want %q", name, got, ok, want)
		}
	}
	if _, ok := LLVMPrimitive(types.String); ok {
		t.Error("string representation should be deferred (ok=false)")
	}
}

func TestIsSignedInt(t *testing.T) {
	signed := []types.PrimitiveTypeName{
		types.Int8, types.Int16, types.Int32, types.Int64,
		types.UntypedInt, types.UntypedSignedInt,
	}
	for _, n := range signed {
		if !IsSignedInt(n) {
			t.Errorf("IsSignedInt(%s) = false; want true", n)
		}
	}
	unsigned := []types.PrimitiveTypeName{
		types.UInt8, types.UInt16, types.UInt32, types.UInt64,
		types.Float32, types.Float64, types.Boolean, types.Char, types.String,
	}
	for _, n := range unsigned {
		if IsSignedInt(n) {
			t.Errorf("IsSignedInt(%s) = true; want false", n)
		}
	}
}

func TestSizeAndAlign_Primitives(t *testing.T) {
	cases := []struct {
		name       types.PrimitiveTypeName
		size, algn int
	}{
		{types.Int8, 1, 1}, {types.Boolean, 1, 1},
		{types.Int16, 2, 2}, {types.Float16, 2, 2},
		{types.Int32, 4, 4}, {types.Char, 4, 4},
		{types.Int64, 8, 8}, {types.Float64, 8, 8},
	}
	for _, c := range cases {
		s, a, ok := SizeAndAlign(prim(c.name))
		if !ok || s != c.size || a != c.algn {
			t.Errorf("SizeAndAlign(%s) = %d,%d,%v; want %d,%d", c.name, s, a, ok, c.size, c.algn)
		}
	}
}

func TestSizeAndAlign_StructPadding(t *testing.T) {
	// { i8, i64 } → i8 at 0, pad to 8, i64 at 8..16 ⇒ size 16, align 8.
	st := types.NamedStructType{Fields: []types.StructField{
		{Name: "a", Type: prim(types.Int8)},
		{Name: "b", Type: prim(types.Int64)},
	}}
	s, a, ok := SizeAndAlign(st)
	if !ok || s != 16 || a != 8 {
		t.Fatalf("got %d,%d,%v; want 16,8", s, a, ok)
	}
}

func TestSizeAndAlign_SharedIsPointer(t *testing.T) {
	// A shared struct is a pointer regardless of its (huge) payload.
	st := types.NamedStructType{
		Allocation: types.Shared,
		Fields: []types.StructField{
			{Name: "a", Type: prim(types.Int64)},
			{Name: "b", Type: prim(types.Int64)},
			{Name: "c", Type: prim(types.Int64)},
		},
	}
	s, a, ok := SizeAndAlign(st)
	if !ok || s != 8 || a != 8 {
		t.Fatalf("shared struct got %d,%d,%v; want 8,8 (a ptr)", s, a, ok)
	}
}

func TestSizeAndAlign_StaticArray(t *testing.T) {
	// [3]i32 ⇒ 12, align 4.
	arr := types.StaticArrayType{ElementType: prim(types.Int32), Size: 3}
	s, a, ok := SizeAndAlign(arr)
	if !ok || s != 12 || a != 4 {
		t.Fatalf("got %d,%d,%v; want 12,4", s, a, ok)
	}
}

func TestSizeAndAlign_DeferredString(t *testing.T) {
	if _, _, ok := SizeAndAlign(prim(types.String)); ok {
		t.Error("string sizing should be deferred (ok=false)")
	}
}

func TestTagType(t *testing.T) {
	cases := map[int]string{1: "i8", 256: "i8", 257: "i16", 70000: "i32"}
	for n, want := range cases {
		if got := TagType(n).String(); got != want {
			t.Errorf("TagType(%d) = %q; want %q", n, got, want)
		}
	}
}

func TestSharedBoxType(t *testing.T) {
	node := lltypes.NewStruct()
	node.TypeName = "Node"
	if got := SharedBoxType(node).String(); got != "{ i64, %Node }" {
		t.Errorf("SharedBoxType = %q", got)
	}
}

func TestDataUnionType_MaybeLike(t *testing.T) {
	// data M = None | Some(i64) → tag i8 + one i64 payload.
	// Payload align 8, size 8 ⇒ [1 x i64]; tag i8 padded by LLVM. Type: { i8, [1 x i64] }.
	dt := types.DataType{Name: "M", Constructors: []types.DataTypeConstructor{
		{Name: "None"},
		{Name: "Some", Params: []types.Type{prim(types.Int64)}},
	}}
	got, ok := DataUnionType(dt)
	if !ok || got.String() != "{ i8, [1 x i64] }" {
		t.Fatalf("DataUnionType = %v,%v; want { i8, [1 x i64] }", got, ok)
	}
	// Whole value: tag(1) padded to 8, + payload 8 ⇒ 16, align 8.
	s, a, _ := SizeAndAlign(dt)
	if s != 16 || a != 8 {
		t.Fatalf("SizeAndAlign(M) = %d,%d; want 16,8", s, a)
	}
}

func TestDataUnionType_Enum(t *testing.T) {
	// All-nullary → just the tag, no payload blob.
	dt := types.DataType{Name: "Color", Constructors: []types.DataTypeConstructor{
		{Name: "Red"}, {Name: "Green"}, {Name: "Blue"},
	}}
	got, ok := DataUnionType(dt)
	if !ok || got.String() != "{ i8 }" {
		t.Fatalf("DataUnionType(enum) = %v,%v; want { i8 }", got, ok)
	}
}

func TestDataUnionType_MixedWidths(t *testing.T) {
	// data V = A(i8) | B(i32, i8) → largest payload is B's struct {i32,i8}=size 8, align 4.
	// Blob element = i32 (align 4), count = ceil(8/4) = 2 ⇒ [2 x i32].
	dt := types.DataType{Name: "V", Constructors: []types.DataTypeConstructor{
		{Name: "A", Params: []types.Type{prim(types.Int8)}},
		{Name: "B", Params: []types.Type{prim(types.Int32), prim(types.Int8)}},
	}}
	got, ok := DataUnionType(dt)
	if !ok || got.String() != "{ i8, [2 x i32] }" {
		t.Fatalf("DataUnionType = %v,%v; want { i8, [2 x i32] }", got, ok)
	}
}

func TestSizeAndAlign_RecursiveSharedIsFinite(t *testing.T) {
	// shared data List = Nil | Cons(i64, List): the recursive List field is
	// shared ⇒ a ptr, so Cons payload = {i64, ptr} = 16, and the whole thing is
	// itself a ptr (shared) ⇒ 8. Just assert it sizes (doesn't blow the stack).
	list := types.DataType{Name: "List", Allocation: types.Shared}
	list.Constructors = []types.DataTypeConstructor{
		{Name: "Nil"},
		{Name: "Cons", Params: []types.Type{
			prim(types.Int64),
			types.DataType{Name: "List", Allocation: types.Shared}, // shared self-ref = ptr
		}},
	}
	s, a, ok := SizeAndAlign(list)
	if !ok || s != 8 || a != 8 {
		t.Fatalf("SizeAndAlign(shared List) = %d,%d,%v; want 8,8", s, a, ok)
	}
}
