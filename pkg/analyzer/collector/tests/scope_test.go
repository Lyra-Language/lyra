package collector_test

import (
	"testing"

	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/ast/symbols"
)

// --- block scope ---

func TestScope_BlockScopeIsolation(t *testing.T) {
	// x is declared inside a block; it must not be visible at the file top level.
	_, table, _, _ := parseAndCollect(t, `
	let result = {
		let x = 42
		x
	}`)

	if _, ok := table.EntryScope().LookupLocal("x"); ok {
		t.Error("x should not be visible at the file top level")
	}
	if _, ok := table.EntryScope().LookupLocal("result"); !ok {
		t.Error("result should be visible at the file top level")
	}
}

func TestScope_NestedBlockScopes(t *testing.T) {
	// Inner block variables must not leak into the outer block.
	_, table, _, _ := parseAndCollect(t, `
	let outer = {
		let a = 1
		let inner = {
			let b = 2
			b
		}
		inner
	}`)

	if _, ok := table.EntryScope().LookupLocal("a"); ok {
		t.Error("a should not be at the file top level")
	}
	if _, ok := table.EntryScope().LookupLocal("b"); ok {
		t.Error("b should not be at the file top level")
	}
}

// --- function / lambda scope ---

func TestScope_LambdaParametersInFunctionScope(t *testing.T) {
	// Parameters must be registered in a child scope, not the file scope.
	_, table, _, _ := parseAndCollect(t, `let add = (x: i32, y: i32) -> i32 => x + y`)

	if _, ok := table.EntryScope().LookupLocal("x"); ok {
		t.Error("parameter x should not be at the file top level")
	}
	if _, ok := table.EntryScope().LookupLocal("y"); ok {
		t.Error("parameter y should not be at the file top level")
	}
	if _, ok := table.EntryScope().LookupLocal("add"); !ok {
		t.Error("add should be at the file top level")
	}
}

func TestScope_LambdaFunctionScope_ChildExists(t *testing.T) {
	// The file scope should have exactly one child scope for the lambda.
	_, table, _, _ := parseAndCollect(t, `let f = (a: i32) -> i32 => a * 2`)

	if len(table.EntryScope().Children) == 0 {
		t.Fatal("expected a child scope for the lambda function, got none")
	}
	functionScope := table.EntryScope().Children[0]
	if _, ok := functionScope.LookupLocal("a"); !ok {
		t.Error("parameter a should be registered in the function scope")
	}
}

// --- for loop scope ---

func TestScope_ForLoopInitVarInLoopScope(t *testing.T) {
	// The loop init variable must not be at the file top level.
	_, table, _, _ := parseAndCollect(t, `
	for let i = 0; i < 10; i + 1 {
		let x = i
	}`)

	if _, ok := table.EntryScope().LookupLocal("i"); ok {
		t.Error("loop variable i should not be at the file top level")
	}
	if _, ok := table.EntryScope().LookupLocal("x"); ok {
		t.Error("loop body variable x should not be at the file top level")
	}
}

func TestScope_ForLoopScopeHierarchy(t *testing.T) {
	// The loop scope should be a direct child of the file scope,
	// and the body block scope should be a child of the loop scope.
	_, table, _, _ := parseAndCollect(t, `
	for let i = 0; i < 10; i + 1 {
		let x = i
	}`)

	if len(table.EntryScope().Children) == 0 {
		t.Fatal("expected a child scope for the for loop")
	}
	loopScope := table.EntryScope().Children[0]
	// i is registered in the loop scope itself
	if _, ok := loopScope.LookupLocal("i"); !ok {
		t.Error("loop variable i should be in the loop scope")
	}
	// x is in the block scope nested inside the loop scope
	if len(loopScope.Children) == 0 {
		t.Fatal("expected a child block scope inside the loop scope")
	}
	blockScope := loopScope.Children[0]
	if _, ok := blockScope.LookupLocal("x"); !ok {
		t.Error("loop body variable x should be in the nested block scope")
	}
	// x is also visible from the block scope via Lookup (walks up to loop scope and beyond)
	if _, ok := blockScope.Lookup("i"); !ok {
		t.Error("loop variable i should be visible from the body block scope via Lookup")
	}
}

// --- for-in loop scope ---

func TestScope_ForInLoopVarsInLoopScope(t *testing.T) {
	// The iteration variable must not be at the file top level.
	_, table, _, _ := parseAndCollect(t, `
	for item in my_collection {
		let x = item
	}`)

	if _, ok := table.EntryScope().LookupLocal("item"); ok {
		t.Error("for-in variable item should not be at the file top level")
	}
	if _, ok := table.EntryScope().LookupLocal("x"); ok {
		t.Error("loop body variable x should not be at the file top level")
	}
}

func TestScope_ForInLoopKeyAndValueVars(t *testing.T) {
	// Both key and value iteration variables must be in the loop scope.
	_, table, _, _ := parseAndCollect(t, `
	for item, idx in [1, 2, 3] {
		let x = item
	}`)

	if len(table.EntryScope().Children) == 0 {
		t.Fatal("expected a child scope for the for-in loop")
	}
	loopScope := table.EntryScope().Children[0]
	if _, ok := loopScope.LookupLocal("item"); !ok {
		t.Error("item should be in the loop scope")
	}
	if _, ok := loopScope.LookupLocal("idx"); !ok {
		t.Error("idx should be in the loop scope")
	}
}

// --- with statement scope ---

func TestScope_WithNamedBindingInScope(t *testing.T) {
	// The named arena binding must be in a child scope, not the file scope.
	_, table, _, _ := parseAndCollect(t, `
	with frame = Arena.new(megabytes(4)) {
		let x = 1
	}`)

	if _, ok := table.EntryScope().LookupLocal("frame"); ok {
		t.Error("with binding frame should not be at the file top level")
	}
	if len(table.EntryScope().Children) == 0 {
		t.Fatal("expected a child scope for the with statement")
	}
	withScope := table.EntryScope().Children[0]
	if _, ok := withScope.LookupLocal("frame"); !ok {
		t.Error("frame should be registered in the with scope")
	}
}

func TestScope_WithAnonymousNoBinding(t *testing.T) {
	// An anonymous with statement should still push a scope for its body.
	_, table, _, _ := parseAndCollect(t, `
	with Arena.new(megabytes(4)) {
		let x = 1
	}`)

	if _, ok := table.EntryScope().LookupLocal("x"); ok {
		t.Error("body variable x should not be at the file top level")
	}
	if len(table.EntryScope().Children) == 0 {
		t.Fatal("expected a child scope for the anonymous with statement")
	}
}

// --- scope lookup walks parent chain ---

func TestScope_LookupWalksParentChain(t *testing.T) {
	// A global let binding should be visible from inside a nested block via Lookup.
	_, table, _, _ := parseAndCollect(t, `
	let global_val = 99
	let result = {
		let local_val = global_val
		local_val
	}`)

	// global_val should be at the file top level
	if _, ok := table.EntryScope().LookupLocal("global_val"); !ok {
		t.Error("global_val should be at the file top level")
	}
	// Lookup from a child scope should find global_val
	if len(table.EntryScope().Children) == 0 {
		t.Fatal("expected child scopes")
	}
	child := table.EntryScope().Children[0]
	if _, ok := child.Lookup("global_val"); !ok {
		t.Error("global_val should be visible from child scope via Lookup")
	}
}

// --- destructuring declarations register their bound names ---

// findDestructuredName walks the scope tree looking for name bound to a
// DestructuringDeclStmt (the placeholder the collector registers for a
// destructured binding).
func findDestructuredName(s *symbols.Scope, name string) bool {
	if sym, ok := s.Symbols[name]; ok {
		if _, isDecl := sym.(*ast.DestructuringDeclStmt); isDecl {
			return true
		}
	}
	for _, child := range s.Children {
		if findDestructuredName(child, name) {
			return true
		}
	}
	return false
}

func TestScope_TupleAndArrayDestructuringBindsNames(t *testing.T) {
	// Tuple and array destructuring register each bound name in the scope so
	// later references resolve. Each maps to the owning DestructuringDeclStmt
	// (which carries var/let mutability).
	_, table, _, _ := parseAndCollect(t, `
	let (a, b) = pair
	let [c, d] = arr`)

	for _, name := range []string{"a", "b", "c", "d"} {
		sym, ok := table.EntryScope().LookupLocal(name)
		if !ok {
			t.Errorf("destructured name %q should be registered in the scope", name)
			continue
		}
		if _, ok := sym.(*ast.DestructuringDeclStmt); !ok {
			t.Errorf("name %q should map to a *ast.DestructuringDeclStmt, got %T", name, sym)
		}
	}
}

func TestScope_StructDestructuringBindsNames(t *testing.T) {
	// Struct destructuring — shorthand `{x}` and rename `{y: b}` — registers its
	// bound names (the shorthand field name and the renamed local, respectively).
	_, table, _, _ := parseAndCollect(t, `
	struct Point { x: i64, y: i64 }
	let f = (p: Point) -> i64 => {
	    let {x, y: b} = p
	    x + b
	}`)

	for _, name := range []string{"x", "b"} {
		if !findDestructuredName(table.EntryScope(), name) {
			t.Errorf("struct-destructured name %q should be registered in some scope", name)
		}
	}
}

func TestScope_IfLetBindsNamesOnlyInThenBranch(t *testing.T) {
	// `if let [a,b,c] = arr { Then } else { Else }` scopes a/b/c to Then only
	// (mirroring Rust's if-let): visible inside Then, but not in Else (reaching
	// Else means the pattern failed to match) and not in the function scope
	// after the if-let statement.
	_, table, _, errs := parseAndCollect(t, `
	let f = (arr: [3]i64) -> i64 => {
	    if let [a, b, c] = arr {
	        a
	    } else {
	        0
	    }
	    a
	}`)
	if len(errs) != 0 {
		t.Fatalf("expected no collector errors, got %v", errs)
	}

	fnScope := table.EntryScope().Children[0]
	if _, ok := fnScope.LookupLocal("a"); ok {
		t.Error("a should not leak into the enclosing function scope")
	}
	if !findDestructuredName(table.EntryScope(), "a") {
		t.Error("a should be registered somewhere (Then's scope or its parent)")
	}
}

func TestScope_LetElseBindsNamesAfterTheStatement(t *testing.T) {
	// `let {x, y} = rec else { Else }` binds x/y for code following the
	// statement (like a plain `let`), but not inside the diverging Else branch.
	_, table, _, errs := parseAndCollect(t, `
	struct Point { x: i64, y: i64 }
	let f = (p: Point) -> i64 => {
	    let {x, y} = p else {
	        return 0
	    }
	    x + y
	}`)
	if len(errs) != 0 {
		t.Fatalf("expected no collector errors, got %v", errs)
	}

	if !findDestructuredName(table.EntryScope(), "x") {
		t.Error("x should be registered in the function scope after the let-else statement")
	}
	if !findDestructuredName(table.EntryScope(), "y") {
		t.Error("y should be registered in the function scope after the let-else statement")
	}
}

// --- top-level function registration (Functions) ---

func TestScope_TopLevelFunctionsRegistered(t *testing.T) {
	// Every top-level `let`/`var name = <lambda>` binding is registered in
	// SymbolTable.Functions.
	//
	// It also asserted a `PureFuncs` subset until 08/07, when a sweep for AST fields
	// nothing reads found that map written and never read — the purity pass its doc
	// comment named as the consumer had never consulted it. Both the map and these
	// assertions are gone: a test guarding dead state reports that the state is still
	// there, which is the opposite of useful.
	_, table, _, _ := parseAndCollect(t, `
	let explicitPure = pure (n: i64) -> i64 => n + 1
	let plain = (n: i64) -> i64 => n + 1`)

	if _, ok := table.Functions["explicitPure"]; !ok {
		t.Error("explicitPure should be registered in Functions")
	}
	if _, ok := table.Functions["plain"]; !ok {
		t.Error("plain should be registered in Functions")
	}
}

func TestScope_NestedFunctionsNotRegisteredAtTopLevel(t *testing.T) {
	// Functions is a flat, name-keyed map scoped to top-level bindings
	// only — a nested function (which could collide by name with an unrelated
	// top-level or sibling binding) is deliberately not registered here.
	_, table, _, _ := parseAndCollect(t, `
	let outer = () -> i64 => {
	    let nestedPure = pure (n: i64) -> i64 => n
	    0
	}`)

	if _, ok := table.Functions["nestedPure"]; ok {
		t.Error("nestedPure should not be registered in the top-level Functions map")
	}
}
