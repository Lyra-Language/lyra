package types

// TypesEqual checks structural equality of two types
func TypesEqual(a, b Type) bool {
	switch at := a.(type) {
	case PrimitiveType:
		if bt, ok := b.(PrimitiveType); ok {
			return at.Name == bt.Name
		}
	case ArrayType:
		if bt, ok := b.(ArrayType); ok {
			return TypesEqual(at.ElementType, bt.ElementType)
		}
	case FunctionType:
		if bt, ok := b.(FunctionType); ok {
			if len(at.ParameterTypes) != len(bt.ParameterTypes) {
				return false
			}
			for i := range at.ParameterTypes {
				if !TypesEqual(at.ParameterTypes[i].Type, bt.ParameterTypes[i].Type) {
					return false
				}
			}
			return TypesEqual(at.ReturnType, bt.ReturnType)
		}
	case StructType:
		if bt, ok := b.(StructType); ok {
			if at.Name != bt.Name {
				return false
			}
			for name, aFieldType := range at.Fields {
				if bFieldType, ok := bt.Fields[name]; !ok || !TypesEqual(aFieldType.Type, bFieldType.Type) {
					return false
				}
			}
			return true
		}
	case TupleType:
		if bt, ok := b.(TupleType); ok {
			if len(at.Elements) != len(bt.Elements) {
				return false
			}
			for i := range at.Elements {
				if !TypesEqual(at.Elements[i], bt.Elements[i]) {
					return false
				}
			}
			return true
		}
	case DataType:
		if bt, ok := b.(DataType); ok {
			return at.Name == bt.Name
		}
	default:
		return false
	}
	return false
}
