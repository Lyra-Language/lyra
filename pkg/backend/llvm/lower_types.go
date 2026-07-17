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
			if err := l.lowerTypeDecl(typeDeclStmt); err != nil {
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
	switch t := typeDeclStmt.Type.(type) {
	case types.TupleType, types.NamedStructType, types.DataType:
		// Key by the declaration's name (typeDeclStmt.Name), not t.GetName():
		// TupleType.GetName() renders the full shape ("Point(i32, i32)"), while
		// the definition pass and lowerType both look the type up by its bare
		// declared name ("Point"). A `data` type registers the same way — its
		// definition is the tagged union `{ tag, payload-blob }`.
		return l.declareNamedStruct(typeDeclStmt.Name)
	default:
		return fmt.Errorf("llvm: unsupported type %s", t)
	}
}

// declareNamedStruct registers an empty, named LLVM struct type as a placeholder
// for a tuple/struct declaration. The definition pass (lowerTupleDef/
// lowerStructDef) fills in its fields; declaring every named type first lets a
// later type's fields reference an earlier one — and a type reference itself,
// once boxed layout lands.
func (l *lowerer) declareNamedStruct(name string) error {
	if name == "" {
		return fmt.Errorf("llvm: tuple or struct type must have a name")
	}
	st := lltypes.NewStruct()     // empty placeholder
	l.module.NewTypeDef(name, st) // registers it, sets TypeName — st now has identity
	l.structTypes[name] = st
	return nil
}

func (l *lowerer) lowerTypeDefinitions(program *ast.Program) error {
	for _, statement := range program.Statements {
		if typeDeclStmt, ok := statement.(*ast.TypeDeclStmt); ok {
			if err := l.lowerTypeDef(typeDeclStmt); err != nil {
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
	switch t := typeDeclStmt.Type.(type) {
	case types.TupleType:
		return l.lowerTupleDef(t)
	case types.NamedStructType:
		return l.lowerStructDef(t)
	case types.DataType:
		return l.lowerDataDef(t)
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
	st := l.structTypes[t.Name]
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
	case types.UnresolvedType:
		if v.Allocation == types.Shared {
			return t // a pointer; don't chase the referent (it may be recursive)
		}
		decl, ok := l.res.SymbolTable.Types[v.Name]
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
	}
	return t
}

func (l *lowerer) lowerTupleDef(t types.TupleType) error {
	st := l.structTypes[t.Name]
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
	st := l.structTypes[t.Name]
	for _, field := range t.Fields {
		fieldType, err := l.lowerType(field.Type)
		if err != nil {
			return err
		}
		st.Fields = append(st.Fields, fieldType)
	}
	return nil
}

func (l *lowerer) lowerType(lyraType types.Type) (lltypes.Type, error) {
	switch t := lyraType.(type) {
	case types.PrimitiveType:
		irType, ok := LLVMPrimitive(t.Name)
		if !ok {
			return nil, fmt.Errorf("unknown primitive type: %s", t.Name)
		}
		return irType, nil
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
	st, ok := l.structTypes[name]
	if !ok {
		return nil, fmt.Errorf("llvm: unknown named type %q", name)
	}
	return st, nil
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

// resolveDataType resolves a scrutinee type to its DataType — directly, or via
// the symbol table for an UnresolvedType naming a non-generic data type. A
// generic (ParameterizedType) data type is not handled (monomorphization TODO).
func (l *lowerer) resolveDataType(t types.Type) (types.DataType, bool) {
	switch v := t.(type) {
	case types.DataType:
		return v, true
	case types.UnresolvedType:
		if decl, ok := l.res.SymbolTable.Types[v.Name]; ok {
			if dt, ok := decl.Type.(types.DataType); ok {
				return dt, true
			}
		}
	}
	return types.DataType{}, false
}

// resolveStructType resolves a scrutinee type to its NamedStructType — directly,
// or via the symbol table for an UnresolvedType naming a struct.
func (l *lowerer) resolveStructType(t types.Type) (types.NamedStructType, bool) {
	switch v := t.(type) {
	case types.NamedStructType:
		return v, true
	case types.UnresolvedType:
		if decl, ok := l.res.SymbolTable.Types[v.Name]; ok {
			if st, ok := decl.Type.(types.NamedStructType); ok {
				return st, true
			}
		}
	}
	return types.NamedStructType{}, false
}

// resolveTupleType resolves a scrutinee type to its TupleType — directly (named
// or anonymous), or via the symbol table for an UnresolvedType naming one.
func (l *lowerer) resolveTupleType(t types.Type) (types.TupleType, bool) {
	switch v := t.(type) {
	case types.TupleType:
		return v, true
	case types.UnresolvedType:
		if decl, ok := l.res.SymbolTable.Types[v.Name]; ok {
			if tt, ok := decl.Type.(types.TupleType); ok {
				return tt, true
			}
		}
	}
	return types.TupleType{}, false
}
