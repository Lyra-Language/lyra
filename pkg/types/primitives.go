package types

type PrimitiveTypeName string

const (
	Int     PrimitiveTypeName = "int"
	Int8    PrimitiveTypeName = "i8"
	Int16   PrimitiveTypeName = "i16"
	Int32   PrimitiveTypeName = "i32"
	Int64   PrimitiveTypeName = "i64"
	UInt    PrimitiveTypeName = "uint"
	UInt8   PrimitiveTypeName = "u8"
	UInt16  PrimitiveTypeName = "u16"
	UInt32  PrimitiveTypeName = "u32"
	UInt64  PrimitiveTypeName = "u64"
	Float   PrimitiveTypeName = "float"
	Float16 PrimitiveTypeName = "f16"
	Float32 PrimitiveTypeName = "f32"
	Float64 PrimitiveTypeName = "f64"
	Bool    PrimitiveTypeName = "bool"
	String  PrimitiveTypeName = "string"
	Char    PrimitiveTypeName = "char"
)

type PrimitiveType struct {
	Name PrimitiveTypeName
}

func (PrimitiveType) typeNode() {}

func (p PrimitiveType) GetName() string {
	return string(p.Name)
}

func (p PrimitiveType) String() string {
	return p.GetName()
}
