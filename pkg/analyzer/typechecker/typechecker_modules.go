package typechecker

import (
	"github.com/Lyra-Language/lyra/pkg/ast"
	diag "github.com/Lyra-Language/lyra/pkg/diagnostic"
	"github.com/Lyra-Language/lyra/pkg/types"
)

// A plain `import util.math` binds a *namespace*, so the module's contents are reached
// as `math.double(…)`. Syntactically that is indistinguishable from a field read on a
// value called `math`, and the two are told apart the only way they can be: by asking
// whether `math` is a namespace this file imported.
//
// The check has to run *before* the object is inferred as an expression. A namespace is
// not a value — inferring it would report `math` as an undefined identifier, and the
// real resolution would never be reached.
//
// Which module is asking comes from the node's own Location.File. That is why stamping
// the file onto every location earned its keep twice: it is what lets a lookup be
// resolved against the asking module without threading a module context through every
// pass that might make one.

// moduleMemberType resolves `alias.name` when alias names an imported module, returning
// the referenced declaration's type. handled is true when the object *was* a namespace,
// including when the member does not exist — in that case the error is already reported
// and the caller must not fall through to a field read, which would report a second,
// more confusing one.
//
// When the member is a function, its **declaration** comes back alongside the type. A
// signature alone is not enough for a call: a *generic* callee's type variables have to be
// solved from the call's arguments, and that solver (inferGenericCall) works from the
// declaration, not from a `*types.LambdaType` whose variables are still free. Handing back
// only the type is what made `maybe.map(m, f)` report "cannot assign Maybe<i64> to
// Maybe<t>" while the same function called unqualified checked fine.
func (tc *TypeChecker) moduleMemberType(m *ast.MemberExpr) (typ types.Type, fn *ast.LambdaExpr, handled bool) {
	id, ok := m.Object.(*ast.IdentifierExpr)
	if !ok {
		return nil, nil, false
	}
	// A local binding shadows a namespace: a value named `math` in scope means
	// `math.x` is a field read, not a module reference.
	if _, shadowed := tc.scope.Lookup(id.Name); shadowed {
		return nil, nil, false
	}
	if _, isParam := tc.paramTypes[id.Name]; isParam {
		return nil, nil, false
	}
	imp, ok := tc.symTable.NamespaceImport(m.GetLocation().File, id.Name)
	if !ok {
		return nil, nil, false
	}

	name := m.Property.Name
	// Membership is checked, not assumed: a bare lookup would *find* `other.thing`
	// through `math.thing` — resolving a reference the source never made, and silently.
	//
	// Asked of the module's own scope rather than of DeclaringModule, which is
	// last-writer-wins and so forgets a module ever declared a name another module (or
	// the prelude) also declares.
	if !tc.symTable.ModuleDeclares(imp.Path, name) {
		tc.addError(m.GetLocation(), SeverityError,
			"module %q has no member %q", imp.Path, name)
		return nil, nil, true
	}
	// A namespace reference is a cross-module reference by construction, so `pub` is
	// checked before the member is handed back.
	if !tc.checkVisible(tc.visibilityIn(imp.Path, name), m.GetLocation()) {
		return nil, nil, true
	}
	if fn, ok := tc.symTable.LookupFunctionIn(imp.Path, name); ok {
		t := tc.lambdaSignature(fn)
		tc.typeTable.Set(m, t)
		return t, fn, true
	}
	if decl, ok := tc.symTable.LookupTypeIn(imp.Path, name); ok {
		tc.typeTable.Set(m, decl.Type)
		return decl.Type, nil, true
	}
	tc.addError(m.GetLocation(), SeverityError,
		"module %q has no member %q", imp.Path, name)
	return nil, nil, true
}

// visibility is what the `pub` check needs from a declaration, so one check can serve
// types, traits and bindings without three near-identical copies.
type visibility struct {
	name     string
	module   string
	isPublic bool
	found    bool
}

// visibilityOf looks a bare top-level name up and reports where it was declared and
// whether it is exported.
func (tc *TypeChecker) visibilityOf(name string) visibility {
	return tc.visibilityIn(tc.symTable.DeclaringModule(name), name)
}

// visibilityIn is the same question asked of a **named** module, which is what a
// namespace reference (`shapes.Point`) needs: the member belongs to the module the
// import names, not to whichever module DeclaringModule happens to remember.
//
// The lookups are the `In` forms rather than LookupType/LookupTrait, and that is the
// whole point of them: a private declaration lives under a module-qualified key, so a
// bare read no longer finds it — and a visibility check that cannot see a private
// declaration reports every one of them as *visible*, which is the failure mode this
// function exists to prevent.
func (tc *TypeChecker) visibilityIn(module, name string) visibility {
	v := visibility{name: name, module: module}
	if decl, ok := tc.symTable.LookupTypeIn(module, name); ok {
		return visibility{name, module, decl.IsPublic, true}
	}
	if decl, ok := tc.symTable.LookupTraitIn(module, name); ok {
		return visibility{name, module, decl.IsPublic, true}
	}
	// A function's `pub` lives on its *binding*, not on the lambda, so it is read from
	// the declaring statement rather than from SymbolTable.Functions — and from the
	// binding *this module* made, for the reason the two lookups above are `In` forms.
	// BindingOf finds the module through a last-writer-wins map, so it answered `seq.map`
	// with the entry file's own `map` and called an exported function private.
	if decl, ok := tc.symTable.BindingIn(module, name); ok {
		return visibility{name, module, decl.IsPublic, true}
	}
	return v
}

// declVisibility reports the visibility of **the declaration a reference actually
// resolved to**, rather than of whichever declaration shares its name.
//
// That distinction is the whole point. visibilityOf finds a declaration by name, through
// DeclaringModule — a last-writer-wins map — so once two modules may each declare a
// private `Point`, it answers for whichever was collected last: module one's `impl Size
// for Point` reported *its own* type as "private to module two". This is the identical
// mistake the bare-call path made before privacy became structural (see
// pkg/modules/README.md), and the same fix — ask about the declaration in hand.
func (tc *TypeChecker) declVisibility(name string, decl ast.AstNode, isPublic bool) visibility {
	return visibility{
		name:     name,
		module:   tc.symTable.ModuleOfFile[decl.GetLocation().File],
		isPublic: isPublic,
		found:    true,
	}
}

// reportPrivateType turns a failed type lookup into "not yours" when some other module
// does declare the name, without exporting it. Reports true when it did.
//
// Privacy for a type is enforced **structurally**, the way it already is for a binding:
// a private declaration lives under a module-qualified key, so a reference from another
// module does not find it rather than finding it and being refused. That is the right
// mechanism and, on its own, the wrong message — "unknown type" reads as a typo for a
// name the author can see in the other file. This is the same not-found path lyra-E028
// survives on for a bare call.
func (tc *TypeChecker) reportPrivateType(name string, loc ast.Location) bool {
	from := tc.symTable.ModuleOfFile[loc.File]
	// A name the other module *exports* did not fail to resolve because it is private —
	// it failed because this file did not import it, which is a different fix and has
	// its own message. Before imports restricted visibility the two could not be told
	// apart here, since an exported type always resolved; now the commoner of the two
	// would otherwise be reported as the rarer, telling an author to add a `pub` that is
	// already there.
	if exporter, ok := tc.symTable.ExportingModule(name); ok && exporter != from {
		return false
	}
	for _, module := range tc.symTable.DeclaringModulesOf(name) {
		if module == from {
			continue
		}
		tc.addErrorCode(loc, SeverityError, diag.CodePrivateAccess,
			"%s is private to module %q — declare it `pub` there to export it", name, module)
		return true
	}
	return false
}

// checkVisible reports whether a reference at loc may see name, and reports an error
// when it may not.
//
// The rule is exactly the module boundary: within a module everything is visible, and
// `pub` is what crosses. Both halves matter — enforcing `pub` inside a module would
// make private helpers unusable by their own module, which is the opposite of the
// point.
//
// A reference from a file with no module (the entry file, which need not declare one)
// to a name that also has no module is same-module by definition, so nothing is
// enforced for a single-file program. That is what keeps this from changing the meaning
// of every existing one-file program.
func (tc *TypeChecker) checkVisible(v visibility, loc ast.Location) bool {
	if !v.found || v.isPublic {
		return true
	}
	from := tc.symTable.ModuleOfFile[loc.File]
	if from == v.module {
		return true
	}
	tc.addErrorCode(loc, SeverityError, diag.CodePrivateAccess,
		"%s is private to module %q — declare it `pub` there to export it", v.name, v.module)
	return false
}
