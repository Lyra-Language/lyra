package collector_test

import (
	"testing"
)

// --- block scope ---

func TestScope_BlockScopeIsolation(t *testing.T) {
	// x is declared inside a block; it must not be visible in the global scope.
	_, table, _, _ := parseAndCollect(t, `
	let result = {
		let x = 42
		x
	}`)

	if _, ok := table.GlobalScope.LookupLocal("x"); ok {
		t.Error("x should not be visible in the global scope")
	}
	if _, ok := table.GlobalScope.LookupLocal("result"); !ok {
		t.Error("result should be visible in the global scope")
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

	if _, ok := table.GlobalScope.LookupLocal("a"); ok {
		t.Error("a should not be in the global scope")
	}
	if _, ok := table.GlobalScope.LookupLocal("b"); ok {
		t.Error("b should not be in the global scope")
	}
}

// --- function / lambda scope ---

func TestScope_LambdaParametersInFunctionScope(t *testing.T) {
	// Parameters must be registered in a child scope, not the global scope.
	_, table, _, _ := parseAndCollect(t, `let add = (x: i32, y: i32) -> i32 => x + y`)

	if _, ok := table.GlobalScope.LookupLocal("x"); ok {
		t.Error("parameter x should not be in the global scope")
	}
	if _, ok := table.GlobalScope.LookupLocal("y"); ok {
		t.Error("parameter y should not be in the global scope")
	}
	if _, ok := table.GlobalScope.LookupLocal("add"); !ok {
		t.Error("add should be in the global scope")
	}
}

func TestScope_LambdaFunctionScope_ChildExists(t *testing.T) {
	// The global scope should have exactly one child scope for the lambda.
	_, table, _, _ := parseAndCollect(t, `let f = (a: i32) -> i32 => a * 2`)

	if len(table.GlobalScope.Children) == 0 {
		t.Fatal("expected a child scope for the lambda function, got none")
	}
	functionScope := table.GlobalScope.Children[0]
	if _, ok := functionScope.LookupLocal("a"); !ok {
		t.Error("parameter a should be registered in the function scope")
	}
}

// --- for loop scope ---

func TestScope_ForLoopInitVarInLoopScope(t *testing.T) {
	// The loop init variable must not be in the global scope.
	_, table, _, _ := parseAndCollect(t, `
	for let i = 0; i < 10; i + 1 {
		let x = i
	}`)

	if _, ok := table.GlobalScope.LookupLocal("i"); ok {
		t.Error("loop variable i should not be in the global scope")
	}
	if _, ok := table.GlobalScope.LookupLocal("x"); ok {
		t.Error("loop body variable x should not be in the global scope")
	}
}

func TestScope_ForLoopScopeHierarchy(t *testing.T) {
	// The loop scope should be a direct child of the global scope,
	// and the body block scope should be a child of the loop scope.
	_, table, _, _ := parseAndCollect(t, `
	for let i = 0; i < 10; i + 1 {
		let x = i
	}`)

	if len(table.GlobalScope.Children) == 0 {
		t.Fatal("expected a child scope for the for loop")
	}
	loopScope := table.GlobalScope.Children[0]
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
	// The iteration variable must not be in the global scope.
	_, table, _, _ := parseAndCollect(t, `
	for item in my_collection {
		let x = item
	}`)

	if _, ok := table.GlobalScope.LookupLocal("item"); ok {
		t.Error("for-in variable item should not be in the global scope")
	}
	if _, ok := table.GlobalScope.LookupLocal("x"); ok {
		t.Error("loop body variable x should not be in the global scope")
	}
}

func TestScope_ForInLoopKeyAndValueVars(t *testing.T) {
	// Both key and value iteration variables must be in the loop scope.
	_, table, _, _ := parseAndCollect(t, `
	for item, idx in [1, 2, 3] {
		let x = item
	}`)

	if len(table.GlobalScope.Children) == 0 {
		t.Fatal("expected a child scope for the for-in loop")
	}
	loopScope := table.GlobalScope.Children[0]
	if _, ok := loopScope.LookupLocal("item"); !ok {
		t.Error("item should be in the loop scope")
	}
	if _, ok := loopScope.LookupLocal("idx"); !ok {
		t.Error("idx should be in the loop scope")
	}
}

// --- with statement scope ---

func TestScope_WithNamedBindingInScope(t *testing.T) {
	// The named arena binding must be in a child scope, not the global scope.
	_, table, _, _ := parseAndCollect(t, `
	with frame = Arena.new(megabytes(4)) {
		let x = 1
	}`)

	if _, ok := table.GlobalScope.LookupLocal("frame"); ok {
		t.Error("with binding frame should not be in the global scope")
	}
	if len(table.GlobalScope.Children) == 0 {
		t.Fatal("expected a child scope for the with statement")
	}
	withScope := table.GlobalScope.Children[0]
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

	if _, ok := table.GlobalScope.LookupLocal("x"); ok {
		t.Error("body variable x should not be in the global scope")
	}
	if len(table.GlobalScope.Children) == 0 {
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

	// global_val should be in the global scope
	if _, ok := table.GlobalScope.LookupLocal("global_val"); !ok {
		t.Error("global_val should be in the global scope")
	}
	// Lookup from a child scope should find global_val
	if len(table.GlobalScope.Children) == 0 {
		t.Fatal("expected child scopes")
	}
	child := table.GlobalScope.Children[0]
	if _, ok := child.Lookup("global_val"); !ok {
		t.Error("global_val should be visible from child scope via Lookup")
	}
}
