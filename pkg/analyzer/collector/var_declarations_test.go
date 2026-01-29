package collector

import (
	"testing"

	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/parser"
	"github.com/Lyra-Language/lyra/pkg/types"
)

func TestCollector_VariableDeclaration(t *testing.T) {
	source := `let the_answer: Int = 42`

	tree, err := parser.Parse(source)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}

	collector := NewCollector([]byte(source))
	program, table, errors := collector.Collect(tree.RootNode())
	if len(errors) > 0 {
		t.Fatalf("Collector errors: %v", errors)
	}
	// program.Print("")

	// Check AST was built
	if len(program.Statements) != 1 {
		t.Fatalf("Expected 1 statement, got %d", len(program.Statements))
	}

	// Check symbol table lookup
	namedNode, ok := table.GlobalScope.Lookup("the_answer")
	if !ok {
		t.Fatalf("\"the_answer\" not found in global scope")
	}

	varDecl, ok := namedNode.(*ast.VarDeclStmt)
	if !ok {
		t.Fatalf("\"the_answer\" is not a VarDeclStmt, got %T", namedNode)
	}

	if !types.TypesEqual(varDecl.Type, intType) {
		t.Fatalf("\"the_answer\" type is not Int. Got %v", varDecl.Type)
	}

	// Note: Init expression type is not set during collection, only during type checking
	if varDecl.Value == nil {
		t.Fatalf("\"the_answer\" has no init value")
	}
}
