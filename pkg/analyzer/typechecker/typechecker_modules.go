package typechecker

import (
	"github.com/Lyra-Language/lyra/pkg/ast"
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
func (tc *TypeChecker) moduleMemberType(m *ast.MemberExpr) (typ types.Type, handled bool) {
	id, ok := m.Object.(*ast.IdentifierExpr)
	if !ok {
		return nil, false
	}
	// A local binding shadows a namespace: a value named `math` in scope means
	// `math.x` is a field read, not a module reference.
	if _, shadowed := tc.scope.Lookup(id.Name); shadowed {
		return nil, false
	}
	if _, isParam := tc.paramTypes[id.Name]; isParam {
		return nil, false
	}
	imp, ok := tc.symTable.NamespaceImport(m.GetLocation().File, id.Name)
	if !ok {
		return nil, false
	}

	name := m.Property.Name
	// Membership is checked, not assumed. Names are program-wide unique today
	// (a cross-module duplicate is rejected), so a bare lookup would *find*
	// `other.thing` through `math.thing` — resolving a reference the source never
	// made, and silently.
	if tc.symTable.DeclaringModule(name) != imp.Path {
		tc.addError(m.GetLocation(), SeverityError,
			"module %q has no member %q", imp.Path, name)
		return nil, true
	}
	if fn, ok := tc.symTable.LookupFunction(name); ok {
		t := tc.lambdaSignature(fn)
		tc.typeTable.Set(m, t)
		return t, true
	}
	if decl, ok := tc.symTable.LookupType(name); ok {
		tc.typeTable.Set(m, decl.Type)
		return decl.Type, true
	}
	tc.addError(m.GetLocation(), SeverityError,
		"module %q has no member %q", imp.Path, name)
	return nil, true
}
