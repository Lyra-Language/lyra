package typechecker

import (
	"fmt"
	"maps"

	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/types"
)

// withParamScope temporarily installs a parameter-type map for the duration of
// fn. This lets inferExprType resolve identifiers that name the lambda's
// parameters. An IdentifierPattern parameter is added directly; a destructured
// parameter (tuple, struct, or array pattern) with a type annotation has each
// of its names bound to the corresponding element/field type via
// walkDestructuredPattern — the parameter counterpart of
// checkDestructuringDecl's binding, except the type is already known
// statically from the annotation, so it can be done up front here rather than
// lazily during body-checking. An unannotated destructured parameter is
// silently skipped (there's no type to destructure against yet). Nested calls
// (e.g. a lambda inside a lambda) correctly save and restore.
//
// Each parameter's declared type is resolved through resolveType so that
// user-defined names (constrained types, structs, etc.) are expanded and
// unknown names emit an "unknown type" diagnostic.
func (tc *TypeChecker) withParamScope(lambda *ast.LambdaExpr, fn func()) {
	oldTypes, oldMods, oldBound := tc.paramTypes, tc.paramMods, tc.patternBound
	// A nested lambda is lexically inside the enclosing one, so it sees that
	// lambda's parameters too — start from them and let this lambda's own
	// parameters shadow. Replacing the map outright made an enclosing parameter
	// invisible in a nested body (`(n) -> … => (x) -> … => x + n` reported `n`
	// undefined), which stayed hidden while an annotated nested lambda's body was
	// never checked at all.
	tc.paramTypes = make(map[string]types.Type, len(oldTypes)+len(lambda.Parameters))
	tc.paramMods = make(map[string]types.TypeModifier, len(oldMods)+len(lambda.Parameters))
	// patternBound is copied and restored with them: it is a tag *on* these names, so
	// deleting a shadowed entry in place would erase an enclosing match arm's tag for
	// good rather than only for this body.
	tc.patternBound = make(map[string]bool, len(oldBound))
	maps.Copy(tc.paramTypes, oldTypes)
	maps.Copy(tc.paramMods, oldMods)
	maps.Copy(tc.patternBound, oldBound)
	for _, p := range lambda.Parameters {
		if ip, ok := p.Pattern.(*ast.IdentifierPattern); ok {
			// Record the modifier even when the parameter has no type annotation,
			// so the interior-mutation check can attribute mutability to the param.
			tc.paramMods[ip.Name] = p.TypeModifier
			delete(tc.patternBound, ip.Name) // a parameter shadowing a pattern binding is a parameter
			if p.Type != nil {
				tc.paramTypes[ip.Name] = tc.resolveType(p.Type, p.GetLocation())
			}
			continue
		}
		if p.Type != nil {
			resolved := tc.resolveType(p.Type, p.GetLocation())
			tc.walkDestructuredPattern(p.Pattern, resolved, func(name string, typ types.Type) {
				tc.paramTypes[name] = typ
			})
		}
	}
	tc.enterScope(lambda, fn)
	tc.paramTypes, tc.paramMods, tc.patternBound = oldTypes, oldMods, oldBound
}

// checkLambdaBody verifies that the lambda's body is consistent with the
// declared return type. It is a no-op when no return type is declared.
//
// For void functions it checks that no explicit `return <expr>` appears.
// For typed functions it checks that the body expression / every explicit
// return statement / the implicit last expression all match the declared type.
func (tc *TypeChecker) checkLambdaBody(funcName string, lambda *ast.LambdaExpr) {
	// A multi-clause function is a match on its parameters; rewrite it into one before
	// anything looks at the body, so the rest of this function — and every pass after it —
	// sees an ordinary lambda. See multi_clause.go for why this is a front-end elaboration.
	tc.desugarClauses(funcName, lambda)
	// Positional arguments fill left to right, so a defaulted parameter followed by one
	// without a default can never be called as written.
	tc.checkDefaultsAreTrailing(funcName, lambda)

	declaredReturn := lambda.ReturnType.Type
	// Track the enclosing return type so inferTryExpr can match a `?` operand's
	// kind (Result/Maybe) against it. Save/restore handles nested lambdas.
	prevRet := tc.enclosingRet
	ret := lambda.ReturnType
	tc.enclosingRet = &ret
	defer func() { tc.enclosingRet = prevRet }()
	// Always enter the param scope: withParamScope resolves every parameter
	// type annotation via resolveType, emitting "unknown type" for any name
	// that has no declaration. This must happen even when there is no return
	// type to check.
	tc.withParamScope(lambda, func() {
		// Resolve the declared return type so that user-defined names (structs,
		// data types, constrained types) compare equal to the resolved parameter
		// types produced by withParamScope. Without this, `(v: Vec2) -> Vec2 => v`
		// would compare NamedStructType("Vec2") against UnresolvedType("Vec2").
		// Use resolveTypeIfKnown (not resolveType) to avoid emitting a duplicate
		// "unknown type" diagnostic when the return annotation names an unknown type.
		//
		// declaredReturn is nil for an inferred-return function (`() => body`
		// with no `-> T`). The body is still walked below so body-level
		// diagnostics surface; only the return-type comparison is skipped.
		if declaredReturn != nil {
			declaredReturn = tc.resolveTypeIfKnown(declaredReturn, lambda.GetLocation())
		}

		// An owned return (bare or `own`) transfers the value to the caller, so its
		// allocation flavor must match the declared return type; a `ref`/`mut`
		// return is a borrow and is allocation-polymorphic (see isOwnedReturn).
		ownedReturn := isOwnedReturn(lambda.ReturnType.TypeModifier)

		_, isVoid := declaredReturn.(types.VoidType)
		if lambda.Body != nil {
			if block, ok := lambda.Body.(*ast.BlockExpr); ok {
				if isVoid {
					tc.checkBlockVoidReturn(funcName, block)
				} else {
					// declaredReturn may be nil (inferred return); checkBlockReturn
					// still walks every statement, skipping only the return-type
					// comparison.
					tc.checkBlockReturn(funcName, block, declaredReturn, ownedReturn)
				}
			} else if !isVoid {
				// Single-expression body: the expression value is the return value.
				tc.checkReturnValue(funcName, lambda.Body, lambda.Body.GetLocation(), declaredReturn, ownedReturn)
			} else {
				// Single-expression body of a void function: the value is discarded,
				// but still infer it so an effectful call (`() -> void => print("x")`)
				// is validated and its argument types are recorded for the backend.
				tc.inferExprType(lambda.Body)
			}
		}

		// Multi-clause body: each clause body is the return value.
		// For void functions the value is discarded, so no check is needed.
		if !isVoid {
			for _, clause := range lambda.LambdaClauses {
				clauseType := tc.inferExprType(clause.Body)
				if declaredReturn != nil && clauseType != nil && !isAssignable(clauseType, declaredReturn) {
					tc.addError(clause.Body.GetLocation(), SeverityError,
						"%s: return type mismatch: expected %s, got %s",
						funcName, declaredReturn, clauseType)
				} else if declaredReturn != nil && clauseType != nil && ownedReturn {
					tc.checkAllocationCompat(clauseType, declaredReturn, clause.Body.GetLocation(), funcName)
				}
			}
		}

		// Last, because it reads the types the walk above recorded: fill in a return
		// type that was never written.
		if lambda.ReturnType.Type == nil {
			tc.inferLambdaReturnType(funcName, lambda)
		}
	})
}

// inferLambdaReturnType fills in `lambda.ReturnType.Type` for a function written
// without `-> T`, from the type its body produces.
//
// **It writes to the AST node**, the same elaboration `contextual_lambda.go` performs
// for a lambda literal's missing annotations, and for the same reason: every later pass
// reads `ast.LambdaExpr.ReturnType`, so filling the blank once here means none of them —
// ownership, captures, the backend — needs a notion of an un-annotated function. Until
// 07/31/26 nothing did this, so `let sum = ((a, b): (i64, i64)) => a + b` type-checked
// and then failed the *build* with "needs a return type annotation", the same
// front-end-accepts-what-the-backend-refuses split that default params and multi-clause
// functions had.
//
// Scope, deliberately: the body's *value* is the return type. A body containing an
// explicit `return` is refused with a diagnostic asking for an annotation, because
// inferring it means joining several candidates and that is a design question of its own
// — what happens when they disagree, how a diverging `panic` arm participates — not
// something to settle silently here. The refusal is still an improvement: a front-end
// diagnostic naming the fix, where before there was a backend error naming an internal
// requirement.
func (tc *TypeChecker) inferLambdaReturnType(funcName string, lambda *ast.LambdaExpr) {
	if lambda.Body == nil || len(lambda.LambdaClauses) > 0 {
		return // a clause form is desugared before this; nothing to infer from
	}

	if stmt := explicitReturnIn(lambda.Body); stmt != nil {
		tc.addError(stmt.GetLocation(), SeverityError,
			"%s: a function with an explicit `return` needs a return type annotation — only a function whose body is its value can have one inferred",
			funcName)
		return
	}

	value := lambda.Body
	if block, ok := lambda.Body.(*ast.BlockExpr); ok {
		last, ok := blockValueExpr(block)
		if !ok {
			// A block that ends in something other than an expression has no value,
			// which is exactly what `void` means.
			lambda.ReturnType.Type = types.VoidType{}
			lambda.ReturnTypeInferred = true
			return
		}
		value = last
	}

	inferred, ok := tc.typeTable.Get(value)
	if !ok || inferred == nil {
		// The body was walked already, so a missing entry means inference could not
		// finish — overwhelmingly because the function calls itself, where computing
		// the return type requires the return type. Callers of a recursive function
		// need its signature, so the annotation is not a limitation to apologize for.
		tc.addError(lambda.GetLocation(), SeverityError,
			"%s: cannot infer the return type — annotate it (a recursive function always needs one)",
			funcName)
		return
	}
	lambda.ReturnType.Type = defaultUntyped(inferred)
	lambda.ReturnTypeInferred = true
}

// defaultUntyped resolves a literal's provisional type to the one it lowers as, so a
// recorded signature reads `i64` rather than "integer literal". Nothing else consumes an
// untyped type usefully — the backend already lays UntypedInt out as i64 — and leaving it
// in a *signature* would leak an inference artifact into every call site's comparison.
func defaultUntyped(t types.Type) types.Type {
	p, ok := t.(types.PrimitiveType)
	if !ok {
		return t
	}
	switch p.Name {
	case types.UntypedInt, types.UntypedSignedInt:
		return types.PrimitiveType{Name: types.Int64}
	case types.UntypedFloat:
		return types.PrimitiveType{Name: types.Float64}
	}
	return t
}

// blockValueExpr returns the expression a block evaluates to — its final statement, when
// that is an expression statement.
func blockValueExpr(block *ast.BlockExpr) (ast.Expression, bool) {
	if len(block.Statements) == 0 {
		return nil, false
	}
	last, ok := block.Statements[len(block.Statements)-1].(*ast.ExpressionStmt)
	if !ok {
		return nil, false
	}
	return last.Expression, true
}

// explicitReturnIn finds a `return` belonging to this body, or nil.
//
// It stops at a nested `LambdaExpr`: a `return` inside one belongs to *that* function,
// and counting it here would refuse to infer for an enclosing function that has no
// `return` of its own — `let f = (n: i64) => { let g = (m: i64) -> i64 => { return m }; g(n) }`.
func explicitReturnIn(body ast.Expression) *ast.ReturnStmt {
	var found *ast.ReturnStmt
	ast.WalkExpr(body,
		func(s ast.Statement) bool {
			if r, ok := s.(*ast.ReturnStmt); ok && found == nil {
				found = r
			}
			return true
		},
		func(e ast.Expression) bool {
			if _, isLambda := e.(*ast.LambdaExpr); isLambda {
				return false
			}
			return true
		},
	)
	return found
}

// checkBlockVoidReturn walks block reporting explicit `return <expr>` statements,
// which are illegal in a void function (bare `return` is allowed), and otherwise
// runs the full statement check on every statement so body-level diagnostics
// (interior mutation, must-use, etc.) surface. A void body has no typed implicit
// return, so even the final expression statement is checked for effect.
func (tc *TypeChecker) checkBlockVoidReturn(funcName string, block *ast.BlockExpr) {
	tc.enterScope(block, func() {
		for _, stmt := range block.Statements {
			switch s := stmt.(type) {
			case *ast.ReturnStmt:
				if s.Value != nil {
					tc.addError(s.GetLocation(), SeverityError,
						"%s: void function must not return a value", funcName)
				}
			case *ast.ExpressionStmt:
				tc.checkExpressionStmt(s)
			default:
				tc.checkNode(stmt)
			}
		}
	})
}

// checkBlockReturn walks the statements in block, checking:
//   - Every explicit ReturnStmt against declaredReturn.
//   - The last statement, when it is an ExpressionStmt, as an implicit return value.
//
// declaredReturn may be nil for an inferred-return function (`() => body` with
// no `-> T`). In that case the return-type comparison is skipped, but every
// statement is still walked so body-level diagnostics (const reassignment,
// interior mutation, must-use, …) surface just as they do for an annotated
// function.
//
// The block's own scope is entered for the duration so that variables declared
// inside the body (e.g. `let local: i32 = 5`) are visible to inferExprType.
// checkReturnValue checks one value against a function's declared return type. It
// is the single definition of what returning a value means, and every position that
// has one goes through it: a single-expression body, an explicit `return`, a block's
// trailing expression, and a trait-impl method's body.
//
// `loc` is separate from `value` because the two disagree about where to point — a
// `return` reports at the statement, a body expression at itself.
//
// The order is load-bearing, which is the reason to have one copy rather than four.
// contextualType runs *first* and may report on its own, and when it has, the
// coarser "return type mismatch" is suppressed: naming the offending value and then
// restating it as a whole-body mismatch is the same mistake twice. Only once the
// value is known assignable does the declared type act as its context — pushing its
// width onto untyped literal leaves so `() -> u8 => 5 + 3` lowers at u8, and
// stamping construction leaves with its allocation flavor so
// `(xs) -> shared List => match xs { … => Cons(…) }` builds shared nodes inside
// match arms. The allocation *check* is last and only for an owned return, since a
// `ref`/`mut` return is a borrow and allocation-polymorphic (isOwnedReturn).
//
// Written four times until 08/05, and the fourth had already drifted: the trait-impl
// copy did none of the propagation, so a method body's literals kept their default
// width where the identical free function's narrowed (hazard 8).
func (tc *TypeChecker) checkReturnValue(funcName string, value ast.Expression, loc ast.Location, declaredReturn types.Type, ownedReturn bool) {
	valueType := tc.inferExprType(value)
	valueType, reported := tc.contextualType(value, declaredReturn, valueType)
	if reported || declaredReturn == nil || valueType == nil {
		return
	}
	if !isAssignable(valueType, declaredReturn) {
		tc.addError(loc, SeverityError,
			"%s: return type mismatch: expected %s, got %s", funcName, declaredReturn, valueType)
		return
	}
	tc.propagateLiteralType(value, declaredReturn)
	tc.propagateAllocation(value, types.AllocationOf(declaredReturn))
	if ownedReturn {
		tc.checkAllocationCompat(valueType, declaredReturn, loc, funcName)
	}
}

func (tc *TypeChecker) checkBlockReturn(funcName string, block *ast.BlockExpr, declaredReturn types.Type, ownedReturn bool) {
	tc.enterScope(block, func() {
		stmts := block.Statements
		for i, stmt := range stmts {
			switch s := stmt.(type) {
			case *ast.ReturnStmt:
				if s.Value == nil {
					continue // bare return – void compatibility is not checked yet
				}
				tc.checkReturnValue(funcName, s.Value, s.GetLocation(), declaredReturn, ownedReturn)
			case *ast.ExpressionStmt:
				if i == len(stmts)-1 {
					// The last expression in a block is its implicit return value,
					// so it is being used (returned), not dropped.
					tc.checkReturnValue(funcName, s.Expression, s.GetLocation(), declaredReturn, ownedReturn)
				} else {
					// A non-final expression statement is evaluated for effect and
					// its value discarded; run the full statement check so dropped
					// Result/Maybe values (must-use) and other diagnostics surface.
					tc.checkExpressionStmt(s)
				}
			default:
				// Type-check non-return, non-expression statements (e.g. VarDeclStmt)
				// so their initializer types are recorded in the TypeTable before
				// they may be referenced by later expressions in the same block.
				tc.checkNode(stmt)
			}
		}
	})
}

// inferLambdaCall validates a call against a LambdaExpr (from a VarDeclStmt or
// direct lambda callee). calleeName is used in error messages.
func (tc *TypeChecker) inferLambdaCall(calleeName string, lambda *ast.LambdaExpr, call *ast.FunctionCallExpr) types.Type {
	// Fill any omitted trailing arguments from the declaration's defaults, before arity is
	// counted or the generic path is taken — both then see a call that passes everything
	// explicitly, and so does the backend. Idempotent, since the count matches afterwards.
	tc.applyDefaultArguments(lambda, call)
	// A *generic* callee's signature mentions type variables, which have to be
	// solved from this call's argument types before anything can be checked against
	// them — a declared `t` is assignable from nothing until it is bound. Solving
	// also produces the specialization the backend will emit, recorded per call site.
	if vars := lambdaTypeVars(lambda); len(vars) > 0 {
		return tc.inferGenericCall(calleeName, lambda, call, vars)
	}
	// Count required parameters (those without a default value).
	required := 0
	for _, p := range lambda.Parameters {
		if p.DefaultValue == nil {
			required++
		}
	}
	total := len(lambda.Parameters)
	got := len(call.Arguments)

	if got < required || got > total {
		if required == total {
			tc.addError(call.GetLocation(), SeverityError,
				"%s: expected %d argument(s), got %d", calleeName, total, got)
		} else {
			tc.addError(call.GetLocation(), SeverityError,
				"%s: expected %d to %d argument(s), got %d",
				calleeName, required, total, got)
		}
		return tc.resolveTypeIfKnown(lambda.ReturnType.Type, lambda.GetLocation())
	}

	// A parameter's declared type is the argument's context, which for a lambda literal
	// means its parameter and return annotations — filled in before the loop below infers
	// anything, since inferring a lambda without them reports `undefined symbol` first.
	tc.elaborateLambdaArgsFromParams(lambda.Parameters, call.Arguments)

	// Check each argument's inferred type against the parameter's declared type.
	for i, arg := range call.Arguments {
		param := lambda.Parameters[i]
		if param.Type == nil {
			continue // no type annotation on this parameter; cannot check
		}
		resolvedParamType := tc.resolveType(param.Type, param.GetLocation())
		argType := tc.inferExprType(arg)
		if argType == nil {
			continue // cannot infer argument type; skip silently
		}
		paramName := param.Pattern.GetName()
		argType, reported := tc.contextualType(arg, resolvedParamType, argType)
		if reported {
			continue // already named the offending value
		}
		if !isAssignable(argType, resolvedParamType) {
			tc.addError(arg.GetLocation(), SeverityError,
				"%s: argument %d (%s): cannot assign %s to %s",
				calleeName, i+1, paramName, argType, param.Type)
		} else {
			// The parameter type is the argument's context: push its width onto
			// untyped literal args so the backend lowers `add(200)` at the param's
			// width, not the i64 default. Applies to every assignable arg, not just
			// `own` ones (width is orthogonal to ownership).
			tc.propagateLiteralType(arg, resolvedParamType)
			if param.TypeModifier == types.Mut {
				tc.checkMutArgument(calleeName, i+1, paramName, arg, resolvedParamType)
			}
			if paramOwnsArgument(param.TypeModifier) {
				// An `own` parameter adopts the argument into its own storage, so
				// the flavors must match; a borrowed parameter is allocation-
				// polymorphic and is skipped.
				tc.checkAllocationCompat(argType, resolvedParamType, arg.GetLocation(),
					fmt.Sprintf("%s: argument %d (%s)", calleeName, i+1, paramName))
			}
		}
	}

	// `mut`/`ref` arguments are pointers to the caller's storage, so two of them
	// naming one binding alias — checked once for the whole call, since the rule is
	// about the argument list as a whole rather than any single argument.
	tc.checkExclusiveMutableBorrow(calleeName, lambda, call)

	// The declared return type is resolved (as the parameter types above already
	// are): a return type naming a declared type is stored as an UnresolvedType,
	// so an unresolved result compared unequal to the same type resolved from an
	// annotation — `let p: Point = mk()` reported "cannot assign Point to Point",
	// and the newtype analogue made a newtype unusable across any call boundary.
	return tc.resolveTypeIfKnown(lambda.ReturnType.Type, lambda.GetLocation())
}

// inferLambdaCallFromType validates a call against a LambdaType (used for
// member-expression callees and any future callable-type sites.
func (tc *TypeChecker) inferLambdaCallFromType(calleeName string, lambdaType *types.LambdaType, call *ast.FunctionCallExpr) types.Type {
	required := 0
	for _, p := range lambdaType.Parameters {
		if p.DefaultValue == nil {
			required++
		}
	}
	total := len(lambdaType.Parameters)
	got := len(call.Arguments)

	if got < required || got > total {
		if required == total {
			tc.addError(call.GetLocation(), SeverityError,
				"%s: expected %d argument(s), got %d", calleeName, total, got)
		} else {
			tc.addError(call.GetLocation(), SeverityError,
				"%s: expected %d to %d argument(s), got %d",
				calleeName, required, total, got)
		}
		return tc.resolveTypeIfKnown(lambdaType.ReturnType.Type, call.GetLocation())
	}

	for i, arg := range call.Arguments {
		param := lambdaType.Parameters[i]
		if param.Type == nil {
			continue
		}
		argType := tc.inferExprType(arg)
		if argType == nil {
			continue
		}
		if !isAssignable(argType, param.Type) {
			tc.addError(arg.GetLocation(), SeverityError,
				"%s: argument %d: cannot assign %s to %s",
				calleeName, i+1, argType, param.Type)
		}
	}

	return tc.resolveTypeIfKnown(lambdaType.ReturnType.Type, call.GetLocation())
}

// inferFunctionCallExpr checks argument count and types at a call site and
// returns the callee's declared return type (or nil when the callee is unknown
// or has no declared return type). Handles identifier callees, direct lambda
// expressions, member-expression callees (method calls), and fully-qualified
// trait-method-path callees (TraitName::method(...)).
func (tc *TypeChecker) inferFunctionCallExpr(call *ast.FunctionCallExpr) types.Type {
	// Defensive: a malformed callee (e.g. `f.()` — a member expression with no
	// property) can collect to a nil expression on some paths; the collector emits
	// its own diagnostic, so return without dereferencing it (calling GetName on a
	// nil callee in the default arm below crashed lyrac check).
	if call.Function == nil {
		return nil
	}
	switch callee := call.Function.(type) {
	case *ast.IdentifierExpr:
		return tc.inferIdentifierCall(callee, call)
	case *ast.LambdaExpr:
		return tc.inferDirectLambdaCall(callee, call)
	case *ast.MemberExpr:
		return tc.inferMemberCall(callee, call)
	case *ast.TraitMethodPathExpr:
		return tc.inferTraitMethodPathCall(callee, call)
	default:
		// Any other expression is callable when it *evaluates to* a function —
		// `fs[1](5)` on an array of closures, or a call returning one. The value's
		// LambdaType carries the full signature, so the call is checked against it
		// exactly as a call through a function-typed parameter is.
		if calleeType := tc.inferExprType(call.Function); calleeType != nil {
			if lt, ok := calleeType.(*types.LambdaType); ok {
				return tc.inferLambdaCallFromType(call.Function.GetName(), lt, call)
			}
		}
		tc.addError(call.GetLocation(), SeverityError,
			"cannot call %s expression", call.Function.GetName())
		return nil
	}
}

// inferTraitMethodPathCall type-checks a fully-qualified trait method call
// (`Show::show(n)`). Unlike a `.`-call, the receiver is an ordinary first
// element of call.Arguments (there is no object to imply it), so its type
// drives dispatch and the whole parameter list — including Self — lines up
// directly against call.Arguments.
func (tc *TypeChecker) inferTraitMethodPathCall(path *ast.TraitMethodPathExpr, call *ast.FunctionCallExpr) types.Type {
	if _, ok := tc.symTable.LookupTraitFrom(path.TraitName, path.GetLocation()); !ok {
		tc.addError(call.GetLocation(), SeverityError, "unknown trait %q", path.TraitName)
		return nil
	}
	if len(call.Arguments) == 0 {
		tc.addError(call.GetLocation(), SeverityError,
			"%s::%s: expected a receiver argument", path.TraitName, path.Method.Name)
		return nil
	}
	receiverType := tc.inferExprType(call.Arguments[0])
	if receiverType == nil {
		return nil
	}
	receiverType = tc.resolveType(receiverType, call.Arguments[0].GetLocation())

	matches := tc.resolveTraitMethod(receiverType, path.Method.Name, path.TraitName)
	if len(matches) == 0 {
		tc.addError(call.GetLocation(), SeverityError,
			"no implementation of %s::%s for %s", path.TraitName, path.Method.Name, receiverType)
		return nil
	}
	// requiredTrait already narrows resolveTraitMethod to one trait, and a
	// well-formed program has at most one impl of a given trait for a given
	// concrete type, so matches[0] is unambiguous here.
	qualifiedName := path.TraitName + "::" + path.Method.Name
	return tc.inferResolvedTraitMethodCall(qualifiedName, matches[0], call, nil)
}

// inferIdentifierCall resolves the identifier in scope and validates the call
// against a LambdaExpr found as a VarDeclStmt value.
func (tc *TypeChecker) inferIdentifierCall(ident *ast.IdentifierExpr, call *ast.FunctionCallExpr) types.Type {
	// A parameter shadows any outer binding of the same name (mirroring the
	// IdentifierExpr resolution in inferExprType, which consults paramTypes
	// first). A function-typed parameter is callable — `f(x)` where
	// `f: (u8) -> u8` — so validate the call against its lambda-type signature.
	if tc.paramTypes != nil {
		if pt, ok := tc.paramTypes[ident.Name]; ok {
			if lt, ok := pt.(*types.LambdaType); ok {
				// Record the callee's own type: a call through a function *value* is
				// lowered as an indirect call, and the signature to call through comes
				// from this node's recorded type. Nothing else infers a callee
				// identifier — the resolution above is structural, not by inference.
				tc.typeTable.Set(ident, lt)
				return tc.inferLambdaCallFromType(ident.Name, lt, call)
			}
			tc.addError(call.GetLocation(), SeverityError,
				"identifier %q is not callable (type %s)", ident.Name, pt)
			return nil
		}
	}

	sym, ok := tc.scope.Lookup(ident.Name)
	if !ok {
		// A compiler-provided free function (print/println) — consulted only after
		// scope resolution misses, so a user binding of the same name shadows it.
		if isBuiltinPrintFn(ident.Name) {
			return tc.inferPrintCall(ident.Name, call)
		}
		if isBuiltinPanicFn(ident.Name) {
			return tc.inferPanicCall(call)
		}
		// A name that exists but belongs privately to another module gets the
		// privacy diagnostic rather than "undefined": the distinction between "no
		// such function" and "not yours to call" is the whole point of the rule.
		if v := tc.visibilityOf(ident.Name); v.found && !v.isPublic {
			tc.checkVisible(v, call.GetLocation())
			return nil
		}
		tc.addError(call.GetLocation(), SeverityError, "undefined function %q", ident.Name)
		return nil
	}
	// No visibility check on a *successful* lookup: scoping now enforces it
	// structurally. A private declaration lives only in its own module's scope, so a
	// name that resolves is either this module's own or one the global scope holds —
	// and the global scope holds only exports.
	//
	// Checking anyway was actively wrong, because it asked whether *some* declaration
	// of that name is private (via a name-keyed module map that is last-writer-wins)
	// rather than whether *the declaration this reference resolved to* is visible. Two
	// modules each declaring `helper` made one module's call to its own function report
	// the other module's privacy.
	// An overloaded name: several declarations telling themselves apart by the type of
	// their `self` receiver, which at this point is argument 0 whether the call was
	// written `f(m, x)` or desugared from `m.f(x)`.
	if set, ok := sym.(*ast.OverloadSet); ok {
		return tc.inferOverloadedCall(set, call)
	}
	if lambda, ok := sym.(*ast.LambdaExpr); ok {
		return tc.inferLambdaCall(ident.Name, tc.bareCalleeFor(ident.Name, lambda, call), call)
	}
	if decl, ok := sym.(*ast.VarDeclStmt); ok {
		lambda, ok := decl.Value.(*ast.LambdaExpr)
		if !ok {
			// Not a literal lambda, but the binding may still hold one: `let add5 =
			// makeAdder(5)` binds a closure returned by a call. Its declared type is
			// then a LambdaType, which carries everything a call site needs, so check
			// against the signature rather than a body that isn't here.
			declValType := tc.inferExprType(decl.Value)
			if lt, ok := declValType.(*types.LambdaType); ok {
				tc.typeTable.Set(ident, lt) // the indirect call's signature (see above)
				return tc.inferLambdaCallFromType(ident.Name, lt, call)
			}
			if declValType == nil {
				// The initializer has no type *yet*. The reachable way to get here is a
				// definition cycle — `let f = f(1)`, or the mutual `let a = b(1); let b
				// = a(1)` — which the guard in inferExprType breaks by returning nil
				// rather than recursing until the process dies. Say that, instead of
				// formatting the nil through `%s` and emitting the Go verb error
				// `identifier "f" is not callable (type %!s(<nil>))`, which is what this
				// line did and which tells the reader nothing about the actual mistake.
				tc.addError(call.GetLocation(), SeverityError,
					"cannot infer the type of %q: its definition depends on itself. Break the cycle, or annotate it",
					ident.Name)
				return nil
			}
			tc.addError(call.GetLocation(), SeverityError, "identifier %q is not callable (type %s)", ident.Name, declValType)
			return nil
		}
		// Record the callee's signature: a *local* binding of a lambda is a closure
		// value, and calling it lowers as an indirect call through this node's type.
		// The signature is built from the declaration alone (lambdaSignature), never
		// by inferring the lambda as an expression — that would re-check a body
		// checkVarDecl has already checked.
		// The callee may not be the one the scope chain found: a `self` function that
		// does not take this receiver gives way to a reachable one that does (see
		// bareCalleeFor). The recorded signature must be the chosen callee's, since an
		// indirect call lowers through it.
		lambda = tc.bareCalleeFor(ident.Name, lambda, call)
		tc.typeTable.Set(ident, tc.lambdaSignature(lambda))
		return tc.inferLambdaCall(ident.Name, lambda, call)
	}
	// sym is some other Named (e.g. Parameter) — fall through to lambda call
	if lambda, ok := sym.(*ast.LambdaExpr); ok {
		return tc.inferLambdaCall(ident.Name, lambda, call)
	}
	tc.addError(call.GetLocation(), SeverityError, "cannot resolve function %q", ident.Name)
	return nil
}

// inferPrintCall type-checks a print/println call: exactly one argument of a
// printable type (string, an integer, a float, bool, or rune), result void. An
// untyped numeric literal argument (`print(5)`) is settled to its default width
// (i64/f64) so the backend has a concrete type to format. Unlike an ordinary
// function, print is polymorphic over the printable types — the backend picks
// the formatting per the argument's type — so it isn't expressed as a single
// LambdaType signature.
func (tc *TypeChecker) inferPrintCall(name string, call *ast.FunctionCallExpr) types.Type {
	if len(call.Arguments) != 1 {
		tc.addError(call.GetLocation(), SeverityError,
			"%s: expected 1 argument(s), got %d", name, len(call.Arguments))
		return types.VoidType{}
	}
	arg := call.Arguments[0]
	argType := tc.inferExprType(arg)
	if argType == nil {
		return types.VoidType{}
	}
	if !isPrintableType(argType) {
		tc.addError(arg.GetLocation(), SeverityError,
			"%s: cannot print a value of type %s (expected a string, an integer, a float, bool, or rune)",
			name, argType)
		return types.VoidType{}
	}
	// Settle an untyped numeric literal to its default width so the backend reads a
	// concrete type (a non-literal or already-concrete arg is left unchanged).
	if types.IsNumeric(argType) {
		tc.propagateLiteralType(arg, promoteToDefault(argType))
	}
	return types.VoidType{}
}

// inferPanicCall type-checks `panic(msg)`: exactly one `string` argument, and a
// result of `never` (types.NeverType), which is assignable to anything — so the
// call is legal wherever a value of any type is expected, and legal as a statement.
//
// The message is a runtime `string`, not a literal: an interpolated message
// ("index ${i} out of range") is the case that makes a panic worth writing at all,
// and restricting it to a literal would rule that out. The backend's existing traps
// bake a constant message into a cached function; this one passes the fat pointer
// through (see panicMessageFunc).
//
// It returns NeverType even on the error paths below, so one mistake produces one
// diagnostic: handing back nil would make every construct downstream report the
// missing type as a second, less specific error.
func (tc *TypeChecker) inferPanicCall(call *ast.FunctionCallExpr) types.Type {
	if len(call.Arguments) != 1 {
		tc.addError(call.GetLocation(), SeverityError,
			"panic: expected 1 argument(s), got %d", len(call.Arguments))
		return types.NeverType{}
	}
	arg := call.Arguments[0]
	if argType := tc.inferExprType(arg); argType != nil && !types.IsString(argType) {
		tc.addError(arg.GetLocation(), SeverityError,
			"panic: message must be a string, got %s", promoteToDefault(argType))
	}
	return types.NeverType{}
}

// inferDirectLambdaCall type-checks a call where the callee is a bare lambda
// expression, e.g. ((n: int) -> int => n * 2)(5).
func (tc *TypeChecker) inferDirectLambdaCall(lambda *ast.LambdaExpr, call *ast.FunctionCallExpr) types.Type {
	return tc.inferLambdaCall("lambda", lambda, call)
}

// inferMemberCall type-checks a call where the callee is a member expression,
// e.g. obj.method(args). A struct field holding a callable (function) value
// is checked first — same priority as plain (non-call) member access via
// inferMemberExprType — falling back to trait-method dispatch only when no
// such field exists. This can't simply delegate to inferMemberExprType (as it
// did before trait dispatch existed): that function emits a "has no field"
// error itself when the lookup misses, which would be wrong/duplicated for a
// name that turns out to resolve via a trait instead.
func (tc *TypeChecker) inferMemberCall(member *ast.MemberExpr, call *ast.FunctionCallExpr) types.Type {
	// `math.double(21)` — a call through an imported namespace resolves to the
	// module's function, then goes through the ordinary call path so arguments and
	// generics are checked exactly as a direct call would be.
	//
	// That means calling inferLambdaCall against the *declaration*, the same entry point
	// inferIdentifierCall uses, rather than checking against the declared signature: a
	// generic callee's type variables are free until this call's arguments solve them, so
	// the signature route rejected every generic namespace call ("cannot assign
	// Maybe<i64> to Maybe<t>") and, when it did not, recorded no instantiation for the
	// backend to emit. A member that is not a function (a type) keeps the type route.
	if t, fn, handled := tc.moduleMemberType(member); handled {
		if fn != nil {
			return tc.inferLambdaCall(member.Property.Name, fn, call)
		}
		if lt, ok := t.(*types.LambdaType); ok {
			return tc.inferLambdaCallFromType(member.Property.Name, lt, call)
		}
		return nil
	}
	objType := tc.inferExprType(member.Object)
	if objType == nil {
		return nil
	}
	objType = tc.resolveType(objType, member.Object.GetLocation())
	methodName := member.Property.Name

	// A struct field holding a lambda is looked up on the (generic-substituted)
	// struct; trait dispatch below keeps the original objType, which for a
	// generic receiver is the ParameterizedType still carrying the type
	// arguments the unifier needs.
	if f, ok := structFieldByName(tc.resolveGenericAggregate(objType, member.Object.GetLocation()), methodName); ok {
		tc.typeTable.Set(member, f.Type)
		if lambdaType, ok := f.Type.(*types.LambdaType); ok {
			return tc.inferLambdaCallFromType(methodName, lambdaType, call)
		}
		tc.addError(call.GetLocation(), SeverityError,
			"member %q is not callable (type %s)", methodName, f.Type)
		return nil
	}

	if matches := tc.resolveTraitMethod(objType, methodName, ""); len(matches) > 0 {
		if len(matches) > 1 {
			tc.addError(call.GetLocation(), SeverityError,
				"call to %q is ambiguous between traits %s; use TraitName::%s(...) to disambiguate",
				methodName, traitNamesOf(matches), methodName)
			return nil
		}
		return tc.inferResolvedTraitMethodCall(methodName, matches[0], call, member.Object)
	}

	// The receiver's type may be a bare type parameter (`self.value` of type `t`
	// inside a generic impl body). It has no concrete impl, but a `where t: Trait`
	// bound in scope lets a method the bound's trait declares be dispatched
	// against that trait's signature (Self = the parameter).
	if g, ok := objType.(types.GenericType); ok {
		if ret, ok := tc.dispatchViaGenericBound(g, methodName, call); ok {
			return ret
		}
		tc.addError(member.GetLocation(), SeverityError,
			"type parameter %s has no method %q; add a `where %s: Trait` bound whose trait declares it",
			g.Name, methodName, g.Name)
		return nil
	}

	// UFCS: a free function that opted in by naming its first parameter `self`
	// (typechecker_ufcs.go). Below a real trait impl, so an impl always wins; above
	// the builtins, so a `self` function may shadow one — matching the rule that user
	// code beats a compiler-provided method. The call is rewritten to pass the
	// receiver as its first argument and then checked as the direct call it now is.
	//
	// Against the *declaration*, not a signature — the same reason the namespace path
	// at the top of this function gives: a generic callee's type variables are free
	// until this call's arguments solve them, and solving is also what records the
	// specialization the backend emits. With the receiver prepended, a `Maybe<i64>`
	// receiver binds `t` exactly as it does when the call is written `map(m, f)`.
	if fn, res := tc.ufcsCandidate(objType, methodName, member, call); res != ufcsNoMatch {
		if res == ufcsRefused {
			return nil
		}
		desugarUFCSCall(member, call)
		return tc.inferLambdaCall(methodName, fn, call)
	}

	// A compiler-provided method on a primitive receiver (e.g. `x.wrapping_add(y)`
	// on an integer). Checked last so a user type or trait impl of the same name
	// always takes priority (see builtins.go).
	if sig, ok := builtinMethodSignature(objType, methodName); ok {
		tc.typeTable.Set(member, sig)
		return tc.inferLambdaCallFromType(methodName, sig, call)
	}

	// A free function of this name that *nearly* matched — one that never opted in, or
	// one this file has not imported — is the likeliest thing the author meant, so the
	// "no such member" message says so rather than leaving them hunting for a method.
	hint := tc.ufcsHint(methodName, member.GetLocation())
	switch t := objType.(type) {
	case types.NamedStructType:
		tc.addError(member.GetLocation(), SeverityError, "%s has no field or method %q%s", t.Name, methodName, hint)
	case types.AnonymousStructType:
		tc.addError(member.GetLocation(), SeverityError, "anonymous struct has no field %q%s", methodName, hint)
	case types.PrimitiveType:
		// A primitive is a valid method receiver (see the builtin methods above),
		// so report the missing method rather than "non-struct type".
		tc.addError(member.GetLocation(), SeverityError, "%s has no method %q%s", t, methodName, hint)
	default:
		if objType != nil {
			tc.addError(member.GetLocation(), SeverityError, "member access on non-struct type %s%s", objType, hint)
		}
	}
	return nil
}

// checkMutArgument verifies that an argument passed to a `mut` parameter can
// actually be mutated by the callee.
//
// A `mut` parameter is a *mutable borrow* — the callee writes through to the
// caller's own storage (the backend passes it by reference) — so the argument must
// be two things:
//
//   - an **lvalue**: a binding, or a field/element path rooted at one. A temporary
//     (`f(Point { x: 1 })`, `f(make())`, a literal) has no storage the caller can
//     observe afterwards, so mutating it is meaningless; before this check such a
//     call compiled and silently discarded the writes.
//   - rooted at a **mutable** binding, by the same rule that governs writing to it
//     directly (rootBindingIsMutable). Passing a deeply-immutable `let` to a `mut`
//     parameter was accepted and then mutated it, which is the mutability system
//     being bypassed by a function call.
//
// A copied scalar is exempt: `mut` there is inert (lyra-W010 says so, and the
// backend keeps it by value via types.IsCopiedScalar), so nothing is written
// through and an ordinary value argument is fine.
func (tc *TypeChecker) checkMutArgument(calleeName string, position int, paramName string, arg ast.Expression, paramType types.Type) {
	if types.IsCopiedScalar(paramType) {
		return
	}
	root := rootIdentifier(arg)
	if root == nil {
		tc.addError(arg.GetLocation(), SeverityError,
			"%s: argument %d (%s): a `mut` parameter mutates the caller's value, so the argument must be a variable or a field/element of one, not a temporary",
			calleeName, position, paramName)
		return
	}
	if !tc.rootBindingIsMutable(root) {
		tc.addError(arg.GetLocation(), SeverityError,
			"%s: argument %d (%s): cannot pass immutable binding %q to a `mut` parameter (declare it `var`, or take the parameter by value)",
			calleeName, position, paramName, root.Name)
	}
}

// checkExclusiveMutableBorrow rejects passing the same binding to a `mut`
// parameter and to any other parameter of the same call.
//
// `mut` and `ref` are both lowered as pointers to the caller's storage, so two
// arguments naming one binding are two views of the same memory. If either is
// `mut`, the callee's writes are visible through the other view mid-call, and
// which value it observes depends on statement order inside a function the caller
// can't see. `both(p, p)` with `(a: ref Pt, b: mut Pt)` reads 1 or 99 purely by
// where `a.x` sits relative to `b.x = 99`, and `two(p, p)` with two `mut`
// parameters lets each write clobber the other's.
//
// The rule is Rust's: a mutable borrow is *exclusive*. Enforcing it here is what
// keeps by-reference lowering from being observable — a `ref` may see the caller's
// live value rather than a snapshot only when nothing can mutate it during the
// call. Lyra has no general borrow checker, so this is deliberately narrow: it
// compares argument *roots* within one call, which is exactly the aliasing that
// by-reference parameter passing introduces. Two `ref` arguments naming one
// binding are fine — neither can write.
//
// Scalars are exempt for the same reason they are passed by value
// (types.IsCopiedScalar): there is no shared storage to alias.
func (tc *TypeChecker) checkExclusiveMutableBorrow(calleeName string, lambda *ast.LambdaExpr, call *ast.FunctionCallExpr) {
	type argRoot struct {
		name     string
		position int
		isMut    bool
	}
	var roots []argRoot
	for i, arg := range call.Arguments {
		if i >= len(lambda.Parameters) {
			break
		}
		param := lambda.Parameters[i]
		if param.Type == nil || types.IsCopiedScalar(tc.resolveType(param.Type, param.GetLocation())) {
			continue
		}
		mode := param.TypeModifier
		if mode != types.Mut && mode != types.Ref {
			continue // `own` transfers a copy; a bare parameter is a value
		}
		root := rootIdentifier(arg)
		if root == nil {
			continue // a temporary has no storage to alias
		}
		roots = append(roots, argRoot{name: root.Name, position: i + 1, isMut: mode == types.Mut})
	}
	for i, a := range roots {
		for _, b := range roots[i+1:] {
			if a.name != b.name || (!a.isMut && !b.isMut) {
				continue
			}
			mutPos, otherPos := a.position, b.position
			if !a.isMut {
				mutPos, otherPos = b.position, a.position
			}
			tc.addError(call.GetLocation(), SeverityError,
				"%s: %q is passed to argument %d as `mut` and also to argument %d — a `mut` borrow is exclusive, so no other argument of the same call may name it",
				calleeName, a.name, mutPos, otherPos)
			return
		}
	}
}
