package typechecker

import (
	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/types"
)

// propagateInstantiation pushes a context's generic instantiation down onto the
// construction leaves that produce the value — the generic-type analogue of
// propagateLiteralType (which pushes a width) and propagateAllocation (which pushes a
// storage flavor), recursing through the same match/if/block arm structure.
//
// It exists because a construction only evaluates to an instantiation when it solves
// *every* type parameter by itself. `Some(v)` fixes `t` and is a `Maybe<i64>`; `None`
// fixes nothing and `Ok(v)` fixes `t` but not `e`, so both stay the bare declaration —
// deliberately, since fabricating an instantiation from a partial substitution would
// claim precision the construction did not supply. The consequence was that a partly
// solved construction could not lower at all: `(n: i64) -> Maybe<i64> => None` failed
// the build with `unknown named type "Maybe"`, and the prelude's `Result` was unusable
// in a return position, because neither `Ok` nor `Err` determines both parameters.
//
// The context is where the missing arguments actually live, and it was already being
// applied at exactly one site — an annotated `let`, which overwrites the value's
// recorded type with the annotation wholesale. That is why `let m: Maybe<i64> = None`
// has always worked while returning the same expression did not. This generalizes that
// one-off to the sites the other two propagators already cover, and reaches inside
// `match`/`if` arms, which the wholesale overwrite never did.
//
// **It checks rather than assumes.** A partly solved construction's payload is not
// currently verified against the context — `let r: Result<i64, string> = Ok("x")` passes
// the front end today and is caught only by the backend refusing to store a string into
// an i64 payload. Stamping the instantiation without re-checking would keep that error
// in the backend, where a type error does not belong; worse, it would be one step from
// silence. So each payload element is re-checked against the field type the *context's*
// substitution gives, and a mismatch is reported here.
func (tc *TypeChecker) propagateInstantiation(expr ast.Expression, want types.Type) bool {
	inst, ok := want.(types.ParameterizedType)
	if !ok || expr == nil {
		return false
	}
	switch e := expr.(type) {
	case *ast.DataConstructorExpr:
		// A nullary constructor solves nothing, so it is *always* the bare
		// declaration and always needs the context: `None`, `Red`.
		return tc.stampDataConstruction(e, e.Constructor, nil, inst)
	case *ast.TupleLiteralExpr:
		// One node, two meanings: an applied data constructor (`Some(v)`) and a
		// named-tuple literal (`Pair(1, 2)`) parse identically, and are told apart
		// here the same way inference tells them apart — by what was recorded.
		if recorded, ok := tc.typeTable.Get(e); ok {
			if tt, isTuple := recorded.(types.TupleType); isTuple && tt.Name == inst.Name {
				return tc.stampAggregate(e, e.Elements, nil, tt.Elements, inst)
			}
		}
		return tc.stampDataConstruction(e, e.Name, e.Elements, inst)
	case *ast.StructInstanceExpr:
		recorded, ok := tc.typeTable.Get(e)
		if !ok {
			return false
		}
		st, isStruct := recorded.(types.NamedStructType)
		if !isStruct || st.Name != inst.Name {
			return false
		}
		values := make([]ast.Expression, 0, len(e.Fields))
		names := make([]string, 0, len(e.Fields))
		declared := make([]types.Type, 0, len(e.Fields))
		for idx, f := range e.Fields {
			name := f.Name
			if name == "" && idx < len(st.Fields) {
				name = st.Fields[idx].Name
			}
			for _, df := range st.Fields {
				if df.Name == name {
					values = append(values, f.Value)
					names = append(names, name)
					declared = append(declared, df.Type)
					break
				}
			}
		}
		return tc.stampAggregate(e, values, names, declared, inst)
	case *ast.MatchExpr:
		reported := false
		for _, arm := range e.MatchArms {
			reported = tc.propagateInstantiation(arm.Body, want) || reported
		}
		return reported
	case *ast.IfExpr:
		thenReported := tc.propagateInstantiation(e.Then, want)
		return tc.propagateInstantiation(e.Else, want) || thenReported
	case *ast.BlockExpr:
		// A block's value is its final statement, the same tail the other two
		// propagators follow.
		if n := len(e.Statements); n > 0 {
			if es, ok := e.Statements[n-1].(*ast.ExpressionStmt); ok {
				return tc.propagateInstantiation(es.Expression, want)
			}
		}
	}
	return false
}

// contextualType applies want's instantiation to expr and reports the type expr should
// now be compared against.
//
// The ordering it exists to fix: a partly solved *data* construction is assignable to
// any instantiation of itself, so propagating after the check worked; a partly solved
// struct or named tuple is not, so the check rejected correct code before the context
// could complete it. Propagating first and re-reading the record makes both paths agree.
// The bool reports whether the propagation already emitted a diagnostic, so the caller
// suppresses its own: a wrong payload otherwise yields both the precise "Tagged.value:
// cannot assign string to i64" and a coarse "return type mismatch … got Tagged", two
// errors for one mistake.
func (tc *TypeChecker) contextualType(expr ast.Expression, want, current types.Type) (types.Type, bool) {
	if expr == nil || want == nil {
		return current, false
	}
	reported := tc.propagateInstantiation(expr, want)
	if t, ok := tc.typeTable.Get(expr); ok && t != nil {
		return t, reported
	}
	return current, reported
}

// stampDataConstruction re-records one data-constructor node at the instantiation the
// context names, after checking its payload against that instantiation's substitution.
//
// Three guards decide whether the stamp applies at all, and each one is load-bearing:
// the node must currently be recorded as the *bare* declaration (an already-solved
// construction has been checked against the context by ordinary assignability, and
// overriding it would let a genuine mismatch through); it must be the same declaration
// the context names (otherwise the mismatch is a real type error somebody else reports);
// and the declaration's parameters must match the context's arguments in number.
func (tc *TypeChecker) stampDataConstruction(node ast.Expression, ctor string, elements []ast.Expression, inst types.ParameterizedType) bool {
	recorded, ok := tc.typeTable.Get(node)
	if !ok {
		return false
	}
	dt, isData := recorded.(types.DataType)
	if !isData || dt.Name != inst.Name {
		return false
	}
	decl, ok := tc.symTable.LookupType(inst.Name)
	if !ok || decl == nil || len(decl.GenericParams) != len(inst.TypeArguments) {
		return false
	}
	subst := make(map[string]types.Type, len(decl.GenericParams))
	for i, gp := range decl.GenericParams {
		subst[gp.Name] = inst.TypeArguments[i]
	}

	var declaredFields []types.Type
	for _, c := range dt.Constructors {
		if c.Name == ctor {
			declaredFields = c.FieldTypes()
			break
		}
	}
	for i, elem := range elements {
		if i >= len(declaredFields) {
			break // arity is reported by the constructor's own inference
		}
		expected := tc.resolveType(substituteGenerics(declaredFields[i], subst), elem.GetLocation())
		if expected == nil {
			continue
		}
		// Narrow an untyped literal to the context's width first, so `Ok(1)` against
		// `Result<u8, string>` records a u8 payload rather than the i64 default —
		// exactly what the local solve does, but with the arguments it lacked.
		tc.propagateLiteralType(elem, expected)
		actual := tc.inferExprType(elem)
		if actual != nil && !isAssignable(actual, expected) {
			tc.addError(elem.GetLocation(), SeverityError,
				"%s: cannot assign %s to %s", ctor, actual, expected)
			return true // leave the node bare: a wrong payload must not lower as this instantiation
		}
		tc.typeTable.Set(elem, expected)
	}
	tc.typeTable.Set(node, inst)
	return false
}

// mentionsGenericParam reports whether t still contains any of the named type
// parameters — i.e. whether a substitution left it incompletely concrete.
//
// It walks the type rather than testing its name, because a parameter hides at any
// depth: `t`, `Maybe<t>`, `[3]t`, `(t, i64)` and `shared List<t>` are all incomplete,
// and only the first is caught by a name comparison.
func mentionsGenericParam(t types.Type, params map[string]bool) bool {
	switch v := t.(type) {
	case types.GenericType:
		return params[v.Name]
	case types.UnresolvedType:
		return params[v.Name]
	case types.ParameterizedType:
		for _, a := range v.TypeArguments {
			if mentionsGenericParam(a, params) {
				return true
			}
		}
	case types.TupleType:
		for _, e := range v.Elements {
			if mentionsGenericParam(e, params) {
				return true
			}
		}
	case types.StaticArrayType:
		return mentionsGenericParam(v.ElementType, params)
	case types.DynamicArrayType:
		return mentionsGenericParam(v.ElementType, params)
	case types.WeakType:
		return mentionsGenericParam(v.Inner, params)
	}
	return params[t.GetName()]
}

// stampAggregate is stampDataConstruction's counterpart for a generic **struct** or
// **named tuple**: the same re-check-then-record, keyed by field name or by position.
//
// These fail differently from a data constructor, which is why they needed their own
// pass. A bare `DataType` was assignable to any instantiation of itself, so a partly
// solved data construction reached the backend and died there; a bare `NamedStructType`
// or `TupleType` is *not*, so a partly solved one is rejected by the front end with
// "return type mismatch: expected Tagged<i64, boolean>, got Tagged" — a spurious error
// on correct code rather than a lowering failure.
//
// After structural field inference (inferStructGenericArgs / the named-tuple solver),
// the only parameters that can still be unsolved are ones that appear in no field at
// all — a phantom parameter. Its fields are therefore already checked correctly, and
// the recursion below exists for the other case: a field whose own value is itself a
// partly solved construction, which the context can now complete.
func (tc *TypeChecker) stampAggregate(node ast.Expression, values []ast.Expression, names []string, declared []types.Type, inst types.ParameterizedType) bool {
	decl, ok := tc.symTable.LookupType(inst.Name)
	if !ok || decl == nil || len(decl.GenericParams) != len(inst.TypeArguments) {
		return false
	}
	subst := make(map[string]types.Type, len(decl.GenericParams))
	for i, gp := range decl.GenericParams {
		subst[gp.Name] = inst.TypeArguments[i]
	}
	for i, v := range values {
		if i >= len(declared) || v == nil {
			break
		}
		expected := tc.resolveType(substituteGenerics(declared[i], subst), v.GetLocation())
		if expected == nil {
			continue
		}
		tc.propagateLiteralType(v, expected)
		tc.propagateInstantiation(v, expected) // a nested partly solved construction
		actual := tc.inferExprType(v)
		if actual != nil && !isAssignable(actual, expected) {
			where := inst.Name
			if i < len(names) && names[i] != "" {
				where = inst.Name + "." + names[i]
			}
			tc.addError(v.GetLocation(), SeverityError,
				"%s: cannot assign %s to %s", where, actual, expected)
			return true
		}
		tc.typeTable.Set(v, expected)
	}
	tc.typeTable.Set(node, inst)
	return false
}
