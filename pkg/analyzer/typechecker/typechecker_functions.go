package typechecker

import (
	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/types"
)

// withParamScope temporarily installs a parameter-type map for the duration of
// fn. This lets inferExprType resolve identifiers that name the lambda's
// parameters. Only IdentifierPattern parameters with a declared type annotation
// are added; complex-pattern or unannotated parameters are silently skipped.
// Nested calls (e.g. a lambda inside a lambda) correctly save and restore.
//
// Each parameter's declared type is resolved through resolveType so that
// user-defined names (constrained types, structs, etc.) are expanded and
// unknown names emit an "unknown type" diagnostic.
func (tc *TypeChecker) withParamScope(lambda *ast.LambdaExpr, fn func()) {
	old := tc.paramTypes
	tc.paramTypes = make(map[string]types.Type, len(lambda.Parameters))
	for _, p := range lambda.Parameters {
		if ip, ok := p.Pattern.(*ast.IdentifierPattern); ok && p.Type != nil {
			tc.paramTypes[ip.Name] = tc.resolveType(p.Type, p.GetLocation())
		}
	}
	tc.enterScope(lambda, fn)
	tc.paramTypes = old
}

// checkLambdaBody verifies that the lambda's body is consistent with the
// declared return type. It is a no-op when no return type is declared.
//
// For void functions it checks that no explicit `return <expr>` appears.
// For typed functions it checks that the body expression / every explicit
// return statement / the implicit last expression all match the declared type.
func (tc *TypeChecker) checkLambdaBody(funcName string, lambda *ast.LambdaExpr) {
	declaredReturn := lambda.ReturnType.Type
	// Always enter the param scope: withParamScope resolves every parameter
	// type annotation via resolveType, emitting "unknown type" for any name
	// that has no declaration. This must happen even when there is no return
	// type to check.
	tc.withParamScope(lambda, func() {
		if declaredReturn == nil {
			return // param annotations validated; nothing else to check
		}

		_, isVoid := declaredReturn.(types.VoidType)
		if lambda.Body != nil {
			if block, ok := lambda.Body.(*ast.BlockExpr); ok {
				if isVoid {
					tc.checkBlockVoidReturn(funcName, block)
				} else {
					tc.checkBlockReturn(funcName, block, declaredReturn)
				}
			} else if !isVoid {
				// Single-expression body: the expression value is the return value.
				// (For void functions the value is simply discarded — no error.)
				bodyType := tc.inferExprType(lambda.Body)
				if bodyType != nil && !isAssignable(bodyType, declaredReturn) {
					tc.addError(lambda.Body.GetLocation(), SeverityError,
						"%s: return type mismatch: expected %s, got %s",
						funcName, declaredReturn, bodyType)
				}
			}
		}

		// Multi-clause body: each clause body is the return value.
		// For void functions the value is discarded, so no check is needed.
		if !isVoid {
			for _, clause := range lambda.LambdaClauses {
				clauseType := tc.inferExprType(clause.Body)
				if clauseType != nil && !isAssignable(clauseType, declaredReturn) {
					tc.addError(clause.Body.GetLocation(), SeverityError,
						"%s: return type mismatch: expected %s, got %s",
						funcName, declaredReturn, clauseType)
				}
			}
		}
	})
}

// checkBlockVoidReturn walks block looking for explicit `return <expr>`
// statements, which are illegal in a void function. Bare `return` is allowed.
func (tc *TypeChecker) checkBlockVoidReturn(funcName string, block *ast.BlockExpr) {
	for _, stmt := range block.Statements {
		if ret, ok := stmt.(*ast.ReturnStmt); ok && ret.Value != nil {
			tc.addError(ret.GetLocation(), SeverityError,
				"%s: void function must not return a value", funcName)
		}
	}
}

// checkBlockReturn walks the statements in block, checking:
//   - Every explicit ReturnStmt against declaredReturn.
//   - The last statement, when it is an ExpressionStmt, as an implicit return value.
//
// The block's own scope is entered for the duration so that variables declared
// inside the body (e.g. `let local: i32 = 5`) are visible to inferExprType.
func (tc *TypeChecker) checkBlockReturn(funcName string, block *ast.BlockExpr, declaredReturn types.Type) {
	tc.enterScope(block, func() {
		stmts := block.Statements
		for i, stmt := range stmts {
			switch s := stmt.(type) {
			case *ast.ReturnStmt:
				if s.Value == nil {
					continue // bare return – void compatibility is not checked yet
				}
				retType := tc.inferExprType(s.Value)
				if retType != nil && !isAssignable(retType, declaredReturn) {
					tc.addError(s.GetLocation(), SeverityError,
						"%s: return type mismatch: expected %s, got %s",
						funcName, declaredReturn, retType)
				}
			case *ast.ExpressionStmt:
				if i == len(stmts)-1 {
					// The last expression in a block is its implicit return value.
					exprType := tc.inferExprType(s.Expression)
					if exprType != nil && !isAssignable(exprType, declaredReturn) {
						tc.addError(s.GetLocation(), SeverityError,
							"%s: return type mismatch: expected %s, got %s",
							funcName, declaredReturn, exprType)
					}
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
		return lambda.ReturnType.Type
	}

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
		if !isAssignable(argType, resolvedParamType) {
			paramName := param.Pattern.GetName()
			tc.addError(arg.GetLocation(), SeverityError,
				"%s: argument %d (%s): cannot assign %s to %s",
				calleeName, i+1, paramName, argType, param.Type)
		}
	}

	return lambda.ReturnType.Type
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
		return lambdaType.ReturnType.Type
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

	return lambdaType.ReturnType.Type
}

// inferFunctionCallExpr checks argument count and types at a call site and
// returns the callee's declared return type (or nil when the callee is unknown
// or has no declared return type). Handles identifier callees, direct lambda
// expressions, and member-expression callees (method calls).
func (tc *TypeChecker) inferFunctionCallExpr(call *ast.FunctionCallExpr) types.Type {
	switch callee := call.Function.(type) {
	case *ast.IdentifierExpr:
		return tc.inferIdentifierCall(callee, call)
	case *ast.LambdaExpr:
		return tc.inferDirectLambdaCall(callee, call)
	case *ast.MemberExpr:
		return tc.inferMemberCall(callee, call)
	default:
		tc.addError(call.GetLocation(), SeverityError,
			"cannot call %s expression", call.Function.GetName())
		return nil
	}
}

// inferIdentifierCall resolves the identifier in scope and validates the call
// against a LambdaExpr found as a VarDeclStmt value.
func (tc *TypeChecker) inferIdentifierCall(ident *ast.IdentifierExpr, call *ast.FunctionCallExpr) types.Type {
	sym, ok := tc.scope.Lookup(ident.Name)
	if !ok {
		tc.addError(call.GetLocation(), SeverityError, "undefined function %q", ident.Name)
		return nil
	}
	if lambda, ok := sym.(*ast.LambdaExpr); ok {
		return tc.inferLambdaCall(ident.Name, lambda, call)
	}
	if decl, ok := sym.(*ast.VarDeclStmt); ok {
		lambda, ok := decl.Value.(*ast.LambdaExpr)
		if !ok {
			declValType := tc.inferExprType(decl.Value)
			tc.addError(call.GetLocation(), SeverityError, "identifier %q is not callable (type %s)", ident.Name, declValType)
			return nil
		}
		return tc.inferLambdaCall(ident.Name, lambda, call)
	}
	// sym is some other Named (e.g. Parameter) — fall through to lambda call
	if lambda, ok := sym.(*ast.LambdaExpr); ok {
		return tc.inferLambdaCall(ident.Name, lambda, call)
	}
	tc.addError(call.GetLocation(), SeverityError, "cannot resolve function %q", ident.Name)
	return nil
}

// inferDirectLambdaCall type-checks a call where the callee is a bare lambda
// expression, e.g. ((n: int) -> int => n * 2)(5).
func (tc *TypeChecker) inferDirectLambdaCall(lambda *ast.LambdaExpr, call *ast.FunctionCallExpr) types.Type {
	return tc.inferLambdaCall("lambda", lambda, call)
}

// inferMemberCall type-checks a call where the callee is a member expression,
// e.g. obj.method(args). The member expression's own type inference (via
// inferMemberExprType) must first resolve to a callable type.
func (tc *TypeChecker) inferMemberCall(member *ast.MemberExpr, call *ast.FunctionCallExpr) types.Type {
	memberType := tc.inferExprType(member)
	if memberType == nil {
		return nil
	}
	// Check if the resolved member type is itself callable (a LambdaType).
	if lambdaType, ok := memberType.(*types.LambdaType); ok {
		return tc.inferLambdaCallFromType(member.Property.Name, lambdaType, call)
	}
	tc.addError(call.GetLocation(), SeverityError,
		"member %q is not callable (type %s)", member.Property.Name, memberType)
	return nil
}
