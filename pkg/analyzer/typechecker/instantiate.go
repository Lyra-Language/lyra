package typechecker

import (
	"sort"
	"strings"

	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/types"
	"github.com/Lyra-Language/lyra/pkg/typetable"
)

// Generic function instantiation: solving a function's type variables from the
// types of its arguments at a call site.
//
// A generic function is declared with lowercase type variables in its signature
// (`let identity = (x: t) -> t => x` — the collector turns a lowercase type name
// into a types.GenericType, an uppercase one into a concrete UnresolvedType). The
// declaration is checked *once*, generically; a call site then solves each
// variable against the actual argument types and is checked against the resulting
// concrete signature.
//
// The solved bindings are also what the backend monomorphizes from — one
// specialized function per distinct binding set — so they are recorded per call
// site rather than discarded after checking (typetable.InstantiationTable).
//
// The unifier is the one trait dispatch already uses (unifyGenericTarget): a
// parameter type and an argument type are matched structurally, binding each
// variable to the argument's corresponding subterm, with binding consistency —
// `(a: t, b: t)` accepts `(1, 2)` and rejects `(1, "x")`. Sharing it keeps "what
// does this type variable match" one definition rather than two.

// lambdaTypeVars is the set of type-variable names a function's signature
// mentions — its parameters and its declared return type. A function with none is
// not generic and takes the ordinary checking path unchanged.
func lambdaTypeVars(lambda *ast.LambdaExpr) map[string]bool {
	vars := map[string]bool{}
	for _, p := range lambda.Parameters {
		collectTypeVars(p.Type, vars)
	}
	collectTypeVars(lambda.ReturnType.Type, vars)
	return vars
}

// collectTypeVars adds every GenericType leaf in t to vars, descending through the
// composite types a signature can be built from.
func collectTypeVars(t types.Type, vars map[string]bool) {
	switch tt := t.(type) {
	case types.GenericType:
		vars[tt.Name] = true
	case types.StaticArrayType:
		collectTypeVars(tt.ElementType, vars)
	case types.DynamicArrayType:
		collectTypeVars(tt.ElementType, vars)
	case types.TupleType:
		for _, e := range tt.Elements {
			collectTypeVars(e, vars)
		}
	case types.WeakType:
		collectTypeVars(tt.Inner, vars)
	case types.ParameterizedType:
		for _, a := range tt.TypeArguments {
			collectTypeVars(a, vars)
		}
	case *types.LambdaType:
		for _, p := range tt.Parameters {
			collectTypeVars(p.Type, vars)
		}
		collectTypeVars(tt.ReturnType.Type, vars)
	}
}

// solveTypeVars unifies each parameter's declared type against its argument's
// inferred type, returning the substitution. ok is false when a variable cannot be
// solved — either the shapes don't match, or the same variable is bound
// inconsistently by two arguments — in which case the caller reports against the
// *declared* signature, which is the error the programmer can act on.
//
// An **untyped literal** argument settles to its default width before binding
// (`identity(7)` gives `t = i64`, not `t = untyped_int`). A type variable is a real
// type in the specialized function — it decides an alloca's width and an
// instruction's signedness — so leaving it untyped would push an unresolved
// literal type into codegen, the same class of bug as an int literal in a float
// slot. Where a *narrower* width is wanted, the call site says so: `identity(u8(7))`.
func (tc *TypeChecker) solveTypeVars(lambda *ast.LambdaExpr, call *ast.FunctionCallExpr, vars map[string]bool) (map[string]types.Type, bool) {
	subst := map[string]types.Type{}
	for i, arg := range call.Arguments {
		if i >= len(lambda.Parameters) {
			break
		}
		declared := lambda.Parameters[i].Type
		if declared == nil {
			continue
		}
		argType := tc.inferExprType(arg)
		if argType == nil {
			return nil, false
		}
		if !unifyGenericTarget(declared, promoteToDefault(argType), vars, subst) {
			return nil, false
		}
	}
	// Every variable in the signature must be solved: an unsolved one would reach
	// the backend as a type with no representation. A variable that appears only in
	// the *return* type cannot be solved from arguments at all, which is why that is
	// reported here rather than discovered during lowering.
	for name := range vars {
		if _, ok := subst[name]; !ok {
			return nil, false
		}
	}
	return subst, true
}

// solveDataTypeVars solves a generic `data` type's parameters from the arguments
// supplied to one of its constructors — `Some(5)` on `data Maybe<t> = None | Some(t)`
// binds t = i64 — by unifying each declared payload field type against the argument's
// inferred type with the same unifier a generic call and trait dispatch use.
//
// An untyped literal settles to its default width before binding, for the reason it
// does at a generic call: a type argument is a real type in the instantiation (it
// decides the payload's width in the tagged-union layout), so leaving it untyped
// would push an unresolved literal type into codegen. A narrower payload is reached
// by saying so — `Some(u8(5))`, or an annotation the literal propagates through.
//
// Returns an empty (non-nil) substitution for a non-generic type, so the caller's
// substitute-then-resolve path is uniform. Partial solutions are returned as-is:
// the caller decides what an unsolved parameter means (parameterizedResult declines
// to build an instantiation from one).
func (tc *TypeChecker) solveDataTypeVars(decl *ast.TypeDeclStmt, declaredFields []types.Type, args []ast.Expression) map[string]types.Type {
	subst := map[string]types.Type{}
	if decl == nil || len(decl.GenericParams) == 0 {
		return subst
	}
	vars := make(map[string]bool, len(decl.GenericParams))
	for _, gp := range decl.GenericParams {
		vars[gp.Name] = true
	}
	for i, arg := range args {
		if i >= len(declaredFields) {
			break
		}
		argType := tc.inferExprType(arg)
		if argType == nil {
			continue
		}
		unifyGenericTarget(declaredFields[i], promoteToDefault(argType), vars, subst)
	}
	return subst
}

// instantiateSignature substitutes a solved binding set through a function's
// signature, giving the concrete parameter types and return type a call site is
// checked against.
func instantiateSignature(lambda *ast.LambdaExpr, subst map[string]types.Type) ([]types.Type, types.Type) {
	params := make([]types.Type, len(lambda.Parameters))
	for i, p := range lambda.Parameters {
		params[i] = substituteGenerics(p.Type, subst)
	}
	return params, substituteGenerics(lambda.ReturnType.Type, subst)
}

// inferGenericCall checks a call to a generic function: solve its type variables
// from the arguments, check the call against the resulting concrete signature, and
// record the specialization for the backend.
//
// Arity is checked first — an argument count mismatch would otherwise surface as a
// confusing "cannot solve t", since a missing argument is exactly a variable with
// nothing to bind it.
func (tc *TypeChecker) inferGenericCall(calleeName string, lambda *ast.LambdaExpr, call *ast.FunctionCallExpr, vars map[string]bool) types.Type {
	if len(call.Arguments) != len(lambda.Parameters) {
		tc.addError(call.GetLocation(), SeverityError,
			"%s: expected %d argument(s), got %d", calleeName, len(lambda.Parameters), len(call.Arguments))
		return nil
	}
	subst, ok := tc.solveTypeVars(lambda, call, vars)
	if !ok {
		tc.addError(call.GetLocation(), SeverityError,
			"%s: cannot infer %s from these arguments", calleeName, typeVarList(vars))
		return nil
	}
	params, ret := instantiateSignature(lambda, subst)
	for i, arg := range call.Arguments {
		argType := tc.inferExprType(arg)
		if argType == nil || params[i] == nil {
			continue
		}
		argType, reported := tc.contextualType(arg, params[i], argType)
		if reported {
			continue // already named the offending value
		}
		if !isAssignable(argType, params[i]) {
			tc.addError(arg.GetLocation(), SeverityError,
				"%s: argument %d: cannot assign %s to %s", calleeName, i+1, argType, params[i])
			continue
		}
		// The solved parameter type is the argument's context, exactly as a concrete
		// one is: narrow an untyped literal leaf to it so the specialization's body
		// and its arguments agree on width, and hand a construction the type
		// arguments it could not solve for itself. The latter is what makes
		// `unwrap_or(None, 42)` lower — `None` fixes nothing, and the parameter type
		// is only `Maybe<i64>` once the *other* argument has solved `t`, so the
		// concrete-callee propagation site never sees an instantiation to push.
		tc.propagateLiteralType(arg, params[i])
	}
	tc.instantiations.Set(call, typetable.Instantiation{Name: calleeName, Func: lambda, Subst: subst})
	return ret
}

// typeVarList renders a signature's type variables for a diagnostic, in a stable
// order.
func typeVarList(vars map[string]bool) string {
	names := make([]string, 0, len(vars))
	for n := range vars {
		names = append(names, n)
	}
	sort.Strings(names)
	if len(names) == 1 {
		return "type variable " + names[0]
	}
	return "type variables " + strings.Join(names, ", ")
}
