package llvm

import (
	"fmt"

	lltypes "github.com/llir/llvm/ir/types"

	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/types"
)

// Generic *types* are monomorphized the same way generic functions are — one
// emitted thing per distinct instantiation, built **by substitution** rather than by
// cloning the declaration — but they are materialized *lazily*, on first use, rather
// than from a table of instantiations collected up front.
//
// Lazily, because unlike a function call there is no single syntactic site that
// "uses" a type. `Box<i64>` can enter codegen as a construction, a parameter, a
// return, a field of another type, an array element, or a type argument of another
// generic — and every one of those already funnels through lowerType. Collecting
// instantiations in a separate pass would mean re-deriving that set by walking the
// AST *and* the TypeTable and keeping the two in agreement; asking lowerType to
// materialize what it is handed cannot fall out of sync with what is actually used.
// It also means an uninstantiated generic type costs nothing.
//
// The declare-then-define split (inherited from the non-generic two-pass lowering)
// is what makes recursion terminate: the placeholder is registered *before* the
// fields are lowered, so a recursive `shared Node<t>` field re-enters lowerType for
// the same instantiation, finds the placeholder, and takes a pointer to it. Without
// the split, laying out a recursive generic type would recurse forever.

// resolveInstantiation normalizes a `ParameterizedType` into the concrete named type
// its instantiation denotes: the declaration with this instantiation's arguments
// substituted in, renamed to the instantiation's mangled name — `Box<i64>` becomes
// `NamedStructType{Name: "Box$i64", Fields: [value: i64]}` — and ensures the matching
// LLVM type exists.
//
// This is the choke point that keeps generic types from touching the rest of the
// backend. Every site that reads an aggregate's shape (construction, field access,
// match, drop and retain glue, layout) switches on NamedStructType / TupleType /
// DataType, and a ParameterizedType matches none of them; adding a case to each would
// be a dozen places to keep in agreement about what `Box<i64>` means. Normalizing
// once — at the same accessor that strips newtypes, for the same reason — means those
// sites are unchanged and cannot disagree.
//
// The rename is load-bearing in both directions: the substituted *fields* are what
// construction and field access need, and the mangled *name* is what lowerType's
// lookupNamedType resolves to this instantiation's LLVM struct rather than to a bare
// declaration that was never registered.
func (l *lowerer) resolveInstantiation(t types.Type) (types.Type, error) {
	p, ok := t.(types.ParameterizedType)
	if !ok {
		return t, nil
	}
	// Materialize first: normalization promises the returned name resolves.
	if _, err := l.lowerParameterizedType(p); err != nil {
		return t, err
	}
	decl, ok := l.lookupTypeDecl(p.Name)
	if !ok {
		return t, fmt.Errorf("llvm: undefined generic type %q", p.Name)
	}
	subst := ast.BindGenericParams(decl.GenericParams, p.TypeArguments)
	name := l.instantiationSymbol(p)
	switch inst := substituteTypeVars(decl.Type, subst).(type) {
	case types.NamedStructType:
		inst.Name = name
		return types.WithAllocation(inst, p.Allocation), nil
	case types.TupleType:
		inst.Name = name
		return types.WithAllocation(inst, p.Allocation), nil
	case types.DataType:
		inst.Name = name
		return types.WithAllocation(inst, p.Allocation), nil
	case *types.ConstrainedType:
		// A **generic newtype**: `newtype Sorted<t> = []t`. The wrapper is nominal only —
		// a Sorted value *is* its base at run time — so the instantiation is the base's,
		// substituted, and there is nothing here to rename: the mangled name belongs to a
		// declaration with a layout of its own, and this one borrows the base's.
		//
		// `Boxed<i64>` over a scalar base worked without this arm only because a scalar
		// needs no drop glue, so nothing ever asked for the instantiation. The moment the
		// base is managed — `[]t`, a string — the glue asks, and the answer was
		// *"\"Sorted\" is not a generic type that can be instantiated"* on a program that
		// type-checked clean: rule 5 inverted, the front end accepting what the backend
		// cannot build.
		//
		// Stripped through StripNewtype rather than by reading `.Type` once, since a
		// newtype over a newtype is legal and every other representation decision in the
		// compiler goes through that same accessor.
		base := types.StripNewtype(inst)
		if pt, ok := base.(types.ParameterizedType); ok {
			// The base is itself generic (`newtype Wrapper<t> = Box<t>`), so it has an
			// instantiation of its own to resolve rather than a shape to hand back.
			return l.resolveInstantiation(types.WithAllocation(pt, p.Allocation))
		}
		return types.WithAllocation(base, p.Allocation), nil
	default:
		return t, fmt.Errorf("llvm: %q is not a generic type that can be instantiated (%T)", p.Name, decl.Type)
	}
}

// lowerParameterizedType returns the LLVM type for one instantiation of a generic
// type, materializing it on first use and caching it under its mangled name
// (`Box$i64`), so two uses of the same instantiation share one layout.
func (l *lowerer) lowerParameterizedType(p types.ParameterizedType) (lltypes.Type, error) {
	// A `shared`/dynamic-array flavor was already handled by lowerType before it got
	// here, so what remains is the by-value layout of this instantiation.
	name := l.instantiationSymbol(p)
	if st, ok := l.structTypes[name]; ok {
		return st, nil
	}
	if l.res.SymbolTable == nil {
		return nil, fmt.Errorf("llvm: cannot instantiate %s without a symbol table", p)
	}
	decl, ok := l.lookupTypeDecl(p.Name)
	if !ok {
		return nil, fmt.Errorf("llvm: undefined generic type %q", p.Name)
	}
	if len(decl.GenericParams) != len(p.TypeArguments) {
		return nil, fmt.Errorf("llvm: %s expects %d type argument(s), got %d",
			p.Name, len(decl.GenericParams), len(p.TypeArguments))
	}
	for i, arg := range p.TypeArguments {
		if arg == nil {
			return nil, fmt.Errorf("llvm: %s has an unresolved type argument in position %d", p.Name, i)
		}
	}

	// Pair the declaration's parameters positionally with this instantiation's
	// arguments. Positional because a type's parameters are ordered by declaration —
	// unlike a generic function's, which are solved by name from the argument types.
	subst := ast.BindGenericParams(decl.GenericParams, p.TypeArguments)

	// **A generic newtype is handled before anything is declared**, because it has no
	// layout of its own: `newtype Sorted<t> = []t` is its base at run time, so the type
	// wanted here is the base's. Declaring the placeholder first and dropping it later is
	// not equivalent — `declareNamedStruct` registers the name with the *module*, and
	// deleting the map entry leaves `%Sorted$i64 = type {}` in the emitted IR, which clang
	// rejects as a redefinition once the real one appears.
	if ct, ok := substituteTypeVars(decl.Type, subst).(*types.ConstrainedType); ok {
		restore := l.pushTypeSubst(subst)
		defer restore()
		return l.lowerType(types.WithAllocation(types.StripNewtype(ct), p.Allocation))
	}

	// Declare before defining: a recursive reference below must find this.
	if err := l.declareNamedStruct(name, name); err != nil {
		return nil, err
	}
	st := l.structTypes[name]

	// The declaration's field/element/payload types are written in terms of its own
	// parameters, so lowering them needs *this* instantiation's bindings installed —
	// and only those: we have entered the declaration's namespace, so an enclosing
	// function specialization's substitution no longer applies (the arguments were
	// already substituted through it on the way in).
	restore := l.pushTypeSubst(subst)
	defer restore()

	instantiated := substituteTypeVars(decl.Type, subst)
	var err error
	switch t := instantiated.(type) {
	case types.NamedStructType:
		err = l.lowerStructDefInto(st, t)
	case types.TupleType:
		err = l.lowerTupleDefInto(st, t)
	case types.DataType:
		err = l.lowerDataDefInto(st, t)
	case *types.ConstrainedType:
		// Unreachable: the early return above takes a generic newtype before the
		// placeholder is declared. Kept so this switch still names every shape a
		// declaration can have — an arm that says "handled elsewhere" is a claim, and a
		// silent default here would be the hazard-8 failure this whole change is one of.
		err = fmt.Errorf("llvm: generic newtype %q reached the layout switch; it should have "+
			"been resolved to its base before the placeholder was declared", p.Name)
		_ = t
	default:
		err = fmt.Errorf("llvm: %q is not a generic type that can be instantiated (%T)", p.Name, decl.Type)
	}
	if err != nil {
		// Drop the half-built placeholder so a retry can't observe a type with no
		// fields, which would lay out as an empty struct rather than failing again.
		delete(l.structTypes, name)
		return nil, err
	}
	return st, nil
}
