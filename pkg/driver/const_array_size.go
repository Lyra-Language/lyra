package driver

import (
	"reflect"

	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/ast/symbols"
	"github.com/Lyra-Language/lyra/pkg/types"
)

// foldConstArraySizes rewrites every array type sized by a named `const` into the number
// it names: `[MAX_BYTES]u8` beside `const MAX_BYTES: i64 = 8` becomes `[8]u8`, in a
// parameter, a return type, a local's annotation, a struct field, a payload — wherever a
// type is written.
//
// **A desugaring, as a constructor value is** (constructor_value.go): the collector writes
// `[NAME]t` as a size variable, the shape a `const` generic parameter has, and until this
// pass every such name that was not one was lyra-E031. Settled here, before every pass, no
// pass downstream learns that a size may be named: the typechecker, layout and the backend
// see `[8]u8` as if it were written so.
//
// **What folds**: a top-level `const` the declaring module reaches — its own, an import,
// the prelude's — whose value is a non-negative integer literal, or another such `const`.
// Anything else (an arithmetic initializer, a float) is left as written and reported by
// lyra-E031, which says why (checkSizeVars). A `const` generic parameter of an enclosing
// binding shadows a module `const` of its name, as any binding shadows the module's.
//
// **Every type the program holds is reached by reflection**, as pkg/printer reads the AST:
// a type is written in a dozen kinds of node and nested in a dozen kinds of type (a struct's
// fields, a payload, a callback's parameter), and a hand-written list of positions is the
// switch rule 8 warns about — one missed is a `[MAX]u8` refused in that position alone.
//
// **The same walk keys a generic type written through a namespace** (10/29):
// `collections.HashMap<string, i64>` becomes `HashMap<string, i64>` carrying the key of
// std.collections' declaration. The dotted name resolved in the typechecker, but the
// backend reads a written annotation as written and mangled `collections.HashMap$…` beside
// `HashMap$…`: one type, two LLVM types, and a store between them refused. A plain
// `shapes.Pt` keeps its dotted name, which every lookup resolves (symbols/qualified.go).
func foldConstArraySizes(program *ast.Program, st *symbols.SymbolTable) {
	if st == nil {
		return
	}
	f := &sizeFolder{st: st, visited: map[visitKey]bool{}}
	for _, node := range program.Statements {
		f.loc = node.GetLocation()
		f.walk(reflect.ValueOf(&node).Elem(), nil)
	}
	// What the symbol table holds outside the tree: an extern's function (Func) carries
	// copies of its signature's types. Everything else there is a node already walked.
	for _, fn := range st.Functions {
		if fn != nil {
			f.loc = fn.GetLocation()
			f.walk(reflect.ValueOf(fn), nil)
		}
	}
}

// constSize is the array size a `const` named name means at loc: its value when that is a
// non-negative integer literal, or names another `const` that is. ok=false for anything
// else — no `const` of that name reachable from loc's module, or one that is not such a
// number — which leaves the size as written for lyra-E031 to report.
func constSize(st *symbols.SymbolTable, name string, loc ast.Location) (int, bool) {
	for depth := 0; depth < 16; depth++ { // a chain of consts naming consts, not a cycle
		decl, ok := reachableConst(st, name, loc)
		if !ok {
			return 0, false
		}
		if decl.Type != nil && !isIntegerType(decl.Type) {
			return 0, false
		}
		switch v := decl.Value.(type) {
		case *ast.IntegerLiteralExpr:
			n, ok := v.Int64()
			if !ok || n < 0 || n > int64(^uint32(0)>>1) {
				return 0, false
			}
			return int(n), true
		case *ast.IdentifierExpr:
			name, loc = v.Name, v.GetLocation()
			continue
		}
		return 0, false
	}
	return 0, false
}

// reachableConst is the top-level `const` named name that loc's module reaches: its own,
// then what it imports, then the prelude's — the module scope's chain, stopping short of
// the global scope, which holds every module's exports whether this one imports them or
// not.
func reachableConst(st *symbols.SymbolTable, name string, loc ast.Location) (*ast.VarDeclStmt, bool) {
	module := st.ModuleOfFile[loc.File]
	for _, scope := range []*symbols.Scope{st.ModuleScopes[module], st.ImportScopes[module], st.PreludeScope} {
		if scope == nil {
			continue
		}
		sym, found := scope.LookupLocal(name)
		if !found {
			continue
		}
		decl, ok := sym.(*ast.VarDeclStmt)
		if !ok || !decl.IsConstant() {
			return nil, false
		}
		return decl, true
	}
	return nil, false
}

func isIntegerType(t types.Type) bool {
	p, ok := t.(types.PrimitiveType)
	if !ok {
		return false
	}
	switch p.Name {
	case types.Int8, types.Int16, types.Int32, types.Int64, types.Int128,
		types.UInt8, types.UInt16, types.UInt32, types.UInt64, types.UInt128,
		types.UntypedInt, types.UntypedSignedInt:
		return true
	}
	return false
}

type sizeFolder struct {
	st *symbols.SymbolTable
	// loc is the top-level declaration being walked: its file names the module whose
	// consts a size may be.
	loc     ast.Location
	visited map[visitKey]bool
}

// visitKey is a pointer already walked: the AST shares nodes (a binding's Shadows, an
// overload set) and the walk must neither loop nor fold one twice.
type visitKey struct {
	ptr uintptr
	typ reflect.Type
}

var (
	staticArrayType   = reflect.TypeOf(types.StaticArrayType{})
	parameterizedType = reflect.TypeOf(types.ParameterizedType{})
	varDeclType       = reflect.TypeOf(ast.VarDeclStmt{})
	lambdaExprType    = reflect.TypeOf(ast.LambdaExpr{})
)

// walk folds every named size inside v, which must be addressable (or an interface or
// pointer leading to something that is), and reports whether it changed anything. shadowed
// holds the `const` generic parameters of the bindings enclosing v.
func (f *sizeFolder) walk(v reflect.Value, shadowed map[string]bool) bool {
	switch v.Kind() {
	case reflect.Interface:
		if v.IsNil() {
			return false
		}
		elem := v.Elem()
		if elem.Kind() == reflect.Ptr {
			return f.walk(elem, shadowed)
		}
		// A value inside an interface cannot be changed where it sits: change a copy and
		// put the copy back.
		cp := reflect.New(elem.Type()).Elem()
		cp.Set(elem)
		if !f.walk(cp, shadowed) {
			return false
		}
		if v.CanSet() {
			v.Set(cp)
		}
		return true
	case reflect.Ptr:
		if v.IsNil() {
			return false
		}
		key := visitKey{v.Pointer(), v.Type()}
		if f.visited[key] {
			return false
		}
		f.visited[key] = true
		return f.walk(v.Elem(), shadowed)
	case reflect.Slice, reflect.Array:
		changed := false
		for i := 0; i < v.Len(); i++ {
			if f.walk(v.Index(i), shadowed) {
				changed = true
			}
		}
		return changed
	case reflect.Map:
		changed := false
		iter := v.MapRange()
		for iter.Next() {
			val := iter.Value()
			cp := reflect.New(val.Type()).Elem()
			cp.Set(val)
			if f.walk(cp, shadowed) {
				v.SetMapIndex(iter.Key(), cp)
				changed = true
			}
		}
		return changed
	case reflect.Struct:
		return f.walkStruct(v, shadowed)
	}
	return false
}

func (f *sizeFolder) walkStruct(v reflect.Value, shadowed map[string]bool) bool {
	t := v.Type()
	switch t {
	case parameterizedType:
		if !v.CanAddr() {
			return false
		}
		p := v.Addr().Interface().(*types.ParameterizedType)
		changed := false
		if p.Key == "" {
			if key, member, ok := f.st.QualifiedTypeKey(f.st.ModuleOfFile[f.loc.File], p.Name); ok {
				p.Name, p.Key = member, key
				changed = true
			}
		}
		if f.walk(v.FieldByName("TypeArguments"), shadowed) {
			changed = true
		}
		return changed
	case staticArrayType:
		changed := false
		if !v.CanAddr() {
			return false
		}
		arr := v.Addr().Interface().(*types.StaticArrayType)
		if arr.SizeVar != "" && !shadowed[arr.SizeVar] {
			if size, ok := constSize(f.st, arr.SizeVar, f.loc); ok {
				arr.Size, arr.SizeVar = size, ""
				changed = true
			}
		}
		if f.walk(v.FieldByName("ElementType"), shadowed) {
			changed = true
		}
		return changed
	case varDeclType, lambdaExprType:
		// A binding's `<const N: i64>` is N throughout it, whatever the module declares.
		params := v.FieldByName("GenericParams").Interface().([]ast.GenericParam)
		for _, p := range params {
			if !p.IsConst {
				continue
			}
			inner := make(map[string]bool, len(shadowed)+len(params))
			for name := range shadowed {
				inner[name] = true
			}
			for _, q := range params {
				if q.IsConst {
					inner[q.Name] = true
				}
			}
			shadowed = inner
			break
		}
	}
	changed := false
	for i := 0; i < t.NumField(); i++ {
		if !t.Field(i).IsExported() {
			continue
		}
		if f.walk(v.Field(i), shadowed) {
			changed = true
		}
	}
	return changed
}
