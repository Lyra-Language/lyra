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
// they are answering different questions about the same tree. `SelfType` is the other
// difference: Substitute binds it (under SelfVar) and CollectTypeVars reports only a
// `Self<a>`'s arguments, because `Self` is fixed by the impl, never solved from a call.

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
	case SelfType:
		// A trait signature's `Self`, bound under SelfVar. It is a type of its own rather
		// than a GenericType, so without this case it was replaced only where a caller
		// checked for it at the top — `(Self) -> Self` worked and `(Self) -> []Self` did not.
		// `Self<a>`'s arguments are ordinary types: a rename of `a` (the impl check's
		// clash rename) and a concrete binding both reach them here, once.
		if len(tt.Args) > 0 {
			args := make([]Type, len(tt.Args))
			for i, a := range tt.Args {
				args[i] = Substitute(a, subst)
			}
			tt.Args = args
		}
		concrete, ok := subst[SelfVar]
		if !ok {
			return tt
		}
		if len(tt.Args) == 0 {
			return concrete
		}
		// `Self<a>` is the implementing type's head at new arguments. One that cannot be
		// applied stays `Self<a>`: a default body is checked with `Self` a bare variable,
		// which has no head, and the impl check refuses a target that has none
		// (SelfApplicable), so what a refused impl's signature says is not read further.
		applied, ok := ApplySelf(concrete, tt.Args)
		if !ok {
			return tt
		}
		return applied
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
	case UnionType:
		// The same rebuild a struct gets, and for the same reason: an instantiation's
		// layout is the declaration rewritten at its arguments. A union's *layout* is
		// max-over-members rather than a sum of offsets, but that is SizeAndAlign's
		// concern; substitution only has to reach every member type.
		members := make([]StructField, len(tt.Members))
		copy(members, tt.Members)
		for i := range members {
			members[i].Type = Substitute(members[i].Type, subst)
		}
		tt.Members = members
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

// ApplySelf is `Self<x1, …, xn>` for an implementing type: that type's head at the variables
// x1…xn. `Self<b>` for `Box<i64>` is `Box<b>`.
//
// Defined only where it has one reading — a parameterized type with exactly n arguments.
// `Self<a>` for `Result<t, e>` could replace the first argument or the last, and for `i64`
// there is no head to apply; both answer false, and the impl check refuses such an impl
// rather than choose (SelfApplicable).
func ApplySelf(concrete Type, args []Type) (Type, bool) {
	pt, ok := concrete.(ParameterizedType)
	if !ok {
		return nil, false
	}
	// A target with holes says which positions the arguments fill — `Result<_, e>` at
	// `Self<b>` is `Result<b, e>`; the rest stays as the impl fixed it.
	if holes := countHoles(pt.TypeArguments); holes > 0 {
		if holes != len(args) {
			return nil, false
		}
		filled := make([]Type, len(pt.TypeArguments))
		next := 0
		for i, a := range pt.TypeArguments {
			if _, isHole := a.(HoleType); isHole {
				filled[i] = args[next]
				next++
			} else {
				filled[i] = a
			}
		}
		pt.TypeArguments = filled
		return pt, true
	}
	if len(pt.TypeArguments) != len(args) {
		return nil, false
	}
	pt.TypeArguments = append([]Type(nil), args...)
	return pt, true
}

func countHoles(args []Type) int {
	n := 0
	for _, a := range args {
		if _, ok := a.(HoleType); ok {
			n++
		}
	}
	return n
}

// SelfApplicable reports whether an impl target can stand for `Self<…>` at arity n: a
// parameterized type applied to n **distinct type variables**, like `Box<t>` or `Pair<k, v>`,
// or one with exactly n holes, like `Result<_, e>` — a hole marks the position an argument
// fills, and what is not a hole stays as the impl fixed it. Without holes, a concrete
// argument (`Box<i64>`) or a repeated one (`Pair<t, t>`) would make `Self<b>` forget what
// the impl said about that position.
func SelfApplicable(target Type, n int) bool {
	pt, ok := target.(ParameterizedType)
	if !ok {
		return false
	}
	if holes := countHoles(pt.TypeArguments); holes > 0 {
		return holes == n
	}
	if len(pt.TypeArguments) != n {
		return false
	}
	seen := map[string]bool{}
	for _, a := range pt.TypeArguments {
		g, isVar := a.(GenericType)
		if !isVar || seen[g.Name] {
			return false
		}
		seen[g.Name] = true
	}
	return true
}
