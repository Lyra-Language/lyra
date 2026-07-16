package collector_test

import (
	"testing"

	"github.com/Lyra-Language/lyra/pkg/ast"
)

// `Pt { … }` and `Node { … }` are syntactically identical (both parse to a
// DataPattern with an inner StructPattern payload), so the collector's
// reclassifyStructPatterns pass splits them by consulting the symbol table: a
// name that is a declared struct type becomes a named StructPattern; a data
// constructor's name stays a DataPattern.

// firstArmPattern returns the first match-arm pattern of the top-level function
// named `f`.
func firstArmPattern(t *testing.T, program *ast.Program) ast.Pattern {
	t.Helper()
	for _, stmt := range program.Statements {
		vd, ok := stmt.(*ast.VarDeclStmt)
		if !ok || vd.Name != "f" {
			continue
		}
		lam, ok := vd.Value.(*ast.LambdaExpr)
		if !ok {
			continue
		}
		m, ok := lam.Body.(*ast.MatchExpr)
		if !ok {
			continue
		}
		return m.MatchArms[0].Pattern
	}
	t.Fatal("could not find function f's match")
	return nil
}

func TestReclassify_StructNamedPatternBecomesStructPattern(t *testing.T) {
	program, _, _, errs := parseAndCollect(t, `
struct Pt {
  x: u8,
  y: u8,
}
let f = (p: Pt) -> u8 => match p {
  Pt { x, y } => x,
  _ => 0,
}
`)
	if len(errs) > 0 {
		t.Fatalf("collector errors: %v", errs)
	}
	pat := firstArmPattern(t, program)
	sp, ok := pat.(*ast.StructPattern)
	if !ok {
		t.Fatalf("Pt { x, y } should reclassify to *ast.StructPattern, got %T", pat)
	}
	if sp.Name != "Pt" {
		t.Errorf("reclassified struct pattern Name = %q, want %q", sp.Name, "Pt")
	}
}

func TestReclassify_DataConstructorPatternStaysDataPattern(t *testing.T) {
	program, _, _, errs := parseAndCollect(t, `
data Tree = Leaf | Node { l: u8, r: u8 }
let f = (t: Tree) -> u8 => match t {
  Node { l, r } => l,
  _ => 0,
}
`)
	if len(errs) > 0 {
		t.Fatalf("collector errors: %v", errs)
	}
	pat := firstArmPattern(t, program)
	if _, ok := pat.(*ast.DataPattern); !ok {
		t.Fatalf("Node { l, r } should stay *ast.DataPattern, got %T", pat)
	}
}
