package types

type PrimitiveTypeName string

const (
	Int8             PrimitiveTypeName = "i8"
	Int16            PrimitiveTypeName = "i16"
	Int32            PrimitiveTypeName = "i32"
	Int64            PrimitiveTypeName = "i64"
	Int128           PrimitiveTypeName = "i128"
	UInt8            PrimitiveTypeName = "u8"
	UInt16           PrimitiveTypeName = "u16"
	UInt32           PrimitiveTypeName = "u32"
	UInt64           PrimitiveTypeName = "u64"
	UInt128          PrimitiveTypeName = "u128"
	Float16          PrimitiveTypeName = "f16"
	Float32          PrimitiveTypeName = "f32"
	Float64          PrimitiveTypeName = "f64"
	UntypedInt       PrimitiveTypeName = "untyped_int"
	UntypedSignedInt PrimitiveTypeName = "untyped_signed_int"
	UntypedFloat     PrimitiveTypeName = "untyped_float"
	// UntypedNullPtr is `nullptr` before a context has said what it points at.
	// It is an untyped literal on exactly the model of UntypedInt — the
	// difference is that there is no default to fall back to, since no pointee
	// is more plausible than another, so an unpinned one is lyra-E069 rather
	// than a widening. Assignable to every RawPointerType and to nothing else.
	UntypedNullPtr PrimitiveTypeName = "untyped_nullptr"
	Boolean        PrimitiveTypeName = "boolean"
	String         PrimitiveTypeName = "string"
	Rune           PrimitiveTypeName = "rune"
	Regex          PrimitiveTypeName = "regex"
)

type PrimitiveType struct {
	Name PrimitiveTypeName
}

func (PrimitiveType) typeNode() {}

func (p PrimitiveType) GetName() string {
	switch p.Name {
	case UntypedInt, UntypedSignedInt:
		return "integer literal"
	case UntypedFloat:
		return "float literal"
	case UntypedNullPtr:
		return "null pointer literal"
	default:
		return string(p.Name)
	}
}

func (p PrimitiveType) String() string {
	return p.GetName()
}
