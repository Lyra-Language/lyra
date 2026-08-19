package typechecker

import (
	"fmt"

	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/types"
)

// On-demand return inference: checking a top-level function early, because something
// needs its return type before the main pass would have reached it.
//
// Lyra has no declare-before-use requirement, and the house style puts helpers *below*
// the function that calls them — so an un-annotated helper is normally checked after its
// caller. That is fine almost everywhere, because a call's type can be deferred: the
// binding takes whatever the callee turns out to return.
//
// **A destructure is the one position that cannot defer.** `let (w, h) = viewport()`
// needs the element types where the pattern is walked, since each name's type comes from
// decomposing the value there and then; nothing later revisits it. So a helper declared
// below its caller and returning a tuple was refused (lyra-E058) with advice to annotate
// it — advice for a program written in the style this project documents.
//
// The fix is to check that declaration *now*, once, wherever it is first needed.
//
// # Two things it has to get right, and both fall out of memoizing the declaration
//
//   - **No double diagnostics.** The main pass reaches the same declaration later, and a
//     body checked twice reports everything twice. checkVarDecl consults `checked` and
//     returns, so "checked early" and "checked in order" are the same event.
//   - **Cycles terminate.** Two un-annotated functions that destructure each other's
//     results have no answer — computing either return type requires the other — so
//     `inferring` breaks the recursion and the caller falls back to lyra-E058, which asks
//     for the annotation that resolves it. That is the same shape resolveType's
//     `resolvingTypes` guard has, and the same honest answer self-recursion already gets.
//
// It deliberately does **not** try to infer every call's return type early. Only the
// destructure asks, because only the destructure cannot wait; making inference eager
// everywhere would be a different design (and a much larger blast radius) for a problem
// nothing else has.

// forceCheckFunction checks the top-level declaration of fn now, so its inferred return
// type is available. Reports whether the return type is known afterwards.
//
// A function that already has a return type — written or inferred — is answered
// immediately, which is the common case: the main pass reached it first.
func (tc *TypeChecker) forceCheckFunction(fn *ast.LambdaExpr) bool {
	if fn == nil {
		return false
	}
	if fn.ReturnType.Type != nil {
		return true
	}
	decl, ok := tc.declOfFunc[fn]
	if !ok {
		// Not a top-level declaration this pass owns — a trait-impl method, or a lambda
		// reached some other way. Nothing to hoist.
		return false
	}
	if tc.inferringRet[decl] || tc.checkedDecls[decl] {
		// Already in progress (a cycle) or already done and still without a type. Either
		// way this cannot answer, and the caller reports.
		return false
	}
	tc.inferringRet[decl] = true
	tc.atTopLevel(func() { tc.checkInModule(decl) })
	delete(tc.inferringRet, decl)
	return fn.ReturnType.Type != nil
}

// atTopLevel runs fn with the enclosing body's context cleared, and restores it after.
//
// **A hoisted declaration must be checked as if the pass had reached it in order** — from
// the top level, not from inside whatever body asked for it. The context that has to go is
// everything scoped to a body: the parameter scope, the enclosing return type and name,
// the `where` bounds in scope, and the impl/trait a method body is being checked inside.
//
// The parameter scope is the one that bites, and it does so silently. withParamScope
// *copies* the enclosing lambda's parameters into a nested one's, deliberately — a nested
// lambda is lexically inside its enclosing one and sees its parameters. A hoisted top-level
// function is not, so without this its body could resolve a name belonging to the caller's
// parameters: `let outer = (secret: i64) …` calling a `helper` whose body says `secret`
// type-checked clean, where checking in declaration order reports it undefined. A false
// *accept*, which is the direction that does not announce itself.
func (tc *TypeChecker) atTopLevel(fn func()) {
	oldTypes, oldMods, oldBound := tc.paramTypes, tc.paramMods, tc.patternBound
	oldRet, oldName := tc.enclosingRet, tc.enclosingFuncName
	oldBounds := tc.genericBounds
	oldMethod, oldImplType := tc.currentImplMethod, tc.currentImplType
	oldDefaultTrait := tc.currentDefaultTrait

	tc.paramTypes = map[string]types.Type{}
	tc.paramMods = map[string]types.TypeModifier{}
	tc.patternBound = map[string]bool{}
	tc.enclosingRet, tc.enclosingFuncName = nil, ""
	tc.genericBounds = map[string][]string{}
	tc.currentImplMethod, tc.currentImplType = ast.MethodName{}, nil
	tc.currentDefaultTrait = ""

	defer func() {
		tc.paramTypes, tc.paramMods, tc.patternBound = oldTypes, oldMods, oldBound
		tc.enclosingRet, tc.enclosingFuncName = oldRet, oldName
		tc.genericBounds = oldBounds
		tc.currentImplMethod, tc.currentImplType = oldMethod, oldImplType
		tc.currentDefaultTrait = oldDefaultTrait
	}()
	fn()
}

// markChecked records that a top-level declaration has been type-checked, so the main
// pass does not check it a second time. Returns true when it had already been checked.
func (tc *TypeChecker) markChecked(decl *ast.VarDeclStmt) bool {
	if tc.checkedDecls == nil {
		tc.checkedDecls = map[*ast.VarDeclStmt]bool{}
	}
	if tc.checkedDecls[decl] {
		return true
	}
	tc.checkedDecls[decl] = true
	return false
}

// collectTopLevelFuncs indexes each top-level function declaration by the lambda it
// binds, which is what lets a call site get from the callee (SymbolTable.Functions maps a
// name to the *lambda*) back to the declaration that has to be checked.
func (tc *TypeChecker) collectTopLevelFuncs(program *ast.Program) {
	tc.declOfFunc = map[*ast.LambdaExpr]*ast.VarDeclStmt{}
	tc.inferringRet = map[*ast.VarDeclStmt]bool{}
	tc.checkedDecls = map[*ast.VarDeclStmt]bool{}
	for _, stmt := range program.Statements {
		decl, ok := stmt.(*ast.VarDeclStmt)
		if !ok {
			continue
		}
		if lam, ok := decl.Value.(*ast.LambdaExpr); ok {
			tc.declOfFunc[lam] = decl
		}
	}
}

// forceCheckDestructureCallee checks, on demand, the function a destructuring's value
// calls — when that call is why the value has no type yet. Reports whether the callee now
// has a return type, i.e. whether retrying the destructure is worth anything.
//
// The shape it recognizes is deliberately the one lyra-E058 was written for: a
// destructuring pattern whose value is a call to a plain identifier naming a top-level
// function with no return annotation. Anything else has already been reported by whatever
// produced the nil, and re-running inference over it would at best change nothing.
func (tc *TypeChecker) forceCheckDestructureCallee(decl *ast.DestructuringDeclStmt) bool {
	if _, isIdent := decl.Pattern.(*ast.IdentifierPattern); isIdent {
		return false // a plain binding does not decompose, so it can wait
	}
	call, ok := decl.Value.(*ast.FunctionCallExpr)
	if !ok {
		return false
	}
	callee, ok := call.Function.(*ast.IdentifierExpr)
	if !ok {
		return false
	}
	fn, ok := tc.symTable.LookupFunctionFrom(callee.Name, decl.Value.GetLocation())
	if !ok {
		return false
	}
	return tc.forceCheckFunction(fn)
}

// unimportedHint names the module that exports a name the current file could not resolve,
// as a clause to append to an "undefined" diagnostic — or "" when nothing exports it.
//
// An import's member list restricts visibility as of 08/18, so "undefined" is now the
// wrong word for by far the commonest new failure: the name exists, it is `pub`, and this
// file simply did not ask for it. Saying so and naming the import is the difference
// between a one-line fix and a hunt.
func (tc *TypeChecker) unimportedHint(name string, loc ast.Location) string {
	if tc.symTable == nil {
		return ""
	}
	module, ok := tc.symTable.ExportingModule(name)
	if !ok || module == tc.symTable.ModuleOfFile[loc.File] {
		return ""
	}
	return fmt.Sprintf(" — module %q exports it, but this file does not import it; add `import %s.{ %s }`",
		module, module, name)
}
