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
	if !tc.checkVisible(tc.visibilityOf(name), m.GetLocation()) {
		return nil, nil, true
	}
	if fn, ok := tc.symTable.LookupFunctionIn(imp.Path, name); ok {
		t := tc.lambdaSignature(fn)
		tc.typeTable.Set(m, t)
		return t, fn, true
	}
	if decl, ok := tc.symTable.LookupType(name); ok {
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

// visibilityOf looks a top-level name up and reports where it was declared and whether
// it is exported.
func (tc *TypeChecker) visibilityOf(name string) visibility {
	v := visibility{name: name, module: tc.symTable.DeclaringModule(name)}
	if decl, ok := tc.symTable.LookupType(name); ok {
		return visibility{name, v.module, decl.IsPublic, true}
	}
	if decl, ok := tc.symTable.LookupTrait(name); ok {
		return visibility{name, v.module, decl.IsPublic, true}
	}
	// A function's `pub` lives on its *binding*, not on the lambda, so it is read from
	// the declaring statement rather than from SymbolTable.Functions.
	if decl, ok := tc.symTable.BindingOf(name); ok {
		return visibility{name, v.module, decl.IsPublic, true}
	}
	return v
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
