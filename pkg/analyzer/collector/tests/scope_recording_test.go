package collector_test

import (
	"testing"

	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/ast/symbols"
)

// findFirst walks the program and returns the first expression/statement for
// which pick returns a non-nil node, or nil if none matches.
func findScopeBearingNodes(program *ast.Program) (lambda, loop, forIn ast.Expression, with ast.Statement) {
	onStmt := func(s ast.Statement) bool {
		if w, ok := s.(*ast.WithStmt); ok && with == nil {
			with = w
		}
		return true
	}
	onExpr := func(e ast.Expression) bool {
		switch ex := e.(type) {
		case *ast.LambdaExpr:
			if lambda == nil {
				lambda = ex
			}
		case *ast.LoopExpr:
			if loop == nil {
				loop = ex
			}
		case *ast.ForInLoopExpr:
			if forIn == nil {
				forIn = ex
			}
		}
		return true
	}
	for _, node := range program.Statements {
		if stmt, ok := node.(ast.Statement); ok {
			ast.WalkStmt(stmt, onStmt, onExpr)
		}
	}
	return
}

// TestScopeRecording_LambdaLoopsWith verifies Phase 1 of the symbol-table-scope
// work: the collector now records a ScopeTable entry (of the right kind) for a
// lambda's parameter scope, both loop forms, and a `with` block — not just the
// block expressions and if-let statements recorded before. This is the node→scope
// mapping the typechecker's enterScope and (later) the purity checker consume.
func TestScopeRecording_LambdaLoopsWith(t *testing.T) {
	src := `
let f = (n: i64) -> i64 => {
    var total = 0
    while total < n {
        total += 1
    }
    for x in [1, 2, 3] {
        total += x
    }
    with a = Arena.new(bytes(64)) {
        total += 1
    }
    total
}`
	program, _, scopeTable, _ := parseAndCollect(t, src)
	lambda, loop, forIn, with := findScopeBearingNodes(program)

	cases := []struct {
		name string
		node ast.AstNode
		kind symbols.ScopeKind
	}{
		{"lambda", lambda, symbols.ScopeFunction},
		{"while-loop", loop, symbols.ScopeLoop},
		{"for-in-loop", forIn, symbols.ScopeLoop},
		{"with", with, symbols.ScopeBlock},
	}
	for _, tc := range cases {
		if tc.node == nil {
			t.Fatalf("%s node not found in AST", tc.name)
		}
		scope, ok := scopeTable.Get(tc.node)
		if !ok {
			t.Errorf("%s: no scope recorded", tc.name)
			continue
		}
		if scope.Kind != tc.kind {
			t.Errorf("%s: recorded scope kind = %d, want %d", tc.name, scope.Kind, tc.kind)
		}
	}
}

// TestScopeRecording_LoopVarInLoopScope verifies the recorded scope is the right
// one: a for-in loop's variable is registered in the loop scope, so it is
// findable there (and not leaked to the parent).
func TestScopeRecording_LoopVarInLoopScope(t *testing.T) {
	src := `
let f = (n: i64) -> i64 => {
    for i in 0..<n {
        i
    }
    0
}`
	program, _, scopeTable, _ := parseAndCollect(t, src)
	_, _, forIn, _ := findScopeBearingNodes(program)
	if forIn == nil {
		t.Fatal("for-in loop not found")
	}
	scope, ok := scopeTable.Get(forIn)
	if !ok {
		t.Fatal("no scope recorded for for-in loop")
	}
	if _, found := scope.LookupLocal("i"); !found {
		t.Errorf("loop variable `i` not registered in the loop scope")
	}
}
