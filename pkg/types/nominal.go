package types

import "strings"

// Nominal identity across modules.
//
// A declared struct, `data` type, union or named tuple is identified by its **declaration**,
// and two modules may each declare a `Point`. The name alone identifies it only inside the
// module that wrote it; once a value carries the type elsewhere — `two.make()` returning
// two's `Point` into a module that imports one's — the name means something else there. So
// the declaration's key (`<module>::<name>`, SymbolTable.DeclKey) rides on the type, set
// where the collector builds a declaration and kept by every copy.

// NominalKey is a nominal type's declaration key, or "" for a type without one: anything
// not nominal, an anonymous tuple, or a nominal type built where no declaration was in hand.
func NominalKey(t Type) string {
	switch v := t.(type) {
	case NamedStructType:
		return v.Key
	case DataType:
		return v.Key
	case UnionType:
		return v.Key
	case TupleType:
		return v.Key
	}
	return ""
}

// WithNominalKey returns t with its declaration key set, for the four nominal kinds; any
// other type is returned unchanged.
func WithNominalKey(t Type, key string) Type {
	switch v := t.(type) {
	case NamedStructType:
		v.Key = key
		return v
	case DataType:
		v.Key = key
		return v
	case UnionType:
		v.Key = key
		return v
	case TupleType:
		if !IsAnonymousTupleName(v.Name) {
			v.Key = key
		}
		return v
	}
	return t
}

// IdentityString renders t as String does, except that a nominal type declared in a named
// module is spelled with its module — `two.Point` — so two modules' types of one name render
// differently. It is for **identities**, never for messages: an instantiation's key and its
// emitted symbol (typetable), where `Opt<Point>` at one's `Point` and at two's must be two
// instantiations. Only a type whose name is not program-wide carries a key
// (SymbolTable.KeyAmbiguousTypes), so every other type keeps its bare spelling.
func IdentityString(t Type) string {
	if t == nil {
		return "<nil>"
	}
	return qualifyNominal(t).String()
}

// qualifyNominal rewrites every nominal name t mentions to its module-qualified spelling.
// It descends what String descends — the composites a type is spelled out of — and not a
// nominal type's own fields, which String does not render.
func qualifyNominal(t Type) Type {
	switch v := t.(type) {
	case NamedStructType:
		v.Name = qualifiedSpelling(v.Name, v.Key)
		return v
	case DataType:
		v.Name = qualifiedSpelling(v.Name, v.Key)
		return v
	case UnionType:
		v.Name = qualifiedSpelling(v.Name, v.Key)
		return v
	case TupleType:
		if !IsAnonymousTupleName(v.Name) {
			v.Name = qualifiedSpelling(v.Name, v.Key)
			return v
		}
		elems := make([]Type, len(v.Elements))
		for i, e := range v.Elements {
			elems[i] = qualifyNominal(e)
		}
		v.Elements = elems
		return v
	case StaticArrayType:
		v.ElementType = qualifyNominal(v.ElementType)
		return v
	case DynamicArrayType:
		v.ElementType = qualifyNominal(v.ElementType)
		return v
	case WeakType:
		v.Inner = qualifyNominal(v.Inner)
		return v
	case RawPointerType:
		v.Pointee = qualifyNominal(v.Pointee)
		return v
	case ParameterizedType:
		args := make([]Type, len(v.TypeArguments))
		for i, a := range v.TypeArguments {
			args[i] = qualifyNominal(a)
		}
		v.TypeArguments = args
		return v
	case AnonymousStructType:
		fields := make([]StructField, len(v.Fields))
		copy(fields, v.Fields)
		for i := range fields {
			fields[i].Type = qualifyNominal(fields[i].Type)
		}
		v.Fields = fields
		return v
	case *LambdaType:
		if v == nil {
			return v
		}
		out := *v
		params := make([]ParameterType, len(v.Parameters))
		copy(params, v.Parameters)
		for i := range params {
			params[i].Type = qualifyNominal(params[i].Type)
		}
		out.Parameters = params
		out.ReturnType.Type = qualifyNominal(v.ReturnType.Type)
		return &out
	}
	return t
}

// qualifiedSpelling is `module.Name` for a keyed type, with the unnamed entry module spelled
// `entry`, and the bare name for a type without a key.
func qualifiedSpelling(name, key string) string {
	module, _, found := strings.Cut(key, "::")
	if !found {
		return name
	}
	if module == "" {
		module = "entry"
	}
	return module + "." + name
}
