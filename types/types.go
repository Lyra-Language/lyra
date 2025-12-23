package types

type Type interface {
	typeNode()
}

func (PrimitiveType) typeNode()   {}
func (ArrayType) typeNode()       {}
func (FunctionType) typeNode()    {}
func (GenericType) typeNode()     {}
func (UserDefinedType) typeNode() {}
func (StructType) typeNode()      {}
func (DataType) typeNode()        {}
func (MapType) typeNode()         {}
func (TupleType) typeNode()       {}

type PrimitiveType struct {
	Name string // Int, Float, Bool, String
}

type ArrayType struct {
	ElementType Type
}

type FunctionType struct {
	Parameters []Type
	ReturnType Type
}

type GenericType struct {
	Name string // lowercase letter optionally followed by any number of letters or numbers
}

type UserDefinedType struct {
	Name     string // uppercase letter optionally followed by any number of letters or numbers
	TypeArgs []Type
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
