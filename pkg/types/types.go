package types

type Type interface {
	typeNode()
	IsNumericType() bool
}

func (PrimitiveType) typeNode() {}
func (ArrayType) typeNode()     {}
func (FunctionType) typeNode()  {}
func (GenericType) typeNode()   {}
func (StructType) typeNode()    {}
func (DataType) typeNode()      {}
func (MapType) typeNode()       {}
func (TupleType) typeNode()     {}

func (p PrimitiveType) IsNumericType() bool {
	return p.Name == "Int" || p.Name == "Float"
}
func (a ArrayType) IsNumericType() bool {
	return false
}
func (f FunctionType) IsNumericType() bool {
	return false
}
func (g GenericType) IsNumericType() bool {
	return false
}
func (s StructType) IsNumericType() bool {
	return false
}
func (d DataType) IsNumericType() bool {
	return false
}
func (m MapType) IsNumericType() bool {
	return false
}
func (t TupleType) IsNumericType() bool {
	return false
}

type PrimitiveType struct {
	Name string // Int, Float, Bool, String
}

type ArrayType struct {
	ElementType Type
}

type FunctionType struct {
	Parameters []NamedParameter
	ReturnType Type
}

type NamedParameter struct {
	Name string
	Type Type
}

type GenericType struct {
	Name string // lowercase letter optionally followed by any number of letters or numbers
}

type StructType struct {
	Name   string // uppercase letter optionally followed by any number of letters or numbers
	Fields map[string]Type
}

type MapType struct {
	KeyType   Type
	ValueType Type
}

type TupleType struct {
	Elements []Type
}

type DataType struct {
	Name         string // uppercase letter optionally followed by any number of letters or numbers
	Constructors map[string]DataTypeConstructor
}

// Data constructor can have different shapes
type DataTypeConstructor struct {
	Name   string
	Params []Type          // for Simple(Int) style
	Fields map[string]Type // for Node { left: Tree, value: t } style
}
