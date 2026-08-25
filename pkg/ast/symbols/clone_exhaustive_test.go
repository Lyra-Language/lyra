package symbols

import (
	"go/ast"
	"go/parser"
	"go/token"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// Every field of SymbolTable must be named in Clone's composite literal.
//
// This is the same shape as pkg/ast's exhaustive_test: the question is about *code* — does
// the clone mention this field — and reflection can say what fields exist but never what a
// function does with them, so the literal is parsed.
//
// It matters more here than a missing switch case usually does. A field left out of the
// clone is silently **shared** with the master, so the editor's next keystroke mutates state
// the following one inherits: analysis that drifts the longer a session runs, with no error
// anywhere. Adding a field to SymbolTable and forgetting the clone is a one-line change with
// that consequence, which is exactly the kind of omission a test should refuse to let pass.
func TestClone_MentionsEverySymbolTableField(t *testing.T) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "clone.go", nil, 0)
	if err != nil {
		t.Fatalf("parsing clone.go: %v", err)
	}

	// The keys of the &SymbolTable{...} literal inside Clone.
	assigned := map[string]bool{}
	ast.Inspect(f, func(n ast.Node) bool {
		lit, ok := n.(*ast.CompositeLit)
		if !ok {
			return true
		}
		id, ok := lit.Type.(*ast.Ident)
		if !ok || id.Name != "SymbolTable" {
			return true
		}
		for _, el := range lit.Elts {
			if kv, ok := el.(*ast.KeyValueExpr); ok {
				if k, ok := kv.Key.(*ast.Ident); ok {
					assigned[k.Name] = true
				}
			}
		}
		return true
	})
	if len(assigned) == 0 {
		t.Fatal("found no &SymbolTable{...} literal in clone.go — has Clone been rewritten? " +
			"This test reads that literal to know which fields the clone handles")
	}

	var missing []string
	ty := reflect.TypeOf(SymbolTable{})
	for i := 0; i < ty.NumField(); i++ {
		if name := ty.Field(i).Name; !assigned[name] {
			missing = append(missing, name)
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Errorf("SymbolTable.Clone does not mention: %s\n\n"+
			"A field left out is shared with the master rather than copied, so each keystroke "+
			"mutates state the next one inherits — analysis that drifts as a session goes on, "+
			"reported nowhere. Copy it (cloneMap for a map, append(nil, …) for a slice, "+
			"cloneScope for a scope) and add it to the literal.", strings.Join(missing, ", "))
	}
}
