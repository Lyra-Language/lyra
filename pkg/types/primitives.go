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
	Boolean          PrimitiveTypeName = "boolean"
	String           PrimitiveTypeName = "string"
	Rune             PrimitiveTypeName = "rune"
	Regex            PrimitiveTypeName = "regex"
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
	default:
		return string(p.Name)
	}
}

func (p PrimitiveType) String() string {
	return p.GetName()
}
