package types

// Type-variable substitution — the one walker.
//
// `Substitute(t, {t: i64})` replaces every type variable in a type with its binding,
// rebuilding each composite around the substituted parts. It is what makes a generic body
// concrete for one instantiation, and it lives here for the reason CollectTypeVars does
// (see typevars.go): **two passes now need it, and a switch over composite types that
// exists twice drifts.** The backend has always used it to lower a specialization; the
// driver needs it to compose a caller's bindings into a callee's before the set of
// specializations is closed. A case missing from one copy would mean a type that stays
// generic in exactly one of the two, which surfaces far from the omission.
//
// What is walked mirrors CollectTypeVars exactly, and for the same reason: these are the
// type constructors a signature or body can be built out of. Where they differ is nominal
// types — CollectTypeVars deliberately does *not* descend into a NamedStructType's or
// DataType's fields, because those are bound by the declaration rather than by the
// signature mentioning it, while Substitute *does*, because building an instantiation's
// layout is precisely rewriting `struct Box<t> { v: t }` at `t = i64`. Both are right;
// they are answering different questions about the same tree.

// Substitute replaces every type variable in t with its binding from subst, leaving any
// variable the map does not mention untouched. t is not modified: each composite is
// rebuilt, since a declaration's type is shared by every instantiation and mutating it
// would let the first one lowered decide the rest.
func Substitute(t Type, subst map[string]Type) Type {
	if len(subst) == 0 || t == nil {
		return t
	}
	switch tt := t.(type) {
	case GenericType:
		if concrete, ok := subst[tt.Name]; ok {
			return concrete
		}
		return tt
	case StaticArrayType:
		tt.ElementType = Substitute(tt.ElementType, subst)
		return tt
	case DynamicArrayType:
		tt.ElementType = Substitute(tt.ElementType, subst)
		return tt
	case TupleType:
		elems := make([]Type, len(tt.Elements))
		for i, e := range tt.Elements {
			elems[i] = Substitute(e, subst)
		}
		tt.Elements = elems
		return tt
	case WeakType:
		tt.Inner = Substitute(tt.Inner, subst)
		return tt
	case RawPointerType:
		tt.Pointee = Substitute(tt.Pointee, subst)
		return tt
	case ParameterizedType:
		// `Box<t>` inside a generic body, and the nested arguments of `Box<Box<i64>>`.
		// Substituting these is what makes one instantiation's identity concrete —
		// without it `Box<t>` at two different bindings mangles to the same name and the
		// two would share a layout.
		args := make([]Type, len(tt.TypeArguments))
		for i, a := range tt.TypeArguments {
			args[i] = Substitute(a, subst)
		}
		tt.TypeArguments = args
		return tt
	case NamedStructType:
		fields := make([]StructField, len(tt.Fields))
		copy(fields, tt.Fields)
		for i := range fields {
			fields[i].Type = Substitute(fields[i].Type, subst)
		}
		tt.Fields = fields
		return tt
	case DataType:
		ctors := make([]DataTypeConstructor, len(tt.Constructors))
		copy(ctors, tt.Constructors)
		for i := range ctors {
			params := make([]Type, len(ctors[i].Params))
			for j, p := range ctors[i].Params {
				params[j] = Substitute(p, subst)
			}
			ctors[i].Params = params
		}
		tt.Constructors = ctors
		return tt
	case AnonymousStructType:
		fields := make([]StructField, len(tt.Fields))
		copy(fields, tt.Fields)
		for i := range fields {
			fields[i].Type = Substitute(fields[i].Type, subst)
		}
		tt.Fields = fields
		return tt
	case *LambdaType:
		// A function-typed parameter, `f: (t) -> u`. Missing from the backend's copy
		// before this moved here, which was invisible while nothing substituted into a
		// signature that carried one — a generic combinator taking a callback is exactly
		// the shape that does.
		if tt == nil {
			return tt
		}
		out := *tt
		params := make([]ParameterType, len(tt.Parameters))
		copy(params, tt.Parameters)
		for i := range params {
			params[i].Type = Substitute(params[i].Type, subst)
		}
		out.Parameters = params
		out.ReturnType.Type = Substitute(tt.ReturnType.Type, subst)
		return &out
	case *ConstrainedType:
		if tt == nil {
			return tt
		}
		out := *tt
		out.Type = Substitute(tt.Type, subst)
		return &out
	case RangeType:
		tt.Start = Substitute(tt.Start, subst)
		tt.End = Substitute(tt.End, subst)
		tt.Step = Substitute(tt.Step, subst)
		return tt
	}
	return t
}
