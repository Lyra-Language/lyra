package collector_test

import (
	"strings"
	"testing"

	"github.com/Lyra-Language/lyra/pkg/analyzer/collector"
	diag "github.com/Lyra-Language/lyra/pkg/diagnostic"
	"github.com/Lyra-Language/lyra/pkg/parser"
)

// Operator-named trait methods — `(_==_)`, `(_+_)` — parse and collect, and **nothing
// dispatches to them**: every consumer filters on an identifier method name, so a
// `(_==_)` impl on a struct is never called and `==` keeps its built-in meaning.
// Verified 08/07; the fourth collected-and-unread surface found in two days, after
// `wallClock`, the `where` bounds and `@derive`.
//
// The split between refusing and warning is the design decision (todo.md): the
// compiler owns the seven comparison operators, and nothing owns arithmetic.

// Collects without the shared helper's fatal-on-error: these tests are *about* the
// diagnostics, so the run must survive producing them.
func diagnosticsOf(t *testing.T, source string) []diag.Diagnostic {
	t.Helper()
	tree, err := parser.Parse(source)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	c := collector.NewCollector([]byte(source))
	_, _, _, errs := c.Collect(tree.RootNode())
	var out []diag.Diagnostic
	for _, e := range errs {
		if d, ok := e.(diag.Diagnostic); ok {
			out = append(out, d)
		}
	}
	return out
}

func hasCode(ds []diag.Diagnostic, code string) bool {
	for _, d := range ds {
		if d.Code == code {
			return true
		}
	}
	return false
}

// `==`/`!=` are structural and overridden by the prelude's `Eq`; the ordering four and
// `<=>` all derive from `Ord::compare`. A second mechanism would be a coherence
// question with no answer, and declaring them one at a time reintroduces the
// `<`-disagrees-with-`<=>` failure Ord's single method exists to prevent.
func TestOperatorMethod_ComparisonsAreRefused(t *testing.T) {
	for op, wantTrait := range map[string]string{
		"==":  "Eq",
		"!=":  "Eq",
		"<":   "Ord",
		"<=":  "Ord",
		">":   "Ord",
		">=":  "Ord",
		"<=>": "Ord",
	} {
		ds := diagnosticsOf(t, "trait T {\n  (_"+op+"_): (Self, Self) -> bool\n}\n")
		if !hasCode(ds, diag.CodeComparisonOperatorMethod) {
			t.Errorf("(_%s_) must be refused, got: %v", op, ds)
			continue
		}
		// The message must name the trait that owns it — they are not all the same.
		if !strings.Contains(ds[0].Message, wantTrait) {
			t.Errorf("(_%s_) should point at %s, got: %s", op, wantTrait, ds[0].Message)
		}
	}
}

// An impl is reported too, not just the declaration: an impl may name a method the
// trait never declared, so an operator impl can exist with no operator declaration to
// report it.
func TestOperatorMethod_TheImplIsReportedToo(t *testing.T) {
	ds := diagnosticsOf(t, `
	trait MyEq { eq: (Self, Self) -> bool }
	impl MyEq for i64 {
		(_==_) = (a, b) => true
	}
	`)
	if !hasCode(ds, diag.CodeComparisonOperatorMethod) {
		t.Errorf("an operator-named method in an impl must be refused, got: %v", ds)
	}
}

// Arithmetic keeps the syntax — it has no canonical trait and no other design on the
// table, and `(_-_)` is load-bearing for a recorded hazard (`Empty - 1` parses as
// `Empty(-1)`, which only bites a data type overloading `-`). It warns rather than
// compiling silently to nothing.
func TestOperatorMethod_ArithmeticWarnsRatherThanRefusing(t *testing.T) {
	ds := diagnosticsOf(t, `
	trait Arith {
		(_+_): (Self, Self) -> Self,
		(_*_): (Self, Self) -> Self
	}
	`)
	if hasCode(ds, diag.CodeComparisonOperatorMethod) {
		t.Fatalf("arithmetic operator methods must not be refused: %v", ds)
	}
	if !hasCode(ds, diag.CodeInertOperatorMethod) {
		t.Errorf("an operator method nothing dispatches to must say so: %v", ds)
	}
	for _, d := range ds {
		if d.Severity == diag.SeverityError {
			t.Errorf("arithmetic operator methods must not be an error: %v", d)
		}
	}
}

// An ordinary identifier method draws nothing at all — the check must not fire on the
// normal case.
func TestOperatorMethod_IdentifierMethodsAreSilent(t *testing.T) {
	ds := diagnosticsOf(t, `
	trait Show { show: (Self) -> string }
	impl Show for i64 { show = (self) => "n" }
	`)
	if len(ds) != 0 {
		t.Errorf("identifier method names must draw no diagnostic, got: %v", ds)
	}
}
