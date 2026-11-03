package typechecker

import (
	"fmt"
	"slices"
	"strings"

	"github.com/Lyra-Language/lyra/pkg/ast"
	diag "github.com/Lyra-Language/lyra/pkg/diagnostic"
	"github.com/Lyra-Language/lyra/pkg/types"
)

// `Type.name(…)` — an associated function: a member of `impl Type { … }` with no `self`, or
// a trait method with no receiver that Type implements.
//
// **A call is rewritten to the direct call it is**, as a method call is (desugarUFCSCall), so
// generics, purity, ownership and the backend meet an ordinary function. Its name holds the
// type's (ast.AssociatedName), and resolution starts from the type rather than from
// that name, because the type is what the call means: `alias R = Rng` makes `R.seeded` and
// `Rng.seeded` one function, and `a.Rng` and `b.Rng` are two.

// typeNamedBy returns the type a member's object spells, if it spells one, and that spelling.
// A PascalCase name always collects as a constructor; an all-caps one or a builtin's collects
// as a name, which a value in scope takes first.
func (tc *TypeChecker) typeNamedBy(member *ast.MemberExpr) (types.Type, string, bool) {
	var name string
	switch obj := member.Object.(type) {
	case *ast.DataConstructorExpr:
		name = obj.Constructor
	case *ast.IdentifierExpr:
		// A type is in scope as itself, and only a value shadows the reading as a type.
		if sym, inScope := tc.scope.Lookup(obj.Name); inScope {
			if _, isType := sym.(*ast.TypeDeclStmt); !isType {
				return nil, "", false
			}
		}
		if _, isParam := tc.paramTypes[obj.Name]; isParam {
			return nil, "", false
		}
		if p, ok := builtinTypeNamed(obj.Name); ok {
			return p, obj.Name, true
		}
		name = obj.Name
	default:
		return nil, "", false
	}
	// Keyed, because `a.Dice` means a's at this node only and the type goes on from here.
	loc := member.Object.GetLocation()
	if _, ok := tc.symTable.LookupTypeFrom(name, loc); !ok {
		return nil, "", false
	}
	return types.UnresolvedType{Name: name, Key: tc.symTable.TypeKey(name, loc)}, name, true
}

// builtinTypeNamed reads a builtin type's name in expression position, which is an ordinary
// identifier there: `string.from_bytes(b)`.
func builtinTypeNamed(name string) (types.Type, bool) {
	if p, ok := numericPrimitiveByName(name); ok {
		return p, true
	}
	if t, ok := identityConversionTargetByName(name); ok {
		return t, true
	}
	return nil, false
}

// associatedFunctionsOf is every associated function named name that belongs to t and that
// the file at loc could call, and whether any exists that it could not.
func (tc *TypeChecker) associatedFunctionsOf(t types.Type, name string, loc ast.Location) (reachable []*ast.LambdaExpr, private *ast.LambdaExpr) {
	head, want, ok := tc.typeIdentity(t, loc)
	if !ok {
		return nil, nil
	}
	for _, fn := range tc.symTable.AssociatedFunctionsNamed(name) {
		h, decl, ok := tc.typeIdentity(fn.AssociatedTo, fn.GetLocation())
		if !ok || h != head || decl != want {
			continue
		}
		key, _ := ast.AssociatedName(fn.AssociatedTo, name)
		if ufcsImportedIn(tc.symTable, key, fn, loc) {
			reachable = append(reachable, fn)
		} else {
			private = fn
		}
	}
	return reachable, private
}

// typeIdentity is what makes two spellings of a type one type: its head with aliases expanded,
// and the declaration that head names from loc (nil for a builtin, whose name is enough).
func (tc *TypeChecker) typeIdentity(t types.Type, loc ast.Location) (string, *ast.TypeDeclStmt, bool) {
	expanded := expandAliases(t, tc.symTable, loc)
	head, ok := types.HeadName(expanded)
	if !ok {
		return "", nil, false
	}
	var decl *ast.TypeDeclStmt
	switch u := expanded.(type) {
	case types.UnresolvedType:
		decl, _ = tc.symTable.LookupTypeRef(u.Name, u.Key, loc)
	case types.ParameterizedType:
		decl, _ = tc.symTable.LookupTypeRef(u.Name, u.Key, loc)
	case types.PrimitiveType:
	default:
		decl, _ = tc.symTable.LookupTypeFrom(head, loc) // a type already resolved
	}
	return head, decl, true
}

// associatedFunctionFor picks the one function a `Type.name` means, reporting anything else.
// The second result is false when nothing was reported and nothing was found.
func (tc *TypeChecker) associatedFunctionFor(t types.Type, written string, member *ast.MemberExpr) (*ast.LambdaExpr, bool) {
	name := member.Property.Name
	loc := member.GetLocation()
	reachable, private := tc.associatedFunctionsOf(t, name, loc)
	switch len(reachable) {
	case 0:
		if private != nil {
			tc.addErrorCode(loc, SeverityError, diag.CodeTypeNameAsValue,
				"%s.%s is private to module %q; mark it `pub` there to call it from here",
				written, name, tc.symTable.ModuleOfFile[private.GetLocation().File])
			return nil, true
		}
		return nil, false
	case 1:
		return reachable[0], true
	}
	// Two in the asking module are one type declared under two names, through an alias.
	if local := tc.localCandidate(reachable, loc); local != nil {
		return local, true
	}
	places := make([]string, len(reachable))
	for i, fn := range reachable {
		at := fn.GetLocation()
		places[i] = at.File + ":" + at.Pretty()
	}
	tc.addErrorCode(loc, SeverityError, diag.CodeTypeNameAsValue,
		"%s.%s is ambiguous: it is declared at %s", written, name, strings.Join(places, " and "))
	return nil, true
}

// callThroughTypeName runs the type-name rung of a member call. handled is false when the
// object names no type, or names one that is also the value a nullary constructor builds and
// that declares nothing of this name: `data Unit = Unit`, `Unit.show()` is a method call.
func (tc *TypeChecker) callThroughTypeName(member *ast.MemberExpr, call *ast.FunctionCallExpr) (ret types.Type, handled bool) {
	t, written, ok := tc.typeNamedBy(member)
	if !ok {
		return nil, false
	}
	name := member.Property.Name
	fn, handled := tc.associatedFunctionFor(t, written, member)
	if fn != nil {
		key, _ := ast.AssociatedName(fn.AssociatedTo, name)
		callee := &ast.IdentifierExpr{Name: key, FromMethodCall: true}
		callee.Location = member.Property.GetLocation()
		if sig := tc.lambdaSignature(fn); sig != nil {
			tc.typeTable.Set(callee, sig)
		}
		tc.noteTypeNameUse(member)
		call.Function = callee
		tc.noteUFCSModule(fn, member.GetLocation(), member.Property.GetLocation())
		tc.noteResolvedCallee(call, fn)
		tc.requireUnsafeCall(key, fn, call)
		return tc.inferLambdaCall(key, fn, call), true
	}
	if handled {
		return nil, true
	}
	if ret, handled := tc.callTraitFunctionThroughType(t, written, member, call); handled {
		return ret, true
	}
	// A value the name also builds keeps its method calls.
	if dc, isCtor := member.Object.(*ast.DataConstructorExpr); isCtor {
		if _, isValue := tc.findDataTypeByConstructor(dc.Constructor, dc.GetLocation()); isValue {
			return nil, false
		}
	}
	tc.addErrorCode(member.GetLocation(), SeverityError, diag.CodeTypeNameAsValue,
		"%s has no associated function %q%s", written, name, tc.associatedHint(t, name, member.GetLocation()))
	return nil, true
}

// noteTypeNameUse keeps `import lib.{ Dice }` from reading as unused: the rewrite is about to
// take away the only mention of the name that lyra-W004 could see.
func (tc *TypeChecker) noteTypeNameUse(member *ast.MemberExpr) {
	obj, ok := member.Object.(*ast.DataConstructorExpr)
	if !ok {
		return
	}
	decl, ok := tc.symTable.LookupTypeFrom(obj.Constructor, obj.GetLocation())
	if !ok {
		return
	}
	file := obj.GetLocation().File
	if tc.aliasedTypeUses[file] == nil {
		tc.aliasedTypeUses[file] = map[string]bool{}
	}
	tc.aliasedTypeUses[file][decl.Name] = true
}

// callTraitFunctionThroughType is `Vec2.zero()`: a trait method with no receiver, at the impl
// the type names. It says outright what `Zero::zero()` leaves the context to say. A method
// with a receiver is not reached this way, since `v.area()` already reads better than
// `Shape.area(v)` could. A generic type names its head and no one impl, so the arguments still
// come from the context, which must be that type: `let b: Box<i64> = Box.zero()`.
func (tc *TypeChecker) callTraitFunctionThroughType(t types.Type, written string, member *ast.MemberExpr, call *ast.FunctionCallExpr) (types.Type, bool) {
	name := member.Property.Name
	loc := call.GetLocation()
	self := tc.resolveType(t, loc)
	var functions []resolvedTraitMethod
	for _, m := range tc.resolveTraitMethod(self, name, "") {
		if m.Signature != nil && !receiverFirst(m.Signature) {
			functions = append(functions, m)
		}
	}
	if len(functions) > 1 {
		functions = tc.matchesAcceptingArguments(functions, call)
	}
	switch len(functions) {
	case 0:
		return nil, false
	case 1:
	default:
		tc.reportAmbiguousTraitCall(name, functions, loc)
		return nil, true
	}
	tc.noteTypeNameUse(member)
	trait := functions[0].Impl.TraitName
	path := &ast.TraitMethodPathExpr{TraitName: trait, Method: ast.IdentifierExpr{Name: name}}
	path.Location = member.GetLocation()
	path.Method.Location = member.Property.GetLocation()
	call.Function = path
	if decl, ok := tc.typeDeclOf(t, loc); ok && len(decl.GenericParams) > 0 {
		return tc.callGenericTraitFunction(t, written, path, call), true
	}
	return tc.inferResolvedTraitMethodCall(trait+"::"+name, functions[0], call, nil), true
}

// callGenericTraitFunction is `Box.zero()` where `impl Zero for Box<t>`: the call
// `Zero::zero()` is, once the context is known to be the type the call wrote.
func (tc *TypeChecker) callGenericTraitFunction(t types.Type, written string, path *ast.TraitMethodPathExpr, call *ast.FunctionCallExpr) types.Type {
	loc := call.GetLocation()
	head, decl, _ := tc.typeIdentity(t, loc)
	want := tc.currentExpectedType()
	if want != nil {
		wantHead, wantDecl, ok := tc.typeIdentity(want, loc)
		if !ok || wantHead != head || wantDecl != decl {
			want = nil
		}
	}
	trait, ok := tc.symTable.LookupTraitFrom(path.TraitName, loc)
	if !ok || want == nil {
		tc.addErrorCode(loc, SeverityError, diag.CodeTypeNameAsValue,
			"%s is generic, so %s.%s takes its arguments from what the result is used as, and nothing here says; give the result a type, as in `let x: %s<…> = %s.%s(…)`",
			written, written, path.Method.Name, written, written, path.Method.Name)
		return nil
	}
	return tc.inferReturnDirectedTraitCall(trait, findTraitMethod(trait, path.Method.Name), path, call)
}

// typeDeclOf is the declaration t names from loc, aliases expanded. A builtin has none.
func (tc *TypeChecker) typeDeclOf(t types.Type, loc ast.Location) (*ast.TypeDeclStmt, bool) {
	_, decl, _ := tc.typeIdentity(t, loc)
	return decl, decl != nil
}

// associatedHint names the near-miss when a type declares no such function.
func (tc *TypeChecker) associatedHint(t types.Type, name string, loc ast.Location) string {
	for _, fn := range tc.symTable.FunctionsNamed(name) {
		if _, isReceiver := ufcsReceiverParam(fn); isReceiver {
			if receiverAccepts(tc.symTable, fn, tc.resolveType(t, loc)) {
				return fmt.Sprintf("; %s takes `self`, so call it on a value: `value.%s(…)`", name, name)
			}
			continue
		}
		if tc.ufcsImported(name, fn, loc) {
			return fmt.Sprintf("; a free function has that name: `%s(…)`", name)
		}
	}
	return "; declare one with no `self` in an `impl` block"
}

// associatedFunctionValue is `Rng.seeded` named in value position, bound or passed along. A
// generic one has no single signature to hand over, as a bare generic function has none.
func (tc *TypeChecker) associatedFunctionValue(m *ast.MemberExpr) (types.Type, bool) {
	t, written, ok := tc.typeNamedBy(m)
	if !ok {
		return nil, false
	}
	fn, handled := tc.associatedFunctionFor(t, written, m)
	if fn == nil {
		return nil, handled
	}
	if len(fn.GenericParams) > 0 || slices.ContainsFunc(fn.Parameters, func(p ast.Parameter) bool {
		vars := map[string]bool{}
		collectTypeVars(p.Type, vars)
		return len(vars) > 0
	}) {
		tc.addErrorCode(m.GetLocation(), SeverityError, diag.CodeTypeNameAsValue,
			"%s.%s is generic, so it has no one signature to pass as a value; wrap the call you mean in a lambda",
			written, m.Property.Name)
		return nil, true
	}
	sig := tc.lambdaSignature(fn)
	tc.typeTable.Set(m, sig)
	tc.typeTable.SetAssociatedFunction(m, fn)
	return sig, true
}
