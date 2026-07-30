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
func (tc *TypeChecker) propagateInstantiation(expr ast.Expression, want types.Type) {
	inst, ok := want.(types.ParameterizedType)
	if !ok || expr == nil {
		return
	}
	switch e := expr.(type) {
	case *ast.DataConstructorExpr:
		// A nullary constructor solves nothing, so it is *always* the bare
		// declaration and always needs the context: `None`, `Red`.
		tc.stampDataConstruction(e, e.Constructor, nil, inst)
	case *ast.TupleLiteralExpr:
		tc.stampDataConstruction(e, e.Name, e.Elements, inst)
	case *ast.MatchExpr:
		for _, arm := range e.MatchArms {
			tc.propagateInstantiation(arm.Body, want)
		}
	case *ast.IfExpr:
		tc.propagateInstantiation(e.Then, want)
		tc.propagateInstantiation(e.Else, want)
	case *ast.BlockExpr:
		// A block's value is its final statement, the same tail the other two
		// propagators follow.
		if n := len(e.Statements); n > 0 {
			if es, ok := e.Statements[n-1].(*ast.ExpressionStmt); ok {
				tc.propagateInstantiation(es.Expression, want)
			}
		}
	}
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
func (tc *TypeChecker) stampDataConstruction(node ast.Expression, ctor string, elements []ast.Expression, inst types.ParameterizedType) {
	recorded, ok := tc.typeTable.Get(node)
	if !ok {
		return
	}
	dt, isData := recorded.(types.DataType)
	if !isData || dt.Name != inst.Name {
		return
	}
	decl, ok := tc.symTable.LookupType(inst.Name)
	if !ok || decl == nil || len(decl.GenericParams) != len(inst.TypeArguments) {
		return
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
			return // leave the node bare: a wrong payload must not lower as this instantiation
		}
		tc.typeTable.Set(elem, expected)
	}
	tc.typeTable.Set(node, inst)
}
