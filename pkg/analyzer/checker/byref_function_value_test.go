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

// byRefValueErrors runs what lyra-E082 needs — a referenced function's parameter modes come
// from its recorded type, so the typechecker has to have run — and returns the messages.
func byRefValueErrors(t *testing.T, source string) []string {
	t.Helper()
	tree, err := parser.Parse(source)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	c := collector.NewCollector([]byte(source))
	program, symTable, scopeTable, _ := c.Collect(tree.RootNode())
	tt := typetable.New()
	typechecker.New(symTable, scopeTable, tt).Check(program)
	var msgs []string
	for _, d := range checker.CheckByRefFunctionValues(program, tt) {
		if d.Code != diag.CodeByRefFunctionValue || d.Severity != diag.SeverityError {
			t.Fatalf("want lyra-E082 errors only, got %q (%v)", d.Code, d.Severity)
		}
		msgs = append(msgs, d.Message)
	}
	return msgs
}

// A named function with a `mut` parameter passed as a value compiled and **segfaulted**:
// the adapter a named function gets as a value passed the number where `inc` expected its
// address (09/28). Refused at `check` now, naming the function.
func TestCheck_ByRefFunctionPassedAsAValueIsRefused(t *testing.T) {
	msgs := byRefValueErrors(t, `
let inc = (k: mut i64) -> void => { k += 1 }
let apply = (f: (i64) -> void, n: i64) -> void => f(n)
let main = () -> void => {
  var n = 1
  apply(inc, n)
}
`)
	if len(msgs) != 1 || !strings.Contains(msgs[0], `"inc" takes a `+"`mut`"+` parameter`) {
		t.Fatalf("want one error naming inc, got %q", msgs)
	}
}

// A lambda written inside a function is a closure value even when only called by its own
// name, and the backend refused its `mut` parameter at `build` while `check` said nothing.
func TestCheck_NestedLambdaWithAMutParameterIsRefused(t *testing.T) {
	msgs := byRefValueErrors(t, `
let main = () -> void => {
  var n = 1
  let f = (k: mut i64) -> void => { k += 1 }
  f(n)
}
`)
	if len(msgs) != 1 || !strings.Contains(msgs[0], "a lambda written inside a function") {
		t.Fatalf("want one error at the lambda, got %q", msgs)
	}
}

// What stays legal: calling a by-reference function directly, a `ref` scalar (by value, so
// a function value can carry it), and ordinary function values and closures.
func TestCheck_ByRefFunctionsCalledDirectlyAndPlainValuesAreFine(t *testing.T) {
	msgs := byRefValueErrors(t, `
let inc = (k: mut i64) -> void => { k += 1 }
let peek = (k: ref i64) -> i64 => k
let apply = (f: (i64) -> i64, n: i64) -> i64 => f(n)
let main = () -> void => {
  var n = 1
  inc(n)
  let add = (x: i64) -> i64 => x + n
  println("${apply(peek, n)} ${apply(add, 2)}")
}
`)
	if len(msgs) != 0 {
		t.Fatalf("want no errors, got %q", msgs)
	}
}
