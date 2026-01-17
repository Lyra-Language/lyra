package types

import "fmt"

type PrimitiveTypeName string

const (
	Int     PrimitiveTypeName = "Int"
	Int8    PrimitiveTypeName = "Int8"
	Int16   PrimitiveTypeName = "Int16"
	Int32   PrimitiveTypeName = "Int32"
	Int64   PrimitiveTypeName = "Int64"
	UInt    PrimitiveTypeName = "UInt"
	UInt8   PrimitiveTypeName = "UInt8"
	UInt16  PrimitiveTypeName = "UInt16"
	UInt32  PrimitiveTypeName = "UInt32"
	UInt64  PrimitiveTypeName = "UInt64"
	Float   PrimitiveTypeName = "Float"
	Float16 PrimitiveTypeName = "Float16"
	Float32 PrimitiveTypeName = "Float32"
	Float64 PrimitiveTypeName = "Float64"
	Bool    PrimitiveTypeName = "Bool"
	String  PrimitiveTypeName = "String"
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
