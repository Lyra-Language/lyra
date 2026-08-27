package typechecker

import (
	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/types"
)

// propagateInstantiation pushes a context's generic instantiation down onto the
// construction leaves that produce the value — the generic-type analogue of
// propagateExpectedType (which pushes a width and a storage flavor), recursing
// through the same match/if/block arm structure.
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
	// Reported here rather than left to the assignability failure below, because the
	// generic message names the symptom and not the cause: `[0; n]` in fixed-array
	// position surfaces as "cannot assign DynamicArray<integer literal> to
	// StaticArray<u32, 3>", which says the literal inferred dynamic and not *why* it
	// had to.
	if tc.reportRuntimeRepeatInFixedContext(expr, want) {
		return current, true
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
// the node must be *open* to a context (see stampableDataType); it must be the same
// declaration the context names (otherwise the mismatch is a real type error somebody
// else reports); and the declaration's parameters must match the context's arguments in
// number.
func (tc *TypeChecker) stampDataConstruction(node ast.Expression, ctor string, elements []ast.Expression, inst types.ParameterizedType) bool {
	dt, ok := tc.stampableDataType(node, inst)
	if !ok {
		return false
	}
	decl, ok := tc.symTable.LookupTypeFrom(inst.Name, node.GetLocation())
	if !ok || decl == nil || len(decl.GenericParams) != len(inst.TypeArguments) {
		return false
	}
	subst := ast.BindGenericParams(decl.GenericParams, inst.TypeArguments)

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
		tc.propagateExpectedType(elem, expected)
		// …and a payload that is itself a construction takes the same treatment, so
		// the context reaches all the way down: `Maybe<Maybe<u8>> = Some(Some 7)`
		// narrows the inner 7, not just the outer instantiation. stampAggregate does
		// the same for a struct or tuple payload.
		tc.propagateInstantiation(elem, expected)
		// A narrowed literal must still fit what it was narrowed *to*. Assignability
		// below cannot answer this — after the narrowing above, `Some 300` against a
		// `Maybe<u8>` has a u8 payload holding 300, which is assignable to u8 and
		// wrong. Until this stamp existed the question could not arise, because the
		// annotation was rejected wholesale before any narrowing happened.
		tc.checkIntegerLiteralRange(ctor, elem, expected)
		actual := tc.inferExprType(elem)
		if actual != nil && !tc.assignableValue(elem, actual, expected) {
			tc.addError(elem.GetLocation(), SeverityError,
				"%s: cannot assign %s to %s", ctor, actual, expected)
			return true // leave the node bare: a wrong payload must not lower as this instantiation
		}
		tc.typeTable.Set(elem, expected)
	}
	tc.typeTable.Set(node, inst)
	return false
}

// fieldTakesWidthFromSolve reports whether a declared payload field's type depends on the
// declaration's type parameters, and so gets its width from the substitution rather than
// from the declaration.
//
// This is the line between `Wrapped(u8)`, whose payload is a u8 because the author said
// so, and `Some t`, whose payload width is whatever solving decided — which for an untyped
// literal is that literal's default. Only the second kind may be deferred to a context;
// deferring the first leaves the leaf untyped and the backend stores an i64 into a u8
// slot, which is how the over-general version of this check announced itself.
func (tc *TypeChecker) fieldTakesWidthFromSolve(decl *ast.TypeDeclStmt, field types.Type) bool {
	if decl == nil || len(decl.GenericParams) == 0 {
		return false
	}
	params := make(map[string]bool, len(decl.GenericParams))
	for _, gp := range decl.GenericParams {
		params[gp.Name] = true
	}
	return mentionsGenericParam(field, params)
}

// markDefaultedConstruction records that a data construction reached its instantiation
// only by promoting an untyped payload literal to that literal's default width.
//
// The distinction it preserves is between a solve the *program* determined and one this
// expression guessed. `Some(x)` where `x: i64` is a `Maybe<i64>` because the program says
// so; `Some 7` is a `Maybe<i64>` because an untyped 7 defaults to i64 — and that guess
// must not outrank a context saying `Maybe<u8>`. Without the mark the two are
// indistinguishable after the fact: both are recorded as `Maybe<i64>`, so the annotated
// `let m: Maybe<u8> = Some 7` was rejected with "cannot assign Maybe<i64> to Maybe<u8>"
// against an annotation that was right there.
func (tc *TypeChecker) markDefaultedConstruction(node ast.Expression) {
	if tc.defaultedCtors == nil {
		tc.defaultedCtors = map[ast.Expression]bool{}
	}
	tc.defaultedCtors[node] = true
}

// stampableDataType returns the data declaration behind a node the context may still
// complete, and whether there is one.
//
// Two shapes qualify, for the same underlying reason — the node's recorded type does not
// yet reflect a decision the program made:
//
//   - **the bare declaration.** A construction that solved nothing (`None`) or only some
//     of its parameters (`Ok(v)` for a `Result<t, e>`) is deliberately left bare, since
//     fabricating an instantiation from a partial substitution would claim precision the
//     construction did not supply.
//   - **an instantiation this expression defaulted into** (markDefaultedConstruction).
//
// Everything else is refused, and that is the load-bearing half: an instantiation the
// program genuinely determined has already been checked against the context by ordinary
// assignability, so overriding it here would let a real mismatch through.
func (tc *TypeChecker) stampableDataType(node ast.Expression, inst types.ParameterizedType) (types.DataType, bool) {
	recorded, ok := tc.typeTable.Get(node)
	if !ok {
		return types.DataType{}, false
	}
	switch r := recorded.(type) {
	case types.DataType:
		return r, r.Name == inst.Name
	case types.ParameterizedType:
		if r.Name != inst.Name || !tc.defaultedCtors[node] {
			return types.DataType{}, false
		}
		// The constructors come from the declaration: an instantiation carries its
		// type arguments, not its payload shapes.
		decl, found := tc.symTable.LookupTypeFrom(r.Name, node.GetLocation())
		if !found || decl == nil {
			return types.DataType{}, false
		}
		dt, isData := decl.Type.(types.DataType)
		return dt, isData
	}
	return types.DataType{}, false
}

// mentionsGenericParam reports whether t still contains any of the named type
// parameters — i.e. whether a substitution left it incompletely concrete.
//
// It walks the type rather than testing its name, because a parameter hides at any
// depth: `t`, `Maybe<t>`, `[3]t`, `(t, i64)` and `shared List<t>` are all incomplete,
// and only the first is caught by a name comparison.
//
// **Both walks, because a parameter reaches here under two spellings.** A resolved one is
// a `GenericType`, which `CollectTypeVars` finds; an unresolved one is an `UnresolvedType`
// carrying the same name, which `CollectTypeNames` finds. This used to be a third private
// switch over composite types and had drifted the way CLAUDE.md rule 8 says one will —
// no case for `AnonymousStructType`, `RawPointerType`, `RangeType`, `*LambdaType` or
// `*ConstrainedType`, so a field typed `{ v: t }`, `^t` or `(t) -> u` read as fully
// concrete and was stamped as an instantiation that still had a variable in it.
//
// Collecting the nominal names too cannot produce a false positive: a type parameter is
// lowercase by lexer rule and a nominal type name is not, so the two name spaces do not
// meet. That is also what the old switch's trailing `params[t.GetName()]` fallback was
// reaching for, less precisely.
func mentionsGenericParam(t types.Type, params map[string]bool) bool {
	if t == nil || len(params) == 0 {
		return false
	}
	found := map[string]bool{}
	types.CollectTypeVars(t, found)
	types.CollectTypeNames(t, found)
	for name := range found {
		if params[name] {
			return true
		}
	}
	return false
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
	decl, ok := tc.symTable.LookupTypeFrom(inst.Name, node.GetLocation())
	if !ok || decl == nil || len(decl.GenericParams) != len(inst.TypeArguments) {
		return false
	}
	subst := ast.BindGenericParams(decl.GenericParams, inst.TypeArguments)
	for i, v := range values {
		if i >= len(declared) || v == nil {
			break
		}
		expected := tc.resolveType(substituteGenerics(declared[i], subst), v.GetLocation())
		if expected == nil {
			continue
		}
		tc.propagateExpectedType(v, expected)
		tc.propagateInstantiation(v, expected) // a nested partly solved construction
		actual := tc.inferExprType(v)
		if actual != nil && !tc.assignableValue(v, actual, expected) {
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
