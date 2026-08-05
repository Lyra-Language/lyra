package llvm

import (
	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/llir/llvm/ir"
)

// Receiver-keyed overloading, the backend half.
//
// Two functions sharing a source name are two emitted functions, and everything here
// follows from the fact that **the by-name table cannot hold them both**. `l.funcs` is
// keyed by the name a call site writes, which is precisely the thing that no longer
// identifies a declaration once a name may mean several.
//
// So an overload is keyed by its *declaration* instead, and a call finds it through the
// resolution the typechecker recorded (typetable.Callee) rather than by looking its name
// up again. That is not a workaround for the map's shape — re-resolving here would mean
// re-deriving dispatch in the backend, from a receiver type it would have to recover, and
// getting a different answer from the front end is how a program silently calls the wrong
// function. The same argument the trait-method path already makes: dispatch is decided
// once, by the pass that has the types.
//
// The emitted *symbol* carries the receiver head (`lyra.std.prelude.unwrap_or$Maybe`),
// since the module-qualified name is shared by every member of the set and LLVM needs
// them distinct. The head is the same discriminant the front end refuses to let two
// members share, so the symbols cannot collide either.

// emitted is a declared function together with the parameters a call site needs in order
// to pass `mut`/`ref` arguments by reference.
//
// The pair travels together because splitting it is what went wrong for plain functions:
// `funcParams` was written under the module-qualified key and read under the bare name,
// so a private function's parameter modes came back empty and a `mut` argument was passed
// by value. One record with one key cannot drift that way.
type emitted struct {
	fn     *ir.Func
	params []ast.Parameter
}

// overloadHead returns the receiver head a declaration's symbol is qualified by, and
// whether the declaration is an overload at all.
//
// The symbol table is the authority on which names are overloaded — it did the merging
// and refused the overlaps — so the backend asks rather than re-deriving the rule from
// the shape of the declaration.
func (l *lowerer) overloadHead(decl *ast.VarDeclStmt) (string, bool) {
	if l.res == nil || l.res.SymbolTable == nil {
		return "", false
	}
	if _, overloaded := l.res.SymbolTable.OverloadSetFor(decl.Name, decl.GetLocation()); !overloaded {
		return "", false
	}
	head, reason := ast.ReceiverHead(decl)
	if reason != "" {
		// A member of an admitted set always has a head; this is unreachable short of
		// the two rules disagreeing, and silently emitting under the shared name would
		// be a duplicate-symbol error in clang rather than anything readable.
		return "", false
	}
	return head, true
}

// recordByDecl registers an emitted function under its declaration.
//
// Every user function is recorded, not only an overload. The by-name table is what a call
// normally resolves through, and it answers for the name as *this* call site sees it —
// which is not always the declaration the typechecker picked. A bare call whose name the
// scope chain resolved elsewhere (`receiverFallback`) reaches its callee only by identity,
// and that callee is often an ordinary singly-declared function in another module. Keying
// every function here costs one map entry and removes the distinction entirely.
func (l *lowerer) recordByDecl(fn *ast.LambdaExpr, declared *ir.Func, params []ast.Parameter) {
	l.overloads[fn] = emitted{fn: declared, params: params}
}

// resolvedCallee returns the emitted function a call was resolved to, when the typechecker
// recorded a callee for it — an overload member, or a declaration a bare call reached past
// the scope chain.
func (l *lowerer) resolvedCallee(e *ast.FunctionCallExpr) (emitted, bool) {
	if l.res == nil || l.res.TypeTable == nil {
		return emitted{}, false
	}
	lam, ok := l.res.TypeTable.Callee(e)
	if !ok {
		return emitted{}, false
	}
	got, ok := l.overloads[lam]
	return got, ok
}
