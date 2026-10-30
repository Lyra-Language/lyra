package checker

import (
	"fmt"

	"github.com/Lyra-Language/lyra/pkg/ast"
	diag "github.com/Lyra-Language/lyra/pkg/diagnostic"
	"github.com/Lyra-Language/lyra/pkg/types"
	"github.com/Lyra-Language/lyra/pkg/typetable"
)

// largeByValueBytes is where a parameter passed by value starts to cost enough to say so:
// clippy's `large_types_passed_by_value` default, and well under what found this — a
// Genesis bus of several kilobytes, copied on every memory read, two thirds of Sheliak's
// frame (09/28). A copy below it is a few cache lines.
const largeByValueBytes = 256

// CheckLargeByValueParams warns about a function taking a large aggregate by a bare
// parameter (`lyra-W031`): every call copies it — a large one is passed as a pointer to
// a copy the caller makes (backend/llvm/sret.go) — and nothing in the source says so.
// `ref` lends the caller's own instead, and costs nothing else: a bare parameter is
// read-only already (lyra-E025), and a `ref` one may be read, copied, stored, captured,
// returned and passed to a bare or `own` parameter as a bare one may.
//
// Where `ref` would not compile, nothing is said: a nested lambda (lyra-E082 refuses one
// with a by-reference parameter) and a function used as a value anywhere in the program,
// matched by name — conservative where two functions share one. Nor where a borrow is not
// safe — a generator or `async` function, whose frame outlives the call lending it — nor
// for an extern, whose convention is C's, or a parameter whose size depends on a type
// variable. A trait method's signature is the trait's, so an impl is not walked.
//
// size answers what a type written at a location takes by value
// (TypeChecker.ByValueSize).
func CheckLargeByValueParams(program *ast.Program, tt *typetable.TypeTable, size func(types.Type, ast.Location) (int, bool)) []diag.Diagnostic {
	values := functionValueNames(program, tt)
	var out []diag.Diagnostic
	for _, node := range program.Statements {
		decl, ok := node.(*ast.VarDeclStmt)
		if !ok {
			continue
		}
		lam, ok := decl.Value.(*ast.LambdaExpr)
		if !ok || lam.IsExtern || lam.IsGenerator || lam.IsAsync || values[decl.Name] {
			continue
		}
		for i := range lam.Parameters {
			p := &lam.Parameters[i]
			ip, named := p.Pattern.(*ast.IdentifierPattern)
			if p.TypeModifier != "" || !named || p.Type == nil {
				continue
			}
			vars := map[string]bool{}
			types.CollectTypeVars(p.Type, vars)
			if len(vars) > 0 {
				continue
			}
			bytes, ok := size(p.Type, p.GetLocation())
			if !ok || bytes < largeByValueBytes {
				continue
			}
			written := types.Source(p.Type)
			// An `impl` block's `self` takes its type from the block (lyra-E089), and the
			// block is erased before this pass, so both spellings are given.
			fix := fmt.Sprintf("`%s: ref %s`", ip.Name, written)
			if ip.Name == "self" {
				fix = fmt.Sprintf("`self: ref` (`self: ref %s` outside an `impl` block)", written)
			}
			out = append(out, diag.Diagnostic{
				Location: p.GetLocation(),
				Severity: diag.SeverityWarning,
				Code:     diag.CodeLargeByValueParam,
				Message: fmt.Sprintf(
					"parameter %q is a %d-byte `%s`, copied at every call to %q — take it by reference, %s: a bare parameter is read-only already, so `ref` changes nothing but the copy",
					ip.Name, bytes, written, decl.Name, fix),
			})
		}
	}
	return out
}

// functionValueNames are the names of functions the program uses as values — a name
// whose recorded type is a function, anywhere but a call's callee — which lyra-E082
// would refuse a by-reference parameter on.
func functionValueNames(program *ast.Program, tt *typetable.TypeTable) map[string]bool {
	names := map[string]bool{}
	callees := map[ast.Expression]bool{}
	isFunction := func(e ast.Expression) bool {
		t, ok := tt.Get(e)
		if !ok {
			return false
		}
		_, isLambda := t.(*types.LambdaType)
		return isLambda
	}
	visit := func(e ast.Expression) bool {
		switch ex := e.(type) {
		case *ast.FunctionCallExpr:
			callees[ex.Function] = true
		case *ast.IdentifierExpr:
			if !callees[ex] && isFunction(ex) {
				names[ex.Name] = true
			}
		case *ast.MemberExpr:
			// `lib.f` passed on, through a namespace.
			if !callees[ex] && isFunction(ex) {
				names[ex.Property.Name] = true
			}
		}
		return true
	}
	for _, node := range program.Statements {
		if stmt, ok := node.(ast.Statement); ok {
			ast.WalkStmt(stmt, nil, visit)
		}
	}
	return names
}
