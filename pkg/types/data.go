package types

type DataType struct {
	Name         string // uppercase letter optionally followed by any number of letters or numbers
	Constructors []DataTypeConstructor
	Allocation   AllocationModifier
}

func (DataType) typeNode() {}
func (d DataType) GetName() string {
	return d.Name
}

func (d DataType) String() string {
	return d.GetName()
}

type DataTypeConstructor struct {
	Name   string
	Params []Type
}

// FieldTypes returns the constructor's payload field types, flattened. The
// collector wraps a positional constructor's fields in a single anonymous tuple
// (`Rect(i64, i64)` → Params = [TupleType{i64, i64}]; even one field:
// `Circle(i64)` → [TupleType{i64}]), so a lone anonymous-tuple param is unwrapped
// to its elements — matching the flat positional arguments a construction supplies
// (`Rect(3, 4)`). A nullary constructor returns an empty slice; a single
// non-tuple param (an inline record's anonymous struct, a struct reference) is
// returned as-is.
func (c DataTypeConstructor) FieldTypes() []Type {
	if len(c.Params) == 1 {
		if tt, ok := c.Params[0].(TupleType); ok && IsAnonymousTupleName(tt.Name) {
			return tt.Elements
		}
	}
	return c.Params
}
