package checker_test

import (
	"strings"
	"testing"

	"github.com/Lyra-Language/lyra/pkg/analyzer/checker"
	"github.com/Lyra-Language/lyra/pkg/analyzer/collector"
	"github.com/Lyra-Language/lyra/pkg/analyzer/typechecker"
	diag "github.com/Lyra-Language/lyra/pkg/diagnostic"
	"github.com/Lyra-Language/lyra/pkg/parser"
	"github.com/Lyra-Language/lyra/pkg/typetable"
)

// The accept-set was established by **running the backend** on each shape before this
// pass existed, not by reading its code — `lowerElseDestructuring` decides on the lowered
// block's terminator, which is not a property the AST wears on its face. The one shape the
// two disagree on is recorded below with its reason.
func letElseDiags(t *testing.T, elseBody string) []diag.Diagnostic {
	t.Helper()
	source := `
data Maybe<t> = None | Some(t)
let opt = pure () -> Maybe<i64> => Some(1)
let main = () -> void => {
  let Some(v) = opt() else { ` + elseBody + ` }
  println("${v}")
}
`
	tree, err := parser.Parse(source)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	c := collector.NewCollector([]byte(source))
	program, symTable, scopeTable, _ := c.Collect(tree.RootNode())
	tt := typetable.New()
	typechecker.New(symTable, scopeTable, tt).Check(program)
	out := checker.CheckLetElseDiverges(program, tt)
	for _, d := range out {
		if d.Code != diag.CodeLetElseMustDiverge {
			t.Fatalf("unexpected code %q", d.Code)
		}
		if d.Severity != diag.SeverityError {
			t.Fatalf("lyra-E074 must be an error, got %v", d.Severity)
		}
	}
	return out
}

func TestLetElseDiverges_Accepted(t *testing.T) {
	for _, c := range []struct{ name, body string }{
		{"return", `return`},
		{"panic", `panic("no")`},
		{"a statement then return", `println("logging"); return`},
		{"nested block", `{ return }`},
		{"match, every arm diverges", `match 1 { 0 => return, _ => return }`},
		{"match, every arm panics", `match 1 { 0 => panic("a"), _ => panic("b") }`},
		// Accepted here and refused by the backend, deliberately: a front-end error
		// says "not a legal program", and this one is legal. The backend's `if`
		// lowering leaves the merge block without a terminator — its gap, in todo.md.
		{"if, both branches diverge (backend still refuses)", `if true { return } else { return }`},
	} {
		t.Run(c.name, func(t *testing.T) {
			if d := letElseDiags(t, c.body); len(d) != 0 {
				t.Errorf("expected no diagnostic for a diverging else, got %v", d)
			}
		})
	}
}

func TestLetElseDiverges_Rejected(t *testing.T) {
	for _, c := range []struct{ name, body string }{
		{"empty", ``},
		{"a plain call", `println("no match")`},
		{"if with no else", `if true { return }`},
		{"if with one diverging branch", `if true { return } else { println("x") }`},
		{"match with one non-diverging arm", `match 1 { 0 => return, _ => println("x") }`},
		// A function that panics internally is still declared `-> void`, so nothing at
		// the call site says it diverges. The backend refuses this too.
		{"a call to a function that panics inside", `bail()`},
	} {
		t.Run(c.name, func(t *testing.T) {
			// `bail` is declared for every case; only the last one calls it.
			d := letElseDiagsWithPrelude(t, c.body, `let bail = () -> void => panic("bail")`)
			if len(d) != 1 {
				t.Fatalf("expected one diagnostic for a fall-through else, got %d: %v", len(d), d)
			}
		})
	}
}

// letElseDiagsWithPrelude is letElseDiags with an extra top-level declaration.
func letElseDiagsWithPrelude(t *testing.T, elseBody, extra string) []diag.Diagnostic {
	t.Helper()
	source := `
data Maybe<t> = None | Some(t)
let opt = pure () -> Maybe<i64> => Some(1)
` + extra + `
let main = () -> void => {
  let Some(v) = opt() else { ` + elseBody + ` }
  println("${v}")
}
`
	tree, err := parser.Parse(source)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	c := collector.NewCollector([]byte(source))
	program, symTable, scopeTable, _ := c.Collect(tree.RootNode())
	tt := typetable.New()
	typechecker.New(symTable, scopeTable, tt).Check(program)
	return checker.CheckLetElseDiverges(program, tt)
}

// `break` and `continue` diverge only where they are legal, which is inside a loop — and
// that is the shape a `let … else` in a loop body actually takes.
func TestLetElseDiverges_BreakInALoopIsAccepted(t *testing.T) {
	source := `
data Maybe<t> = None | Some(t)
let opt = pure () -> Maybe<i64> => Some(1)
let main = () -> void => {
  for i in 0..<3 {
    let Some(v) = opt() else { break }
    println("${v}")
  }
}
`
	tree, err := parser.Parse(source)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	c := collector.NewCollector([]byte(source))
	program, symTable, scopeTable, _ := c.Collect(tree.RootNode())
	tt := typetable.New()
	typechecker.New(symTable, scopeTable, tt).Check(program)
	if d := checker.CheckLetElseDiverges(program, tt); len(d) != 0 {
		t.Errorf("`break` inside a loop diverges; got %v", d)
	}
}

// An `if let` is a different form with branch-scoped bindings, so nothing here applies to
// it — a non-diverging else is exactly what it is for.
func TestLetElseDiverges_IfLetIsUntouched(t *testing.T) {
	source := `
data Maybe<t> = None | Some(t)
let opt = pure () -> Maybe<i64> => Some(1)
let main = () -> void => {
  if let Some(v) = opt() { println("${v}") } else { println("none") }
}
`
	tree, err := parser.Parse(source)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	c := collector.NewCollector([]byte(source))
	program, symTable, scopeTable, _ := c.Collect(tree.RootNode())
	tt := typetable.New()
	typechecker.New(symTable, scopeTable, tt).Check(program)
	if d := checker.CheckLetElseDiverges(program, tt); len(d) != 0 {
		t.Errorf("`if let` scopes its bindings to a branch and needs no divergence; got %v", d)
	}
}

// The message must name the fix, and the *other form* — reaching for `let … else` when
// `if let` was meant is the likely mistake behind a non-diverging else.
func TestLetElseDiverges_MessageNamesBothFixes(t *testing.T) {
	d := letElseDiags(t, `println("no match")`)
	if len(d) != 1 {
		t.Fatalf("want one diagnostic, got %v", d)
	}
	for _, want := range []string{"return", "panic(…)", "if let"} {
		if !strings.Contains(d[0].Message, want) {
			t.Errorf("message should mention %q; got: %s", want, d[0].Message)
		}
	}
}
