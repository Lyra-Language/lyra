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
		return TypesEqual(at.ReturnType.Type, bt.ReturnType.Type)
	case NamedStructType:
		bt, ok := b.(NamedStructType)
		return ok && at.Name == bt.Name
	case AnonymousStructType:
		bt, ok := b.(AnonymousStructType)
		if !ok {
			return false
		}
		if len(at.Fields) != len(bt.Fields) {
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
	case VoidType:
		if _, ok := b.(VoidType); ok {
			return true
		}
		return false
	case ParameterizedType:
		// Nominal: same name and identical type arguments.
		bt, ok := b.(ParameterizedType)
		if !ok || at.Name != bt.Name || len(at.TypeArguments) != len(bt.TypeArguments) {
			return false
		}
		for i := range at.TypeArguments {
			if !TypesEqual(at.TypeArguments[i], bt.TypeArguments[i]) {
				return false
			}
		}
		return true
	case RawPointerType:
		bt, ok := b.(RawPointerType)
		return ok && at.IsMut == bt.IsMut && TypesEqual(at.Pointee, bt.Pointee)
	case SelfType:
		// SelfType is equal to any other SelfType regardless of generic params;
		// the params are resolved during trait/impl checking, not here.
		_, ok := b.(SelfType)
		return ok
	case UnresolvedType:
		// Two unresolved references are equal only if they name the same symbol.
		bt, ok := b.(UnresolvedType)
		return ok && at.Name == bt.Name
	default:
		return false
	}
	return false
}
