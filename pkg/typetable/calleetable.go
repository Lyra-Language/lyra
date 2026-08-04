package typetable

import "github.com/Lyra-Language/lyra/pkg/ast"

// The resolved callee of a direct call, recorded by the typechecker for the passes that
// run after it.
//
// It exists for **receiver-keyed overloading**, where a name no longer determines a
// declaration: `unwrap_or` may be several functions, told apart by the receiver's type.
// Three passes after the typechecker resolve a callee in order to read its parameter
// modes — the ownership pass (to decide where a reference is retained), the
// use-after-move check (to decide whether an argument was consumed), and the backend (to
// find the function to call) — and each did it by looking the name up in the symbol
// table. That question has no answer for an overloaded name.
//
// So the pass that *did* resolve it publishes the answer, rather than three passes
// re-deriving it from a receiver type they would each have to recover. This is the
// MethodTable's arrangement for trait dispatch, applied to the same problem one rung
// down; the note on MethodTable makes the same argument.
//
// **Only an overloaded call is recorded.** A singly-declared name still resolves by
// lookup, so filling this in for every call would be a second, redundant answer to a
// question the symbol table already answers — and a second answer is a thing that can
// disagree. A consumer therefore reads this *first* and falls back to lookup, which is
// exactly the order that makes an overloaded call correct and leaves every other call
// behaving as it did.
// SetCallee records the declaration a call resolved to.
func (t *TypeTable) SetCallee(call *ast.FunctionCallExpr, fn *ast.LambdaExpr) {
	if t == nil {
		return
	}
	if t.callees == nil {
		t.callees = make(map[*ast.FunctionCallExpr]*ast.LambdaExpr)
	}
	t.callees[call] = fn
}

// Callee returns the declaration a call resolved to, when the typechecker had to choose
// between several. Nil-receiver-safe, so a consumer running without a typechecker pass
// (a test, a tool) sees "not recorded" rather than crashing — the same courtesy
// MethodTable.Get extends.
func (t *TypeTable) Callee(call *ast.FunctionCallExpr) (*ast.LambdaExpr, bool) {
	if t == nil || t.callees == nil {
		return nil, false
	}
	fn, ok := t.callees[call]
	return fn, ok
}
