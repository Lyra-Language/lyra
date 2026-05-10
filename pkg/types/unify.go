package types

// TypesEqual checks structural equality of two types
func TypesEqual(a, b Type) bool {
	switch at := a.(type) {
	case nil:
		if b == nil {
			return true
		}
		return false
	case PrimitiveType:
		if bt, ok := b.(PrimitiveType); ok {
			return at.Name == bt.Name
		}
	case GenericType:
		if bt, ok := b.(GenericType); ok {
			return at.Name == bt.Name
		}
	case StaticArrayType:
		if bt, ok := b.(StaticArrayType); ok {
			if at.Size != bt.Size {
				return false
			}
			return TypesEqual(at.ElementType, bt.ElementType)
		}
	case DynamicArrayType:
		if bt, ok := b.(DynamicArrayType); ok {
			return TypesEqual(at.ElementType, bt.ElementType)
		}
		return false
	case *LambdaType:
		// LambdaType uses the pointer convention: it is constructed as
		// *types.LambdaType everywhere (see ast.FunctionDefStmt.Signature,
		// ast.TraitMethod.Signature, Collector.parseLambdaType). The type
		// switch must match the pointer form or equality silently returns false.
		bt, ok := b.(*LambdaType)
		if !ok {
			return false
		}
		if at == nil || bt == nil {
			return at == bt
		}
		if len(at.Parameters) != len(bt.Parameters) {
			return false
		}
		for i := range at.Parameters {
			if !TypesEqual(at.Parameters[i].Type, bt.Parameters[i].Type) {
				return false
			}
		}
		return TypesEqual(at.ReturnType, bt.ReturnType)
	case StructType:
		if bt, ok := b.(StructType); ok {
			if at.Name != bt.Name {
				return false
			}
			for _, aField := range at.Fields {
				found := false
				for _, bField := range bt.Fields {
					if bField.Name == aField.Name {
						if !TypesEqual(aField.Type, bField.Type) {
							return false
						}
						found = true
						break
					}
				}
				if !found {
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
	case *ConstrainedType:
		// ConstrainedType uses the pointer convention: it is constructed as
		// *types.ConstrainedType in Collector.parseConstrainedType and
		// typedecls.collectConstrainedTypeDeclaration. Like DataType, it is a
		// nominal declared type so name equality is sufficient.
		bt, ok := b.(*ConstrainedType)
		if !ok {
			return false
		}
		if at == nil || bt == nil {
			return at == bt
		}
		return at.Name == bt.Name
	case RangeType:
		if bt, ok := b.(RangeType); ok {
			if !TypesEqual(at.Start, bt.Start) {
				return false
			}
			if at.EndOperator != bt.EndOperator {
				return false
			}
			if !TypesEqual(at.End, bt.End) {
				return false
			}
			if at.Step != nil && bt.Step != nil {
				return TypesEqual(at.Step, bt.Step)
			}
			return true
		}
	case ReturnType:
		bt, ok := b.(ReturnType)
		if !ok {
			return false
		}
		if at.Type == nil || bt.Type == nil {
			return at.Type == bt.Type
		}
		return TypesEqual(at.Type, bt.Type)
	case VoidType:
		if _, ok := b.(VoidType); ok {
			return true
		}
		return false
	default:
		return false
	}
	return false
}
