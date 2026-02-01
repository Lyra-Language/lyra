package types

import "fmt"

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
)

type PrimitiveType struct {
	Name PrimitiveTypeName
}

func (PrimitiveType) typeNode() {}

func (p PrimitiveType) GetName() string {
	return string(p.Name)
}

func (p PrimitiveType) IsNumericType() bool {
	isInt := p.Name == Int || p.Name == Int8 || p.Name == Int16 || p.Name == Int32 || p.Name == Int64
	isUInt := p.Name == UInt || p.Name == UInt8 || p.Name == UInt16 || p.Name == UInt32 || p.Name == UInt64
	isFloat := p.Name == Float || p.Name == Float16 || p.Name == Float32 || p.Name == Float64
	return isInt || isUInt || isFloat
}

func (p PrimitiveType) Print(indent string) {
	fmt.Printf("%s%s\n", indent, p.GetName())
}
