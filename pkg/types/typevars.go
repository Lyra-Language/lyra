package types

// Type variables in a signature — the one walker.
//
// A lowercase type name is a type variable (the collector turns it into a
// GenericType); an uppercase one is a concrete type. Three passes need to ask
// "which variables does this signature mention?", and until this file they each
// asked it with their own switch:
//
//   - the typechecker, to decide whether a binding is generic and what a call
//     site must solve (`lambdaTypeVars`)
//   - the backend, to decide whether a function must be monomorphized before it
//     can be laid out (`mentionsTypeVar`)
//   - the checker, to reconcile a written generic parameter list against the
//     signature it belongs to (`CheckGenericParams`)
//
// The copies drifted, exactly as hazard 8 in the project's CLAUDE.md predicts a
// switch over composite types will: the backend's was missing ParameterizedType,
// so `is_some<t> = (m: Maybe<t>) -> bool` read as non-generic, was emitted under
// its bare name, and failed in layout — no program could build with the prelude
// present (the 07/30 build failure). Consolidating them here is the fix that
// entry asks for, and the union of the two switches turned up two cases *neither*
// had: AnonymousStructType and RangeType.
//
// **What is deliberately not walked: nominal types.** NamedStructType, UnionType and
// DataType carry their own declaration's parameters (`struct Box<t> { v: t }`),
// and those are bound by that declaration, not by the signature mentioning it. A
// function taking a `Box<i64>` mentions no variable of its own; descending into
// the struct would report `t` as a use and make every function touching a generic
// type spuriously generic. Only *structural* composition is traversed — the type
// constructors a signature builds out of its own parts.

// CollectTypeVars adds every type variable in t to vars, descending through every
// structural type constructor a signature can be built from. vars must be
// non-nil.
//
// `Self<a>`'s `a` is one: in a trait method's signature it is the method's own variable,
// written as an argument to `Self` rather than to a named type. `Self` itself is not — the
// impl fixes it, and no call solves it.
func CollectTypeVars(t Type, vars map[string]bool) {
	walkSignature(t, func(t Type) {
		switch tt := t.(type) {
		case GenericType:
			vars[tt.Name] = true
		case SelfType:
			for _, p := range tt.GenericParams {
				vars[p] = true
			}
		}
	})
}

// SelfApplications adds the argument count of every applied `Self<…>` in t to arities — the
// 1 of `Self<a>`. A bare `Self` adds nothing. The same walk as CollectTypeVars, asked a
// different question, so an impl's `Self<…>` check reaches every position a variable does.
func SelfApplications(t Type, arities map[int]bool) {
	walkSignature(t, func(t Type) {
		if st, ok := t.(SelfType); ok && len(st.GenericParams) > 0 {
			arities[len(st.GenericParams)] = true
		}
	})
}

// walkSignature calls visit on t and on every type structurally inside it — the one
// traversal behind CollectTypeVars and SelfApplications. What it descends into, and what it
// deliberately does not (a nominal type's own fields), is the file comment above.
func walkSignature(t Type, visit func(Type)) {
	if t == nil {
		return
	}
	visit(t)
	switch tt := t.(type) {
	case StaticArrayType:
		walkSignature(tt.ElementType, visit)
	case DynamicArrayType:
		walkSignature(tt.ElementType, visit)
	case TupleType:
		for _, e := range tt.Elements {
			walkSignature(e, visit)
		}
	case AnonymousStructType:
		// Structural, unlike a NamedStructType: `(p: { v: t }) -> t` writes the
		// field types out in the signature, so `t` is this signature's variable.
		for _, f := range tt.Fields {
			walkSignature(f.Type, visit)
		}
	case WeakType:
		walkSignature(tt.Inner, visit)
	case RawPointerType:
		walkSignature(tt.Pointee, visit)
	case ParameterizedType:
		// `Maybe<t>`: the variable is in the type arguments, never at the leaf.
		// The type's *own* parameters are its declaration's business; only what
		// this signature applied it to is ours.
		for _, a := range tt.TypeArguments {
			walkSignature(a, visit)
		}
	case RangeType:
		walkSignature(tt.Start, visit)
		walkSignature(tt.End, visit)
		walkSignature(tt.Step, visit)
	case *LambdaType:
		// A function-typed parameter, e.g. `f: (t) -> u` — the callback's own
		// signature is as much a part of this signature as any other parameter.
		if tt == nil {
			return
		}
		for _, p := range tt.Parameters {
			walkSignature(p.Type, visit)
		}
		walkSignature(tt.ReturnType.Type, visit)
	case *ConstrainedType:
		if tt == nil {
			return
		}
		walkSignature(tt.Type, visit)
	}
}

// MentionsTypeVar reports whether t mentions any type variable — i.e. whether a
// value of t still needs a substitution before it can be laid out.
//
// Defined over CollectTypeVars rather than as its own short-circuiting switch:
// that is the whole point of this file, and a second switch is what drifted last
// time. The cost is a map allocation and no early exit, against a walk over one
// signature's types — not a hot path in any caller.
func MentionsTypeVar(t Type) bool {
	vars := map[string]bool{}
	CollectTypeVars(t, vars)
	return len(vars) > 0
}

// CollectTypeNames adds every **nominal** type name mentioned in t to names — the mirror
// of CollectTypeVars, which collects the *variables* and deliberately skips the names.
//
// Written for the unused-import check, which asks a question no other pass asks: "does this
// file mention that name anywhere". A reference to an imported *type* appears only in a
// type position (`(c: Complex<f64>)`, `-> Complex<f64>`, a field's type), and the checker
// walked expressions alone — so `import std.math.{ Complex }` warned as unused in a program
// that did not compile without it, which is the failure mode the check's own UFCS comment
// already describes.
//
// Unlike CollectTypeVars this **does** descend into a nominal head: `Maybe<Complex<f64>>`
// mentions both names, and the question here is mention rather than binding. The structural
// composites are walked identically, which is why the two live side by side — a second walk
// that drifted would answer a use as an absence, and the symptom would be advice to delete
// a load-bearing import.
func CollectTypeNames(t Type, names map[string]bool) {
	switch tt := t.(type) {
	case UnresolvedType:
		names[tt.Name] = true
	case NamedStructType:
		names[tt.Name] = true
	case DataType:
		names[tt.Name] = true
	case *ConstrainedType:
		names[tt.Name] = true
	case ParameterizedType:
		names[tt.Name] = true
		for _, a := range tt.TypeArguments {
			CollectTypeNames(a, names)
		}
	case StaticArrayType:
		CollectTypeNames(tt.ElementType, names)
	case DynamicArrayType:
		CollectTypeNames(tt.ElementType, names)
	case TupleType:
		if tt.Name != "" {
			names[tt.Name] = true
		}
		for _, e := range tt.Elements {
			CollectTypeNames(e, names)
		}
	case AnonymousStructType:
		for _, f := range tt.Fields {
			CollectTypeNames(f.Type, names)
		}
	case WeakType:
		CollectTypeNames(tt.Inner, names)
	case RawPointerType:
		CollectTypeNames(tt.Pointee, names)
	case RangeType:
		CollectTypeNames(tt.Start, names)
		CollectTypeNames(tt.End, names)
		CollectTypeNames(tt.Step, names)
	case *LambdaType:
		if tt == nil {
			return
		}
		for _, p := range tt.Parameters {
			CollectTypeNames(p.Type, names)
		}
		CollectTypeNames(tt.ReturnType.Type, names)
	}
}
