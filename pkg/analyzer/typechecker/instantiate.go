package typechecker

import (
	"fmt"
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

// mentionsNoTypeVar reports whether t is fully concrete — no type variable anywhere in
// it. It is the predicate for "may this type be offered as a *context*": an expectation
// still mentioning a variable is not an expectation, it is the question being asked, and
// unifying a callee's return against it either fails harmlessly or binds the wrong thing.
//
// One answer, because three positions ask it — a data constructor's payload, a named
// tuple's element, and whether a lambda's blank may be filled from its slot.
func mentionsNoTypeVar(t types.Type) bool {
	// The same walk isConcreteEnoughToElaborate performs with nothing plantable, rather
	// than a second copy of it: "fully concrete" is one question, asked here about an
	// expectation and there about a blank being filled in.
	return isConcreteEnoughToElaborate(t, nil)
}

// plantableVars are the type variables a substitution's **values** mention — the caller's
// own vocabulary, arrived through a solve.
//
// A variable in this set is not unsolved; it is an enclosing declaration's type parameter,
// which is a real type in every specialization of that declaration. That is what lets a
// lambda literal be elaborated inside a generic function, where the slot it fills is
// `(t, t) -> Ordering` rather than anything concrete. A callee variable that is genuinely
// unsolved appears in no value and so is absent, which is what keeps it blank for the
// lambda's own body to solve.
func plantableVars(subst map[string]types.Type) map[string]bool {
	if len(subst) == 0 {
		return nil
	}
	vars := map[string]bool{}
	for _, v := range subst {
		collectTypeVars(v, vars)
	}
	return vars
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
// An **untyped literal** argument settles to its default width, but **last**: it takes
// the type the rest of the call already bound the variable to, and its default only if
// nothing did. `identity(7)` still gives `t = i64`, and `count.min(80)` on a `u8` gives
// `t = u8` rather than failing.
//
// It settled *before* unifying until 08/22, and the failure that hid in that is the
// reason the pass exists in this shape: a literal promoted to `i64` bound the variable
// as `i64`, so a `u8` argument beside it bound the same variable as `u8`, and the call
// was rejected as inconsistent — "cannot infer type variable t from these arguments",
// about a call that determines it perfectly well. Every width but the default was
// affected, and the workaround was to write the conversion the compiler could have
// inferred (`count.min(u8(80))`).
//
// What has not changed is that a variable is never left *untyped*: it is a real type in
// the specialized function, deciding an alloca's width and an instruction's signedness,
// so an unresolved literal type reaching codegen is the same class of bug as an int
// literal in a float slot. The default still applies — one pass later.
func (tc *TypeChecker) solveTypeVars(lambda *ast.LambdaExpr, call *ast.FunctionCallExpr, vars map[string]bool, seed map[string]types.Type) (map[string]types.Type, bool) {
	declared := func(i int) types.Type { return tc.resolveDeclaredParam(lambda, i) }
	ret := tc.resolveTypeIfKnown(lambda.ReturnType.Type, lambda.GetLocation())
	return tc.solveArgumentTypeVars(len(lambda.Parameters), declared, call, vars, argumentSolve{
		seed:           seed,
		expectedReturn: tc.expectedReturnBindings(ret, vars),
	})
}

// argumentSolve is what a call's context contributes to solving its type variables, beyond
// the arguments themselves.
type argumentSolve struct {
	// seed pre-binds variables no parameter reaches, from the context (seedFromExpectedReturn).
	seed map[string]types.Type
	// callerVars are the enclosing body's variables a lambda literal may be given — a bound
	// receiver's `t` — beyond those the solve's own values bring (plantableVars).
	callerVars map[string]bool
	// expectedReturn is what the context binds each variable to through the callee's return
	// (expectedReturnBindings). It **settles a guess** and nothing else: an array literal a
	// lambda returns, whose fixed flavor is only its default (see settleArrayLiteralGuess).
	expectedReturn map[string]types.Type
}

// expectedReturnBindings unifies a callee's declared return with the call's context, for
// every variable in vars. Unlike seedFromExpectedReturn this reaches variables a parameter
// mentions, which is why it is only ever consulted to break a tie the arguments leave open.
func (tc *TypeChecker) expectedReturnBindings(declaredReturn types.Type, vars map[string]bool) map[string]types.Type {
	want := tc.currentExpectedType()
	if want == nil || declaredReturn == nil || len(vars) == 0 {
		return nil
	}
	bindings := map[string]types.Type{}
	if !unifyGenericTarget(declaredReturn, want, vars, bindings) {
		return nil
	}
	return bindings
}

// solveArgumentTypeVars is solveTypeVars over parameter types given by position rather than
// read off a declaration: a trait method's call has only its dispatched signature, whose
// own variables (`mapv: (Self, (i64) -> b) -> b`) are solved by exactly these rules.
// declaredParam(i) is the resolved type of the parameter call.Arguments[i] fills.
func (tc *TypeChecker) solveArgumentTypeVars(paramCount int, declaredParam func(i int) types.Type, call *ast.FunctionCallExpr, vars map[string]bool, ctx argumentSolve) (map[string]types.Type, bool) {
	seed, callerVars := ctx.seed, ctx.callerVars
	subst := map[string]types.Type{}
	// Pre-bindings the *context* supplied, for variables the arguments cannot reach
	// (seedFromExpectedReturn). Installed before the passes below so a parameter written
	// in terms of such a variable is already concrete when an argument is checked
	// against it; the passes then bind the rest.
	for k, v := range seed {
		subst[k] = v
	}
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
	// An untyped literal's own inferred type, carried to the third pass so the argument
	// is inferred once rather than twice.
	type untypedArg struct {
		index int
		typ   types.Type
	}
	var untyped []untypedArg
	var guesses []untypedArg
	for i, arg := range call.Arguments {
		if i >= paramCount {
			break
		}
		declared := declaredParam(i)
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
		// A literal with no width of its own has nothing to say about which type the
		// variable is — only about which types it *could* be — so it does not get to
		// speak first. Deferred whole rather than by parameter shape: what makes it
		// deferrable is the argument being untyped, and asking that question of the
		// type is more reliable than asking it of the syntax.
		if isUntypedLiteralType(argType) {
			untyped = append(untyped, untypedArg{index: i, typ: argType})
			continue
		}
		// A construction that solved nothing — `None`, or `Ok(v)` against a `Result<t,
		// e>` — records its declaration's bare form, and that form says as little about
		// the variable as an untyped literal does: only which declaration, never which
		// instantiation. It speaks last for the same reason. Unified first it *failed*
		// the call whenever another argument had already bound the variable, because a
		// bound variable is checked by equality and `Maybe` is not `Maybe<string>`:
		// `m.insert(key, None)` on a `HashMap<string, Maybe<string>>` reported "cannot
		// infer type variables k, v" — both of them, for a call whose receiver fixes
		// both — while `m.insert(key, Some(s))` beside it solved (09/07). In the third
		// pass a bound variable is met by assignability, which a bare declaration
		// satisfies nominally, and the ordinary argument check then stamps the
		// construction with the instantiation the call settled on.
		if tc.isBareGenericConstruction(argType, arg.GetLocation()) {
			untyped = append(untyped, untypedArg{index: i, typ: argType})
			continue
		}
		// An **array literal passed to a bare variable** is the third such guess: its
		// fixed `[N]T` is only the flavor it takes when nothing says otherwise, and here
		// nothing has spoken yet. Unified first, `m.unwrap_or([])` on a `Maybe<[]i64>`
		// bound `t` twice — `[]i64` from the receiver, `[0]?` from the literal — and
		// reported "cannot infer type variable t" (09/13). A `[]t` parameter is
		// arrayLiteralAsDeclared's case below and is not deferred.
		if g, isVar := declared.(types.GenericType); isVar && vars[g.Name] && isFixedArrayLiteral(arg, argType) {
			untyped = append(untyped, untypedArg{index: i, typ: argType})
			continue
		}
		if !unifyGenericTarget(declared, arrayLiteralAsDeclared(arg, declared, promoteToDefault(argType)), vars, subst) {
			return nil, false
		}
	}
	for _, i := range deferred {
		declared := declaredParam(i)
		// Substitute what the other arguments settled, so `() -> t` becomes `() -> i64`
		// and the lambda has something concrete to be elaborated against.
		plantable := plantableVars(subst)
		if len(callerVars) > 0 {
			if plantable == nil {
				plantable = map[string]bool{}
			}
			for v := range callerVars {
				plantable[v] = true
			}
		}
		tc.elaborateLambda(call.Arguments[i], substituteGenerics(declared, subst), plantable)
		// The parameter type is this argument's context, so a *nested* generic call whose
		// variables its own arguments cannot reach — `take(empty())` — is solved from the
		// parameter it is being passed to. Substituted through what is bound so far, so an
		// earlier argument's solve reaches a later one.
		restoreExpected := tc.pushExpectedType(substituteGenerics(declared, subst), call.GetLocation())
		argType := tc.inferExprType(call.Arguments[i])
		restoreExpected()
		if argType == nil {
			return nil, false
		}
		// A lambda returning an array literal has guessed its flavor, the way a bare
		// array-literal argument has (the first pass defers those). It speaks after the
		// other lambdas, so they get no say in it either.
		if arrayLiteralLambdaGuess(call.Arguments[i], argType) {
			guesses = append(guesses, untypedArg{index: i, typ: argType})
			continue
		}
		if !unifyGenericTarget(declared, promoteToDefault(argType), vars, subst) {
			return nil, false
		}
	}
	for _, g := range guesses {
		if !tc.settleArrayLiteralGuess(declaredParam(g.index), g.typ, vars, subst, ctx.expectedReturn) {
			return nil, false
		}
	}
	// Third: the untyped literals, now that everything with a width has spoken. A
	// literal whose parameter is a variable the call has already bound simply adopts that
	// binding — the argument check below narrows the literal to it (propagateExpectedType),
	// exactly as it does for a non-generic parameter of that type. Only a variable still
	// free falls back to the literal's default width.
	//
	// Adoption is conditional on the literal being *able* to have that type, which is
	// what keeps a genuinely inconsistent call reported as one. `same(7, true)` against
	// `(a: t, b: t)` binds `t = bool` from the second argument, and an integer literal is
	// not assignable to a bool — so the solve fails here and the caller reports "cannot
	// infer type variable t", naming the real problem. Adopting unconditionally instead
	// let the call type as `bool` and produced two errors: an argument mismatch plus
	// whatever the wrongly-typed *result* then broke.
	for _, u := range untyped {
		declared := declaredParam(u.index)
		if g, isVar := declared.(types.GenericType); isVar && vars[g.Name] {
			if bound, isBound := subst[g.Name]; isBound {
				// assignableValue rather than isAssignable, so an array literal adopts a
				// dynamic binding it can be built as; for a scalar literal or a bare
				// construction the two agree.
				if !tc.assignableValue(call.Arguments[u.index], u.typ, bound) {
					return nil, false
				}
				continue
			}
		}
		arg := call.Arguments[u.index]
		if !unifyGenericTarget(declared, arrayLiteralAsDeclared(arg, declared, promoteToDefault(u.typ)), vars, subst) {
			return nil, false
		}
	}
	// Every variable in the signature must be solved: an unsolved one would reach
	// the backend as a type with no representation. A variable that appears only in
	// the *return* type cannot be solved from arguments at all, which is why that is
	// reported here rather than discovered during lowering.
	//
	// **A nil binding is not a solve.** A lambda literal whose parameter nothing could type
	// infers as `(?) -> string`, and unifying that against `(a) -> b` binds `a` to the `?` —
	// a nil. Counted as solved, `app2((x) => "s")` type-checked and reached the backend as
	// `unknown type: <nil>`; through a trait method it panicked in describeBindings.
	for name := range vars {
		if bound, ok := subst[name]; !ok || bound == nil {
			return nil, false
		}
	}
	return subst, true
}

// isFixedArrayLiteral reports whether arg is an array literal or a repeat whose recorded
// type is the fixed array it infers on its own — a flavor its context may still change.
func isFixedArrayLiteral(arg ast.Expression, argType types.Type) bool {
	switch arg.(type) {
	case *ast.ArrayLiteralExpr, *ast.ArrayRepeatExpr:
		_, fixed := argType.(types.StaticArrayType)
		return fixed
	}
	return false
}

// arrayLiteralLambdaGuess reports whether arg is a lambda literal with no written return
// whose inferred return is a fixed array built by an array literal as its value — `(x) =>
// [x]`. That `[1]i64` is a default, not a statement: the same literal against a `[]i64`
// return is a dynamic array, and solving a variable from the default made `app(n, (x) =>
// [x])` in a `-> []i64` function fail its own return (`expected DynamicArray<i64>, got
// StaticArray<i64, 1>`), where a non-generic callee gets `[]i64` planted before inference.
func arrayLiteralLambdaGuess(arg ast.Expression, argType types.Type) bool {
	lambda, ok := arg.(*ast.LambdaExpr)
	if !ok || lambda.ReturnType.Type != nil || lambda.Body == nil {
		return false
	}
	lt, ok := argType.(*types.LambdaType)
	if !ok || lt == nil {
		return false
	}
	if _, fixed := lt.ReturnType.Type.(types.StaticArrayType); !fixed {
		return false
	}
	switch valueExpr(lambda.Body).(type) {
	case *ast.ArrayLiteralExpr, *ast.ArrayRepeatExpr:
		return true
	}
	return false
}

// valueExpr is the expression that supplies a body's value: a block's final expression,
// followed through nested blocks, or the body itself.
func valueExpr(body ast.Expression) ast.Expression {
	for {
		block, ok := body.(*ast.BlockExpr)
		if !ok || len(block.Statements) == 0 {
			return body
		}
		last, ok := block.Statements[len(block.Statements)-1].(*ast.ExpressionStmt)
		if !ok {
			return body
		}
		body = last.Expression
	}
}

// settleArrayLiteralGuess unifies a guessed lambda (arrayLiteralLambdaGuess) into the solve.
//
// The dynamic flavor wins only when the context asked for exactly it: unified as `(…) ->
// []E` instead, the lambda must bind some variable the call's context (expectedReturn)
// binds to the same type. Anything less keeps the fixed default. The strictness is what keeps
// a context that is not quite this call's harmless — one reached through an `if`'s tail is
// the whole `if`'s, say — since agreeing with it exactly can change only the flavor of an
// array the lambda builds anyway, never introduce a type error. (Until 09/14 the stack also
// leaked into statements inside a branch; withoutExpectedType closed that.)
//
// The lambda itself is re-flavored when the argument check plants the solved return onto it
// (elaborateLambda → reflavorArrayLiteralReturn).
func (tc *TypeChecker) settleArrayLiteralGuess(declared, argType types.Type, vars map[string]bool, subst, expectedReturn map[string]types.Type) bool {
	lt := argType.(*types.LambdaType)
	static := lt.ReturnType.Type.(types.StaticArrayType)
	if len(expectedReturn) > 0 {
		dynamic := *lt
		dynamic.ReturnType.Type = types.DynamicArrayType{ElementType: promoteToDefault(static.ElementType)}
		trial := make(map[string]types.Type, len(subst))
		for k, v := range subst {
			trial[k] = v
		}
		if unifyGenericTarget(declared, promoteToDefault(&dynamic), vars, trial) {
			asked := false
			for v, bound := range trial {
				if _, before := subst[v]; before {
					continue
				}
				if want, ok := expectedReturn[v]; ok && types.TypesEqual(want, bound) {
					asked = true
				}
			}
			if asked {
				for k, v := range trial {
					subst[k] = v
				}
				return true
			}
		}
	}
	return unifyGenericTarget(declared, promoteToDefault(argType), vars, subst)
}

// isBareGenericConstruction reports whether t is the bare form of a *generic* data
// declaration — the type a construction records when its arguments solved none, or only
// some, of the declaration's parameters. A non-generic data type's bare form is its
// whole type and unifies like any other.
func (tc *TypeChecker) isBareGenericConstruction(t types.Type, loc ast.Location) bool {
	name, ok := dataTypeName(t)
	if !ok {
		return false
	}
	decl, found := tc.symTable.LookupTypeFrom(name, loc)
	return found && decl != nil && len(decl.GenericParams) > 0
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
		// A declared field mentioning none of *this declaration's* parameters is a
		// context for the value written in it, the same push a struct field makes:
		// `Wrap(bag_new())` against `Wrap(Bag<string>)` has to seed the callee's
		// variables before the argument is inferred, because a generic call reports its
		// own failure inside inferExprType. A field that does mention one (`Some(t)`) is
		// what this loop exists to solve, so it has nothing to offer and pushing it
		// would present an unsolved variable as an expectation.
		var restoreExpected func()
		if !mentionsGenericParam(declaredFields[i], vars) {
			restoreExpected = tc.pushExpectedType(declaredFields[i], arg.GetLocation())
		}
		argType := tc.inferExprType(arg)
		if restoreExpected != nil {
			restoreExpected()
		}
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
	subst, ok := tc.explicitTypeArguments(calleeName, lambda, call, vars)
	if !ok {
		return nil
	}
	if subst == nil {
		subst, ok = tc.solveTypeVars(lambda, call, vars, tc.seedFromExpectedReturn(lambda, vars))
		if !ok {
			tc.addError(call.GetLocation(), SeverityError,
				"%s: cannot infer %s from these arguments%s", calleeName, typeVarList(vars),
				turbofishHint(lambda, call))
			return nil
		}
	}
	params, ret := tc.instantiateSignature(lambda, subst)
	for i, arg := range call.Arguments {
		// Now that every variable is solved, a lambda argument's remaining blanks are
		// concrete: `(t) -> u` has become `(i64) -> i64`. The deferred pass above could
		// only fill what was already known *before* this lambda's own body was inferred —
		// its return type is precisely what the body solved, so it is filled here or not
		// at all, and the backend needs it to lower the lambda as a value.
		tc.elaborateLambda(arg, params[i], plantableVars(subst))
		argType := tc.inferExprType(arg)
		if argType == nil || params[i] == nil {
			continue
		}
		argType, reported := tc.contextualType(arg, params[i], argType)
		if reported {
			continue // already named the offending value
		}
		if !tc.assignableValue(arg, argType, params[i]) {
			got, want, _ := mismatchNames(argType, params[i])
			tc.addError(arg.GetLocation(), SeverityError,
				"%s: argument %d: cannot assign %s to %s", calleeName, i+1, got, want)
			continue
		}
		// The solved parameter type is the argument's context, exactly as a concrete
		// one is: narrow an untyped literal leaf to it so the specialization's body
		// and its arguments agree on width, and hand a construction the type
		// arguments it could not solve for itself. The latter is what makes
		// `unwrap_or(None, 42)` lower — `None` fixes nothing, and the parameter type
		// is only `Maybe<i64>` once the *other* argument has solved `t`, so the
		// concrete-callee propagation site never sees an instantiation to push.
		tc.propagateExpectedType(arg, params[i])
		// A solved parameter is a width like any other, and the same "no downstream to
		// report it" rule applies.
		tc.checkLiteralRange(
			fmt.Sprintf("%s: argument %d", calleeName, i+1), arg, params[i])
	}
	// Checked after the solve and before the instantiation is recorded: every
	// variable now has the concrete type this call binds it to, which is the only
	// point at which "does the argument satisfy the bound" is a question with an
	// answer.
	tc.checkGenericBounds(calleeName, lambda, call, subst)
	tc.warnFloatEqualityAtInstantiation(calleeName, lambda, call, subst)
	disc := tc.instantiationDisc(lambda, calleeName)
	if disc != "" && strings.HasPrefix(disc, localDiscPrefix) {
		subst = tc.withEnclosingTypeVars(lambda, subst)
	}
	tc.instantiations.Set(call, typetable.Instantiation{
		Name: calleeName, Func: lambda, Disc: disc, Subst: subst,
		// The *call's* location, not the declaration's: the type arguments were resolved
		// here, so this is the module a private type among them can be found in.
		Site: call.GetLocation(),
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
//
// **A generic declared inside a function gets one too**, naming where it is declared: its
// name is a local binding's, so `idf<t=i64>` in `main` and a top-level `idf<t=i64>` — or
// two functions' own local `idf`s — would otherwise share a key, a symbol and an ownership
// table (09/13). The backend reads the discriminant's presence as "this is a local
// generic" only through the key; what decides locality there is the declaration itself.
func (tc *TypeChecker) instantiationDisc(lambda *ast.LambdaExpr, name string) string {
	if tc.symTable == nil {
		return ""
	}
	if fn, found := tc.symTable.LookupFunctionFrom(name, lambda.GetLocation()); !found || fn != lambda {
		if !tc.isOverloadMember(lambda) {
			loc := lambda.GetLocation()
			return fmt.Sprintf("%s%d_%d", localDiscPrefix, loc.StartLine, loc.StartCol)
		}
	}
	recv, ok := ast.ReceiverParam(lambda)
	if !ok {
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

// localDiscPrefix marks the discriminant of a generic declared inside a function.
const localDiscPrefix = "local_"

// withEnclosingTypeVars adds the type variables of the generic function enclosing a local
// generic to its instantiation, each bound to itself: `{t: u}` becomes `{t: u, u: u}`.
//
// A local generic's body may mention the enclosing function's variables as well as its own —
// a capture of type `u`, a parameter `(a: t, b: u)` — and an instantiation carrying only its
// own could neither lower that body nor tell `outer<i64>`'s specialization of it from
// `outer<string>`'s, which would share a key. The identity binding is a *template* binding,
// so composition does the rest: inside `outer<u = i64>` it becomes `{t: i64, u: i64}`, which
// is a key per outer specialization and a substitution covering the whole body (09/13).
// A variable the local generic declares for itself shadows the enclosing one of that name.
//
// **Every enclosing generic counts**, not only the top-level one: `inner` inside a local
// `mid<m>` inside `outer<u>` may mention both `m` and `u`, and composition through `mid`'s
// instantiation settles `m` exactly as composition through `outer`'s settles `u`.
func (tc *TypeChecker) withEnclosingTypeVars(lambda *ast.LambdaExpr, subst map[string]types.Type) map[string]types.Type {
	owner := tc.enclosingTopLevel(lambda)
	if owner == nil {
		return subst
	}
	vars := map[string]bool{}
	addSignatureTypeVars(owner, vars)
	ast.WalkExprChildren(owner, nil, func(e ast.Expression) bool {
		if mid, ok := e.(*ast.LambdaExpr); ok && mid != lambda && len(mid.GenericParams) > 0 &&
			locationContains(mid.GetLocation(), lambda.GetLocation()) {
			addSignatureTypeVars(mid, vars)
		}
		return true
	})
	if len(vars) == 0 {
		return subst
	}
	out := make(map[string]types.Type, len(subst)+len(vars))
	for name, t := range subst {
		out[name] = t
	}
	for name := range vars {
		if _, own := out[name]; !own {
			out[name] = types.GenericType{Name: name}
		}
	}
	return out
}

// addSignatureTypeVars adds the type variables a function declares or its signature mentions.
func addSignatureTypeVars(fn *ast.LambdaExpr, vars map[string]bool) {
	for _, gp := range fn.GenericParams {
		vars[gp.Name] = true
	}
	for _, p := range fn.Parameters {
		types.CollectTypeVars(p.Type, vars)
	}
	types.CollectTypeVars(fn.ReturnType.Type, vars)
}

// enclosingTopLevel is the top-level function whose body contains lambda, or nil.
func (tc *TypeChecker) enclosingTopLevel(lambda *ast.LambdaExpr) *ast.LambdaExpr {
	loc := lambda.GetLocation()
	for _, owner := range tc.topLevelLambdas {
		if owner != lambda && locationContains(owner.GetLocation(), loc) {
			return owner
		}
	}
	return nil
}

func locationContains(outer, inner ast.Location) bool {
	if outer.File != inner.File {
		return false
	}
	startsBefore := outer.StartLine < inner.StartLine || (outer.StartLine == inner.StartLine && outer.StartCol <= inner.StartCol)
	endsAfter := outer.EndLine > inner.EndLine || (outer.EndLine == inner.EndLine && outer.EndCol >= inner.EndCol)
	return startsBefore && endsAfter
}

// isOverloadMember reports whether lambda is one declaration of a receiver-overloaded name,
// which a by-name lookup does not return as itself.
func (tc *TypeChecker) isOverloadMember(lambda *ast.LambdaExpr) bool {
	for _, set := range tc.symTable.OverloadSets {
		for _, member := range set.Lambdas() {
			if member == lambda {
				return true
			}
		}
	}
	return false
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

// explicitTypeArguments binds a call's turbofish (`empty::<i64>()`) to the callee's type
// parameters, positionally and in declaration order.
//
// It answers (nil, true) when there is no turbofish, which is the signal to solve from the
// arguments instead — the path every call took before this existed.
//
// **The turbofish is the only way to call a function whose variables the arguments cannot
// reach.** Solving reads argument types alone, so a parameter mentioned only in the return
// type has nothing to bind it: `let empty<t> = () -> []t => []` was uncallable in every
// position, including under an annotation that says exactly what `t` is. That shape is what
// a constructor is — `with_capacity<k,v>(cap) -> HashMap<k,v>` takes an i64 and returns the
// thing the variables live in — so a generic collection had no constructor at all.
//
// The grammar has parsed `generic_arguments` on a call since before this; the arguments
// were collected onto the node and then read by nobody, so `empty::<i64>()` reported the
// same "cannot infer" as if the turbofish were absent — accepted and silently discarded,
// which is worse than refusing it.
//
// An explicit binding is **not** checked against the arguments here. It does not need to
// be: instantiateSignature substitutes it through the signature, and the ordinary argument
// check downstream then rejects a call that disagrees with it — so `id::<i64>("x")` is an
// argument-type error naming i64 and string, rather than an inference failure naming `t`.
func (tc *TypeChecker) explicitTypeArguments(calleeName string, lambda *ast.LambdaExpr, call *ast.FunctionCallExpr, vars map[string]bool) (map[string]types.Type, bool) {
	if len(call.GenericArguments) == 0 {
		return nil, true
	}
	// Declaration order, not order of appearance in the signature — see LambdaExpr's
	// GenericParams. A lambda with no lifted parameter list (an inline literal, a trait
	// method) has no order to bind against, so the turbofish cannot apply.
	params := lambda.GenericParams
	if len(params) == 0 {
		tc.addError(call.GetLocation(), SeverityError,
			"%s: cannot take explicit type arguments; it declares no type parameters", calleeName)
		return nil, false
	}
	if len(call.GenericArguments) != len(params) {
		tc.addError(call.GetLocation(), SeverityError,
			"%s: expected %d type argument(s), got %d", calleeName, len(params), len(call.GenericArguments))
		return nil, false
	}
	subst := make(map[string]types.Type, len(params))
	for i, p := range params {
		// Resolved at the *call* site: an alias or a private type named in a turbofish
		// is visible where it was written, exactly as an annotation's would be.
		arg := tc.resolveType(call.GenericArguments[i], call.GetLocation())
		if arg == nil {
			return nil, false
		}
		subst[p.Name] = arg
	}
	// A parameter the signature never mentions cannot be bound by position with any
	// confidence that the caller meant it, but binding it is harmless — nothing reads it.
	// What must not happen is a *variable* left unsolved, which would reach the backend
	// as an uninstantiated generic; every name in vars comes from the signature, so the
	// check is that the declaration covers them.
	for v := range vars {
		if _, bound := subst[v]; !bound {
			tc.addError(call.GetLocation(), SeverityError,
				"%s: type variable %s is not one of the declared type parameters", calleeName, v)
			return nil, false
		}
	}
	return subst, true
}

// turbofishHint names the escape hatch on an inference failure, but only where it is
// actually the fix.
//
// **The test is whether the arguments could reach the variable at all**, not merely
// whether solving failed. A variable mentioned in some parameter type is reachable, so a
// failure there is a *mismatch* — `first_of([1, 2, 3])` against `(xs: []t)` where the
// literal inferred fixed — and a turbofish would not fix it: naming `t` explicitly just
// moves the same disagreement to the argument check. Only a variable no parameter mentions
// is unreachable by construction, and that is the case the turbofish exists for.
//
// Hinting on both would attach advice to the far more common mismatch, where following it
// produces a second error and no progress.
func turbofishHint(lambda *ast.LambdaExpr, call *ast.FunctionCallExpr) string {
	if len(call.GenericArguments) > 0 || len(lambda.GenericParams) == 0 {
		return ""
	}
	mentioned := map[string]bool{}
	for i := range lambda.Parameters {
		collectTypeVars(lambda.Parameters[i].Type, mentioned)
	}
	unreachable := false
	for _, p := range lambda.GenericParams {
		if !mentioned[p.Name] {
			unreachable = true
			break
		}
	}
	if !unreachable {
		return ""
	}
	names := make([]string, len(lambda.GenericParams))
	for i, p := range lambda.GenericParams {
		names[i] = p.Name
	}
	return fmt.Sprintf("; name them explicitly with ::<%s>", strings.Join(names, ", "))
}

// pushExpectedType makes t the context for the expression about to be inferred, and
// returns the function that pops it. A nil t pushes nothing and pops nothing, so an
// unannotated position costs nothing and — importantly — does not shadow an enclosing
// context with "no expectation".
func (tc *TypeChecker) pushExpectedType(t types.Type, loc ast.Location) func() {
	if t == nil {
		return func() {}
	}
	resolved := tc.resolveTypeIfKnown(t, loc)
	if resolved == nil {
		return func() {}
	}
	tc.expectedTypes = append(tc.expectedTypes, resolved)
	return func() { tc.expectedTypes = tc.expectedTypes[:len(tc.expectedTypes)-1] }
}

// withoutExpectedType runs check with no context in force — a barrier on the stack, which a
// nil pushExpectedType deliberately is not.
//
// A context belongs to a *value*, and a value reaches through the expressions that pass it
// on: an `if`'s branches, a block's tail, a match arm. It does not reach a **statement**
// nested in one. `-> Maybe<i64> => if c { let q = make(); None } else { None }` pushed
// `Maybe<i64>` around the whole `if`, so the `let` inside the branch — whose initializer has
// no annotation and so pushes nothing — still saw it, and `make<t>() -> Maybe<t>` solved `t`
// from the function's return rather than reporting it unsolvable, as the same `let` in a
// plain block body does. Every statement a block walks is checked behind this.
func (tc *TypeChecker) withoutExpectedType(check func()) {
	tc.expectedTypes = append(tc.expectedTypes, nil)
	defer func() { tc.expectedTypes = tc.expectedTypes[:len(tc.expectedTypes)-1] }()
	check()
}

// currentExpectedType is the innermost context, or nil where there is none.
func (tc *TypeChecker) currentExpectedType() types.Type {
	if len(tc.expectedTypes) == 0 {
		return nil
	}
	return tc.expectedTypes[len(tc.expectedTypes)-1]
}

// seedFromExpectedReturn binds the type variables that **no parameter mentions** by
// unifying the callee's declared return type against the context the call sits in.
//
// The restriction to unreachable variables is what makes this safe to add to a language
// that already had programs in it. A variable some parameter mentions is solved from the
// arguments, and that solve is the author's stated intent at the call; letting the context
// bind it too would introduce a second source of truth and a precedence question between
// them, and could change what an existing call means. A variable no parameter mentions has
// no competing source — argument solving cannot reach it at all — so seeding it can only
// turn a call that was refused into one that compiles.
//
// It is a *seed*, not an override: argument solving runs afterwards over the same map, so
// a call that is inconsistent for its own reasons still fails for those reasons.
func (tc *TypeChecker) seedFromExpectedReturn(lambda *ast.LambdaExpr, vars map[string]bool) map[string]types.Type {
	want := tc.currentExpectedType()
	if want == nil || len(vars) == 0 {
		return nil
	}
	// Only a declaration that **declares** its type parameters is open to this. A
	// lowercase name in a signature is a type variable whether or not a `<…>` list
	// introduced it, so `let make = (n: i64) -> t => n` has a return-only variable too —
	// but there it is far more likely a typo than an intent to be generic, and binding it
	// from the context would let a broken declaration compile at the call sites whose
	// context happens to fit and fail at the others, reporting the declaration's own
	// inconsistency somewhere else entirely. A written `<t>` is the author saying the
	// variable is meant, which is the same line the turbofish draws.
	if len(lambda.GenericParams) == 0 {
		return nil
	}
	mentioned := map[string]bool{}
	for i := range lambda.Parameters {
		collectTypeVars(lambda.Parameters[i].Type, mentioned)
	}
	unreachable := map[string]bool{}
	for v := range vars {
		if !mentioned[v] {
			unreachable[v] = true
		}
	}
	if len(unreachable) == 0 {
		return nil
	}
	declared := tc.resolveTypeIfKnown(lambda.ReturnType.Type, lambda.GetLocation())
	if declared == nil {
		return nil
	}
	seed := map[string]types.Type{}
	// Unified against the *unreachable* set alone, so a shape that also mentions a
	// parameter-solved variable contributes only the part this is allowed to bind.
	if !unifyGenericTarget(declared, want, unreachable, seed) {
		// No match — the context is a different shape from what the callee returns.
		// Not reported here: the ordinary check downstream compares the instantiated
		// return type against the context and says so in concrete terms.
		return nil
	}
	return seed
}
