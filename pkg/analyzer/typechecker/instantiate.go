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

// collectTypeVars adds every type variable in t to vars, descending through the
// composite types a signature can be built from.
//
// The walk itself lives in pkg/types, shared with the backend's MentionsTypeVar
// and the checker's generic-parameter-list reconciliation — see the file comment
// on types/typevars.go for why there is exactly one copy of it.
func collectTypeVars(t types.Type, vars map[string]bool) {
	types.CollectTypeVars(t, vars)
}

// resolveDeclaredParam is a parameter's annotation as unification must see it: type
// aliases expanded, everything else as written. `offset: Index = 0` for
// `type Index = i64` otherwise reaches unifyGenericTarget as an UnresolvedType, fails
// against the argument's resolved i64, and the whole call reports "cannot infer type
// variable t" — blaming a variable the alias had nothing to do with.
//
// Resolved from the **declaration's own location**, not the call's, because that is
// where the annotation was written: an alias private to the declaring module (the
// prelude's, say) is not visible from the calling module at all, and resolving from
// the call site would silently fail for exactly the calls this exists to fix. The
// quiet twin, since a genuinely unknown name in a signature is the signature pass's
// error to report, once.
func (tc *TypeChecker) resolveDeclaredParam(lambda *ast.LambdaExpr, i int) types.Type {
	declared := lambda.Parameters[i].Type
	if declared == nil {
		return nil
	}
	return tc.resolveTypeIfKnown(declared, lambda.GetLocation())
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
	// **Two passes, and the order is the point.** A lambda literal missing annotations
	// cannot be inferred until it knows what is expected of it — but what is expected
	// (`() -> t`) is not concrete until the *other* arguments have solved `t`. Unifying it
	// in the first pass therefore failed the whole call: `unwrap_or_else(m, () => 0)`
	// reported "cannot infer type variable t" even though `m` determines it.
	//
	// A *fully annotated* lambda is not deferred: it carries real types, so it can solve
	// variables itself (`unwrap_or_else(None, () -> i64 => 0)` solves `t` from the
	// callback's return), and deferring it would lose that.
	var deferred []int
	for i, arg := range call.Arguments {
		if i >= len(lambda.Parameters) {
			break
		}
		declared := tc.resolveDeclaredParam(lambda, i)
		if declared == nil {
			continue
		}
		if needsContextualTypes(arg) {
			deferred = append(deferred, i)
			continue
		}
		argType := tc.inferExprType(arg)
		if argType == nil {
			return nil, false
		}
		if !unifyGenericTarget(declared, arrayLiteralAsDeclared(arg, declared, promoteToDefault(argType)), vars, subst) {
			return nil, false
		}
	}
	for _, i := range deferred {
		declared := tc.resolveDeclaredParam(lambda, i)
		// Substitute what the other arguments settled, so `() -> t` becomes `() -> i64`
		// and the lambda has something concrete to be elaborated against.
		tc.elaborateLambda(call.Arguments[i], substituteGenerics(declared, subst))
		argType := tc.inferExprType(call.Arguments[i])
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

// arrayLiteralAsDeclared reads an **array literal** argument's type the way the
// parameter it is being passed to will build it: as `[]T` when the parameter is a
// dynamic array, rather than the `[N]T` the literal infers on its own.
//
// This is the one place a Lyra expression's *representation* is chosen by its
// context — `[1, 2, 3]` is a fixed `[3]T` or a heap-allocated `[]T` "told apart by
// what the literal is used as" — and everywhere else the choice is made by
// propagating the target type onto the literal. That cannot happen here: the target
// is `[]t` and `t` is precisely what is being solved. So the shape is read off the
// declaration instead, which is enough to unify, and the ordinary propagation then
// runs against the substituted `[]i64` and records the literal as dynamic.
//
// Without it, `first_of([1, 2, 3])` against `(xs: []t)` reported *"cannot infer type
// variable t from these arguments"* while the identical call with a `[]i64` binding
// worked, and so did a `[3]t` parameter (08/13). The literal was the only thing
// standing between a legal call and a diagnostic that named the wrong problem.
//
// **Only a literal is adapted, and that is a safety rule rather than a scoping
// convenience.** A fixed-array *variable* is stack storage while `[]T` is a
// ref-counted box, so passing one where the other is expected is a
// misinterpretation of memory, not a widening — and the non-generic path does
// exactly that today: `take(ys)` with `ys: [3]i64` against `(xs: []i64)` checks
// clean and **segfaults** (see todo.md; the assignability rule's own comment says
// "literal" while its code tests only the type). Adapting every static array here
// would import that fault into generic calls; adapting only the literal leaves the
// generic path refusing what it cannot yet do safely.
//
// Only the outermost level is adapted. A nested `[][]t` taking `[[1, 2]]` is left
// unsolved rather than guessed at, since the inner elements are not necessarily
// literals and the same memory question applies one level down.
func arrayLiteralAsDeclared(arg ast.Expression, declared, argType types.Type) types.Type {
	switch arg.(type) {
	case *ast.ArrayLiteralExpr, *ast.ArrayRepeatExpr:
	default:
		return argType
	}
	if _, wantsDynamic := declared.(types.DynamicArrayType); !wantsDynamic {
		return argType
	}
	static, ok := argType.(types.StaticArrayType)
	if !ok {
		return argType
	}
	return types.DynamicArrayType{ElementType: static.ElementType}
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
// A method rather than the free function it was, because the annotations must be
// **resolved before they are substituted**: `offset: Index = 0` for `type Index = i64`
// otherwise reaches the argument check as an UnresolvedType and rejects a plain i64
// ("argument 3: cannot assign i64 to Index") — the same raw-annotation read
// solveTypeVars had, one consumer further down. Resolved from the declaration's own
// location, where the annotation was written and where a private alias is visible.
func (tc *TypeChecker) instantiateSignature(lambda *ast.LambdaExpr, subst map[string]types.Type) ([]types.Type, types.Type) {
	params := make([]types.Type, len(lambda.Parameters))
	for i := range lambda.Parameters {
		params[i] = substituteGenerics(tc.resolveDeclaredParam(lambda, i), subst)
	}
	ret := tc.resolveTypeIfKnown(lambda.ReturnType.Type, lambda.GetLocation())
	return params, substituteGenerics(ret, subst)
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
	params, ret := tc.instantiateSignature(lambda, subst)
	for i, arg := range call.Arguments {
		// Now that every variable is solved, a lambda argument's remaining blanks are
		// concrete: `(t) -> u` has become `(i64) -> i64`. The deferred pass above could
		// only fill what was already known *before* this lambda's own body was inferred —
		// its return type is precisely what the body solved, so it is filled here or not
		// at all, and the backend needs it to lower the lambda as a value.
		tc.elaborateLambda(arg, params[i])
		argType := tc.inferExprType(arg)
		if argType == nil || params[i] == nil {
			continue
		}
		argType, reported := tc.contextualType(arg, params[i], argType)
		if reported {
			continue // already named the offending value
		}
		if !tc.assignableValue(arg, argType, params[i]) {
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
	// Checked after the solve and before the instantiation is recorded: every
	// variable now has the concrete type this call binds it to, which is the only
	// point at which "does the argument satisfy the bound" is a question with an
	// answer.
	tc.checkGenericBounds(calleeName, lambda, call, subst)
	tc.warnFloatEqualityAtInstantiation(calleeName, lambda, call, subst)
	tc.instantiations.Set(call, typetable.Instantiation{
		Name: calleeName, Func: lambda, Disc: tc.instantiationDisc(lambda), Subst: subst,
	})
	return ret
}

// instantiationDisc is what tells two same-named generic declarations apart, for the key a
// specialization is emitted under.
//
// It is the receiver's type head, and only for a name that really is overloaded — the same
// discriminant `userSymbol` uses for a non-generic overload, so the two paths agree about
// what makes `map` on a `Maybe` a different function from `map` on a `[]t`. A name with one
// declaration gets no discriminant at all, which keeps every existing key and emitted
// symbol byte-for-byte what it was.
func (tc *TypeChecker) instantiationDisc(lambda *ast.LambdaExpr) string {
	recv, ok := ast.ReceiverParam(lambda)
	if !ok || tc.symTable == nil {
		return ""
	}
	// Membership is asked by **identity**, not by name: a lambda's own GetName is not the
	// name its binding gave it, and the sets are keyed by the latter — so looking one up
	// by the lambda's name finds nothing and silently returns no discriminant, which is
	// exactly the collision this exists to prevent.
	for _, set := range tc.symTable.OverloadSets {
		for _, member := range set.Lambdas() {
			if member == lambda {
				head, _ := types.HeadName(recv.Type)
				return head
			}
		}
	}
	return ""
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
