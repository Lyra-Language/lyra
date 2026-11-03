package driver

import (
	"maps"
	"reflect"
	"slices"

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
	arraySizeType     = reflect.TypeOf(types.ArraySize{})
	typeDeclType      = reflect.TypeOf(ast.TypeDeclStmt{})
	traitImplStmtType = reflect.TypeOf(ast.TraitImplStmt{})
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
		if f.sizeArguments(p) {
			changed = true
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
		if size, sizeVar, ok := f.foldSize(arr.SizeVar, shadowed); ok {
			arr.Size, arr.SizeVar = size, sizeVar
			changed = true
		}
		if f.walk(v.FieldByName("ElementType"), shadowed) {
			changed = true
		}
		return changed
	case arraySizeType:
		if !v.CanAddr() {
			return false
		}
		arg := v.Addr().Interface().(*types.ArraySize)
		size, sizeVar, ok := f.foldSize(arg.Var, shadowed)
		if ok {
			arg.Size, arg.Var = size, sizeVar
		}
		return ok
	case typeDeclType:
		// A type's `<const N: i64>` is N throughout it, as a binding's is below.
		shadowed = withConstParams(shadowed, v.FieldByName("GenericParams").Interface().([]ast.GenericParam))
	case varDeclType, lambdaExprType:
		// A binding's `<const N: i64>` is N throughout it, whatever the module declares.
		shadowed = withConstParams(shadowed, v.FieldByName("GenericParams").Interface().([]ast.GenericParam))
		if t == lambdaExprType && v.CanAddr() {
			lam := v.Addr().Interface().(*ast.LambdaExpr)
			lam.ImplConstParams = f.implConstParams(lam.ImplTarget)
			shadowed = withConstParams(shadowed, lam.ImplConstParams)
		}
	case traitImplStmtType:
		if v.CanAddr() {
			impl := v.Addr().Interface().(*ast.TraitImplStmt)
			impl.ConstParams = f.implConstParams(impl.Type)
			shadowed = withConstParams(shadowed, impl.ConstParams)
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

// withConstParams answers shadowed with params' `const` parameters added.
func withConstParams(shadowed map[string]bool, params []ast.GenericParam) map[string]bool {
	if !slices.ContainsFunc(params, func(p ast.GenericParam) bool { return p.IsConst }) {
		return shadowed
	}
	inner := make(map[string]bool, len(shadowed)+len(params))
	maps.Copy(inner, shadowed)
	for _, p := range params {
		if p.IsConst {
			inner[p.Name] = true
		}
	}
	return inner
}

// foldSize replaces the module `const`s a size mentions. It answers false when it mentions none
// it can, or would come out negative, which leaves it as written for lyra-E031 to report.
func (f *sizeFolder) foldSize(size string, shadowed map[string]bool) (int, string, bool) {
	bindings := map[string]types.Type{}
	for _, name := range types.SizeVarNames(size) {
		if shadowed[name] {
			continue
		}
		if n, ok := constSize(f.st, name, f.loc); ok {
			bindings[name] = types.ArraySize{Size: n}
		}
	}
	if len(bindings) == 0 {
		return 0, "", false
	}
	n, sizeVar := types.SubstituteSize(size, bindings)
	if sizeVar == "" && n < 0 {
		return 0, "", false
	}
	return n, sizeVar, true
}

// sizeArguments settles a type's arguments in the positions its `const` parameters hold: a bare
// `Ring<t, N>` parsed `N` as a type's name, since the lexer can't tell them apart (the grammar's
// size_argument), and here the declaration says what it is. The folding of a `const` follows
// from the walk over the arguments. A generic type resolved from here is the declaration every
// module's reference to it means; one that isn't found has nothing to say about its arguments.
func (f *sizeFolder) sizeArguments(p *types.ParameterizedType) bool {
	decl, found := f.st.LookupTypeRef(p.Name, p.Key, f.loc)
	if !found || decl == nil {
		return false
	}
	changed := false
	for i, param := range decl.GenericParams {
		if !param.IsConst || i >= len(p.TypeArguments) {
			continue
		}
		if name, ok := p.TypeArguments[i].(types.UnresolvedType); ok {
			if _, isSize := types.ParseSize(name.Name); isSize {
				p.TypeArguments[i] = types.ArraySize{Var: name.Name}
				changed = true
			}
		}
	}
	return changed
}

// implConstParams answers the `const` parameters an impl's target introduces: `N` in `impl
// Ring<t, N>`, as a lowercase `t` there is a variable by being written. **Unless the module
// itself declares or imports a `const` of that name**, which it means: `impl Ring<t, MAX>` is
// for that one size. The prelude's names don't count, since a name a reader can't see in the
// file must not turn a block for every size into a block for one.
func (f *sizeFolder) implConstParams(target types.Type) []ast.GenericParam {
	p, ok := target.(types.ParameterizedType)
	if !ok {
		return nil
	}
	decl, found := f.st.LookupTypeRef(p.Name, p.Key, f.loc)
	if !found || decl == nil {
		return nil
	}
	var params []ast.GenericParam
	for i, param := range decl.GenericParams {
		if !param.IsConst || i >= len(p.TypeArguments) {
			continue
		}
		var size string
		switch arg := p.TypeArguments[i].(type) {
		case types.UnresolvedType:
			size = arg.Name
		case types.ArraySize:
			size = arg.Var
		}
		for _, name := range types.SizeVarNames(size) {
			if f.ownConst(name) || slices.ContainsFunc(params, func(g ast.GenericParam) bool { return g.Name == name }) {
				continue
			}
			params = append(params, ast.GenericParam{Name: name, IsConst: true, ValueType: param.ValueType, Location: f.loc})
		}
	}
	return params
}

// ownConst reports whether the module at f.loc declares or imports a name, as a `const` or not.
func (f *sizeFolder) ownConst(name string) bool {
	module := f.st.ModuleOfFile[f.loc.File]
	for _, scope := range []*symbols.Scope{f.st.ModuleScopes[module], f.st.ImportScopes[module]} {
		if scope == nil {
			continue
		}
		if _, found := scope.LookupLocal(name); found {
			return true
		}
	}
	return false
}
