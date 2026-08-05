package llvm

import (
	"fmt"

	lltypes "github.com/llir/llvm/ir/types"

	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/types"
)

func (l *lowerer) lowerTypeDeclarations(program *ast.Program) error {
	for _, statement := range program.Statements {
		if typeDeclStmt, ok := statement.(*ast.TypeDeclStmt); ok {
			// A declaration's own name and its field types are resolved as its
			// declaring module sees them, not as the last-walked module did.
			restore := l.enterModuleOf(typeDeclStmt.GetLocation())
			err := l.lowerTypeDecl(typeDeclStmt)
			restore()
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func (l *lowerer) lowerTypeDecl(typeDeclStmt *ast.TypeDeclStmt) error {
	if typeDeclStmt.Name == "?" {
		return nil
	}
	if typeDeclStmt.IsAlias {
		// A transparent alias has no representation of its own — it *is* the type it
		// names, which has its own declaration (or, for a primitive or a structural
		// type, needs none). Skipping is not just an optimization: an alias holds the
		// aliased type itself, so `type Point = Pt` would otherwise declare and define
		// Pt's struct a second time under the name Point.
		return nil
	}
	if len(typeDeclStmt.GenericParams) > 0 {
		// A *generic* declaration has no single layout — `Box<t>` is a different
		// struct at every binding of `t`, so there is nothing to register under the
		// bare name. Its instantiations are materialized on demand instead
		// (lowerParameterizedType, generic_types.go), and an uninstantiated generic
		// type costs nothing, exactly as an uninstantiated generic function does.
		// Registering it eagerly is what produced "unknown type: t": the definition
		// pass tried to lower a field whose declared type is a type variable.
		return nil
	}
	switch t := typeDeclStmt.Type.(type) {
	case types.TupleType, types.NamedStructType, types.DataType:
		// Key by the declaration's name (typeDeclStmt.Name), not t.GetName():
		// TupleType.GetName() renders the full shape ("Point(i32, i32)"), while
		// the definition pass and lowerType both look the type up by its bare
		// declared name ("Point"). A `data` type registers the same way — its
		// definition is the tagged union `{ tag, payload-blob }`.
		return l.declareNamedStruct(l.typeKey(typeDeclStmt.Name), typeDeclStmt.Name)
	case *types.ConstrainedType:
		// A `newtype` declares no representation of its own — it is nominal to
		// the typechecker and *is* its base at run time (types.StripNewtype), so
		// there is nothing to register. Deliberately not an LLVM type alias: a
		// distinct named type for `newtype Meters = i64` would make every
		// arithmetic, comparison, and coercion site reconcile two llir types for
		// one machine value, for no gain — nominal identity has already done its
		// work by the time codegen runs.
		return nil
	default:
		return fmt.Errorf("llvm: unsupported type %s", t)
	}
}

// declareNamedStruct registers an empty, named LLVM struct type as a placeholder
// for a tuple/struct declaration. The definition pass (lowerTupleDef/
// lowerStructDef) fills in its fields; declaring every named type first lets a
// later type's fields reference an earlier one — and a type reference itself,
// once boxed layout lands.
func (l *lowerer) declareNamedStruct(key, name string) error {
	if name == "" {
		return fmt.Errorf("llvm: tuple or struct type must have a name")
	}
	st := lltypes.NewStruct() // empty placeholder
	// Registered under the *key* but named by the readable form: two modules' private
	// `Point`s are two entries and two LLVM types, while an ordinary program's IR is
	// byte-for-byte what it was.
	l.module.NewTypeDef(llvmTypeName(key, name), st) // sets TypeName — st now has identity
	l.structTypes[key] = st
	return nil
}

func (l *lowerer) lowerTypeDefinitions(program *ast.Program) error {
	for _, statement := range program.Statements {
		if typeDeclStmt, ok := statement.(*ast.TypeDeclStmt); ok {
			restore := l.enterModuleOf(typeDeclStmt.GetLocation())
			err := l.lowerTypeDef(typeDeclStmt)
			restore()
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func (l *lowerer) lowerTypeDef(typeDeclStmt *ast.TypeDeclStmt) error {
	if typeDeclStmt.Name == "?" {
		return nil
	}
	if typeDeclStmt.IsAlias {
		return nil // nothing to define — see lowerTypeDecl
	}
	if len(typeDeclStmt.GenericParams) > 0 {
		return nil // see lowerTypeDecl — instantiations are materialized on demand
	}
	switch t := typeDeclStmt.Type.(type) {
	case types.TupleType:
		return l.lowerTupleDef(t)
	case types.NamedStructType:
		return l.lowerStructDef(t)
	case types.DataType:
		return l.lowerDataDef(t)
	case *types.ConstrainedType:
		return nil // nothing to define — see lowerTypeDecl
	default:
		return fmt.Errorf("llvm: unsupported type %s", t)
	}
}

// lowerDataDef fills a `data` type's registered placeholder with its tagged-union
// layout `{ iTAG, [K x iA] }` (DATA_LAYOUT.md): a tag sized to the variant count,
// followed by a payload blob sized/aligned to the largest variant. DataUnionType
// (layout.go) computes the shape; an all-nullary `data` (an enum) is just
// `{ iTAG }` with no blob.
//
// A by-value reference to another named type in a payload (`data W = Wrap(P)`)
// arrives as an UnresolvedType, which SizeAndAlign can't size on its own, so the
// payload types are first resolved through the symbol table (resolveForLayout).
// DataUnionType still fails (ok=false) for a genuinely un-sizeable payload — a
// string field or an un-monomorphized generic — a loud error, not a wrong layout.
// (A recursive reference must be `shared`, i.e. a pointer per lyra-E014, which is
// pointer-sized and stops the resolution before it recurses.)
func (l *lowerer) lowerDataDef(t types.DataType) error {
	return l.lowerDataDefInto(l.structTypes[l.typeKey(t.Name)], t)
}

// lowerDataDefInto is lowerDataDef against an explicit target, so a generic
// instantiation can fill its *own* placeholder (generic_types.go) through the same
// definition path a plain declaration uses — one layout rule for both.
func (l *lowerer) lowerDataDefInto(st *lltypes.StructType, t types.DataType) error {
	if st == nil {
		return fmt.Errorf("llvm: no registered LLVM type for data type %q", t.Name)
	}
	resolved := l.resolveForLayout(t).(types.DataType)
	union, ok := DataUnionType(resolved)
	if !ok {
		return fmt.Errorf("llvm: cannot lay out data type %q yet (a variant payload isn't sizeable — e.g. a string field or an un-monomorphized generic)", t.Name)
	}
	st.Fields = append(st.Fields, union.Fields...)
	return nil
}

// resolveForLayout deep-resolves a type's UnresolvedType leaves against the
// symbol table so SizeAndAlign can size it — a named-type reference in a `data`
// payload or a struct field (`Wrap(P)`, `struct S { p: P }`) is stored as just a
// name. It short-circuits a `shared` reference (pointer-sized, so its referent is
// never chased) — which is also what keeps resolution finite: a recursive type's
// cycle must pass through a `shared` field (lyra-E014), so every by-value chain is
// acyclic and terminates.
func (l *lowerer) resolveForLayout(t types.Type) types.Type {
	switch v := t.(type) {
	case *types.ConstrainedType:
		// A newtype is laid out as its base (a `struct S { d: Meters }` field is
		// just an i64). Reached either directly or via the UnresolvedType branch
		// below, which is how a field naming one is recorded.
		return l.resolveForLayout(v.Type)
	case types.UnresolvedType:
		if v.Allocation == types.Shared {
			return t // a pointer; don't chase the referent (it may be recursive)
		}
		decl, ok := l.lookupTypeDecl(v.Name)
		if !ok {
			return t // unknown name; SizeAndAlign will fail loudly downstream
		}
		return l.resolveForLayout(types.WithAllocation(decl.Type, v.Allocation))
	case types.NamedStructType:
		fields := make([]types.StructField, len(v.Fields))
		for i, f := range v.Fields {
			f.Type = l.resolveForLayout(f.Type)
			fields[i] = f
		}
		v.Fields = fields
		return v
	case types.TupleType:
		elems := make([]types.Type, len(v.Elements))
		for i, e := range v.Elements {
			elems[i] = l.resolveForLayout(e)
		}
		v.Elements = elems
		return v
	case types.DataType:
		ctors := make([]types.DataTypeConstructor, len(v.Constructors))
		for i, c := range v.Constructors {
			params := make([]types.Type, len(c.Params))
			for j, p := range c.Params {
				params[j] = l.resolveForLayout(p)
			}
			ctors[i] = types.DataTypeConstructor{Name: c.Name, Params: params}
		}
		v.Constructors = ctors
		return v
	case types.StaticArrayType:
		v.ElementType = l.resolveForLayout(v.ElementType)
		return v
	case types.ParameterizedType:
		// A generic instantiation used *by value* inside another type — a field
		// (`struct Node { parent: Maybe<weak Node> }`), a payload, an element. It is
		// normalized through the same choke point every other shape-reading site uses
		// (resolveInstantiation, generic_types.go), then laid out as the concrete
		// aggregate it denotes; without this it reaches SizeAndAlign as a shape that
		// matches none of its cases and boxing fails with "cannot size a `shared T`
		// payload yet". A failure to instantiate returns the type untouched, so it
		// fails loudly downstream rather than silently laying out something else —
		// the same contract as the unknown-name arm above.
		if v.Allocation == types.Shared {
			return t // a pointer; don't chase the referent (it may be recursive)
		}
		inst, err := l.resolveInstantiation(v)
		if err != nil {
			return t
		}
		return l.resolveForLayout(inst)
	}
	return t
}

func (l *lowerer) lowerTupleDef(t types.TupleType) error {
	return l.lowerTupleDefInto(l.structTypes[l.typeKey(t.Name)], t)
}

func (l *lowerer) lowerTupleDefInto(st *lltypes.StructType, t types.TupleType) error {
	if st == nil {
		return fmt.Errorf("llvm: no registered LLVM type for tuple type %q", t.Name)
	}
	for _, element := range t.Elements {
		elementType, err := l.lowerType(element)
		if err != nil {
			return err
		}
		st.Fields = append(st.Fields, elementType)
	}
	return nil
}

func (l *lowerer) lowerStructDef(t types.NamedStructType) error {
	return l.lowerStructDefInto(l.structTypes[l.typeKey(t.Name)], t)
}

func (l *lowerer) lowerStructDefInto(st *lltypes.StructType, t types.NamedStructType) error {
	if st == nil {
		return fmt.Errorf("llvm: no registered LLVM type for struct type %q", t.Name)
	}
	for _, field := range t.Fields {
		fieldType, err := l.lowerType(field.Type)
		if err != nil {
			return err
		}
		st.Fields = append(st.Fields, fieldType)
	}
	return nil
}

// recordedType returns the Lyra type the typechecker recorded for expr, with any
// newtype wrapper stripped. Every lowering decision reads types through this one
// accessor rather than the TypeTable directly, because a newtype is nominal only:
// a `newtype Percent = u8` value *is* a u8 at run time, and a representation
// decision made against the wrapper (which LLVM type, is it managed, how does
// print format it) would fall through to the "unsupported type" arm or, worse,
// silently pick the i64 default.
func (l *lowerer) recordedType(expr ast.Expression) (types.Type, bool) {
	t, ok := l.res.TypeTable.Get(expr)
	if !ok {
		return t, false
	}
	// While a generic specialization is being lowered, a type read out of the shared
	// body still mentions the function's type variables — substitute them for this
	// instantiation's bindings (monomorphize.go).
	t = l.stripNewtype(l.applyTypeSubst(t))
	// A generic *type* normalizes to its instantiation's concrete named type, so the
	// sites reading this switch on an aggregate they recognize (generic_types.go). A
	// failure to instantiate is deliberately not reported here — recordedType has no
	// error channel, and the raw ParameterizedType falls through to whichever
	// lowering site needed it, which errors loudly there with its own context.
	if inst, err := l.resolveInstantiation(t); err == nil {
		t = inst
	}
	return t, true
}

// stripNewtype removes newtype wrappers from t, resolving a type written as a
// *name* through the symbol table — a struct field or array element declared as
// `Meters` is recorded as an UnresolvedType, so that lookup is the only way to
// find the newtype at all.
//
// It resolves a name only far enough to answer "is this a newtype?": anything
// else is returned untouched, since UnresolvedType is load-bearing downstream
// (lookupNamedType, namedStructFields, resolveDataType all key off the name).
// The depth cap is a backstop against a cyclic declaration (`newtype A = B`,
// `newtype B = A`), which the recursive-type check rejects before codegen.
func (l *lowerer) stripNewtype(t types.Type) types.Type {
	for range 64 {
		if ct, ok := t.(*types.ConstrainedType); ok {
			t = ct.Type
			continue
		}
		u, ok := t.(types.UnresolvedType)
		if !ok || l.res.SymbolTable == nil {
			return t
		}
		decl, ok := l.lookupTypeDecl(u.Name)
		if !ok {
			return t
		}
		ct, ok := decl.Type.(*types.ConstrainedType)
		if !ok {
			return t
		}
		t = types.WithAllocation(ct.Type, u.Allocation)
	}
	return t
}

func (l *lowerer) lowerType(lyraType types.Type) (lltypes.Type, error) {
	// A type variable has no representation of its own: inside a specialization it
	// stands for that instantiation's binding (monomorphize.go). Substituted first,
	// so every case below sees a concrete type.
	lyraType = l.applyTypeSubst(lyraType)
	// A newtype lowers as its base — it declares no LLVM type of its own, so a
	// parameter, return, field, or element written as `Percent` must resolve past
	// the name (which lookupNamedType would otherwise fail on) to the u8 it is.
	lyraType = l.stripNewtype(lyraType)
	// A dynamic array `[]T` is always a heap-boxed, ref-counted value regardless of
	// any flavor (it is variable-length), so it lowers to a `ptr` to its box before
	// the `shared`-stripping below — otherwise `shared []T` would double-box.
	if dyn, ok := lyraType.(types.DynamicArrayType); ok {
		elem, err := l.lowerType(dyn.ElementType)
		if err != nil {
			return nil, err
		}
		return lltypes.NewPointer(DynArrayBoxType(elem)), nil
	}
	// A `shared` value is a pointer to a ref-counted box `{ i64 rc, payload }`
	// (ALLOCATION.md) — regardless of what the payload is. Strip the flavor to get
	// the by-value payload type, lower that, and wrap it in the box pointer. This is
	// also how a `shared` struct/data field or a recursive `shared` reference
	// becomes pointer-sized (finite), matching SizeAndAlign.
	if types.AllocationOf(lyraType) == types.Shared {
		// Strip to Stack (an explicit flavor — WithAllocation treats Unspecified as
		// "no change", which would loop) to get the by-value payload type.
		payload, err := l.lowerType(types.WithAllocation(lyraType, types.Stack))
		if err != nil {
			return nil, err
		}
		return lltypes.NewPointer(SharedBoxType(payload)), nil
	}
	switch t := lyraType.(type) {
	case *types.ConstrainedType:
		// A newtype has no representation of its own — it lowers as its base
		// (`newtype Percent = u8` → i8). This is the annotation-side counterpart
		// of recordedType, which does the same for a type read off the TypeTable.
		return l.lowerType(t.Type)
	case types.StaticArrayType:
		// A fixed-size array `[N]T` lowers to the LLVM aggregate `[N x <T>]` — a
		// first-class value that round-trips through alloca/store/load like a tuple.
		elem, err := l.lowerType(t.ElementType)
		if err != nil {
			return nil, err
		}
		return lltypes.NewArray(uint64(t.Size), elem), nil
	case *types.LambdaType:
		// A function value is a boxed closure `{ i8* fn, i8* env }` (closures.go) —
		// one representation for every function type, which is what lets a
		// `(i64) -> i64` parameter accept a named function, a captureless lambda, and
		// a capturing closure without specializing the call site.
		return ClosureLLVMType(), nil
	case types.WeakType:
		// A weak reference is a non-owning pointer (pointer-sized), so it lowers to
		// an opaque `i8*` — enough to lay out a `weak` field and break a recursive
		// cycle. The non-owning runtime semantics (a weak count, upgrade-to-strong)
		// are deferred, and there is no way to construct a weak value yet, so the
		// concrete pointee representation is intentionally left unspecified.
		return lltypes.NewPointer(lltypes.I8), nil
	case types.PrimitiveType:
		irType, ok := LLVMPrimitive(t.Name)
		if !ok {
			return nil, fmt.Errorf("unknown primitive type: %s", t.Name)
		}
		return irType, nil
	case types.VoidType:
		// Only a function return type is void (the typechecker rejects a void
		// value elsewhere); a void function lowers to an LLVM `void` return.
		return lltypes.Void, nil
	case types.TupleType:
		// A named tuple resolves to the struct type registered in the declaration
		// pass (key by t.Name, not GetName() which renders the full shape — see
		// declareNamedStruct). An anonymous tuple (`(1, 2)`) has no declaration,
		// so build its structural struct type on the fly from its elements.
		if types.IsAnonymousTupleName(t.Name) {
			return l.lowerAnonymousTupleType(t)
		}
		return l.lookupNamedType(t.Name)
	case types.NamedStructType:
		return l.lookupNamedType(t.Name)
	case types.DataType:
		// A `data` value resolves to its registered tagged-union struct.
		return l.lookupNamedType(t.Name)
	case types.ParameterizedType:
		// One instantiation of a generic type, materialized on first use.
		return l.lowerParameterizedType(t)
	case types.GenericType:
		// A type variable that survived substitution has no representation. It means
		// a specialization was lowered without its bindings installed, which would
		// otherwise fall through to the default arm's opaque "unknown type: t".
		return nil, fmt.Errorf("llvm: type variable %q has no concrete type here — a generic must be instantiated before it is lowered", t.Name)
	case types.UnresolvedType:
		// A named type referenced from another type declaration's field/element
		// (`struct Line { a: Point }`) stays an UnresolvedType — the typechecker
		// doesn't rewrite it to the concrete tuple/struct. Both declaration
		// passes ran first, so the name resolves against structTypes.
		return l.lookupNamedType(t.Name)
	default:
		return nil, fmt.Errorf("unknown type: %s", lyraType)
	}
}

// lookupNamedType returns the LLVM struct type registered for a named tuple or
// struct. It must already exist: every top-level type decl is declared (via
// declareNamedStruct) before any definition is lowered, so a field/element
// referencing another named type always resolves.
func (l *lowerer) lookupNamedType(name string) (lltypes.Type, error) {
	st, ok := l.structTypes[l.typeKey(name)]
	if ok {
		return st, nil
	}
	// A transparent alias registers no LLVM type of its own (lowerTypeDecl skips it),
	// so lower what it names instead. An annotation keeps the alias's *name* — the
	// typechecker resolves types for its own use without rewriting the AST — so the
	// backend has to expand it here, at the one place a named type is resolved.
	//
	// Recursion handles a chain (`type A = B` with `type B = Pt`) because the alias's
	// own recorded type is an UnresolvedType that comes straight back through
	// lowerType. A *cycle* would not terminate, and is not guarded here on purpose:
	// the typechecker rejects one (resolveType's resolvingTypes guard), and `lyrac
	// build` runs the backend only on an error-free program, so a cycle cannot reach
	// this code without the front end having been bypassed.
	if decl, ok := l.lookupTypeDecl(name); ok && decl.IsAlias {
		return l.lowerType(decl.Type)
	}
	return nil, fmt.Errorf("llvm: unknown named type %q", name)
}

// lowerAnonymousTupleType builds the (unnamed) LLVM struct type for an anonymous
// tuple `(1, 2)` from its element types. Unlike a named tuple it has no
// declaration to register against, and LLVM struct types are structural, so a
// fresh NewStruct is the whole representation — two anonymous tuples of the same
// shape lower to equal (interchangeable) LLVM types.
func (l *lowerer) lowerAnonymousTupleType(t types.TupleType) (*lltypes.StructType, error) {
	fields := make([]lltypes.Type, len(t.Elements))
	for i, elem := range t.Elements {
		ft, err := l.lowerType(elem)
		if err != nil {
			return nil, err
		}
		fields[i] = ft
	}
	return lltypes.NewStruct(fields...), nil
}

// resolveShape normalizes a scrutinee (or sub-pattern) type to the concrete
// aggregate shape it denotes: the declaration's own type for an UnresolvedType
// naming one, and the substituted instantiation for a ParameterizedType (`Opt<i64>`
// → the DataType named `Opt$i64`). An already-concrete type — or one this cannot
// resolve — comes back untouched, so the three resolvers below stay total.
//
// This switch is deliberately the only one of its kind. Each of resolveDataType /
// resolveStructType / resolveTupleType used to carry its own copy, and only the data
// one ever grew the ParameterizedType arm: a data pattern *nested inside* an
// aggregate pattern (`(Just(v), _)`) had failed with "data pattern on non-data value
// of type Opt<i64>", because the top-level match path resolves its scrutinee before
// dispatching while the sub-pattern path reads the element type straight off the
// tuple, where it is still parameterized. Struct and tuple sub-patterns reach that
// same path and so had the identical latent failure with none of the fix — hazard 8,
// where the durable answer is to stop having more than one switch rather than to add
// the missing arm to each.
func (l *lowerer) resolveShape(t types.Type) types.Type {
	switch v := t.(type) {
	case types.UnresolvedType:
		if decl, ok := l.lookupTypeDecl(v.Name); ok {
			return decl.Type
		}
	case types.ParameterizedType:
		if inst, err := l.resolveInstantiation(v); err == nil {
			return inst
		}
	}
	return t
}

// resolveDataType resolves a scrutinee type to its DataType.
func (l *lowerer) resolveDataType(t types.Type) (types.DataType, bool) {
	dt, ok := l.resolveShape(t).(types.DataType)
	return dt, ok
}

// resolveStructType resolves a scrutinee type to its NamedStructType.
func (l *lowerer) resolveStructType(t types.Type) (types.NamedStructType, bool) {
	st, ok := l.resolveShape(t).(types.NamedStructType)
	return st, ok
}

// resolveTupleType resolves a scrutinee type to its TupleType (named or anonymous).
func (l *lowerer) resolveTupleType(t types.Type) (types.TupleType, bool) {
	tt, ok := l.resolveShape(t).(types.TupleType)
	return tt, ok
}
