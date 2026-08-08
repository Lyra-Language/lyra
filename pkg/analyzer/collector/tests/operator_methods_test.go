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

// Arithmetic **dispatches** as of 08/07, so it draws nothing at all: `a + b` on a type
// with a `(_+_)` impl calls it. This test inverted on that day — it used to assert the
// W015 warning, which was the holding position while nothing consumed the syntax.
func TestOperatorMethod_ArithmeticIsDispatchedAndSilent(t *testing.T) {
	ds := diagnosticsOf(t, `
	trait Arith {
		(_+_): (Self, Self) -> Self,
		(_*_): (Self, Self) -> Self,
		(_&_): (Self, Self) -> Self,
		(-_): (Self) -> Self,
		(~_): (Self) -> Self
	}
	`)
	if len(ds) != 0 {
		t.Errorf("a dispatched operator method must draw no diagnostic, got: %v", ds)
	}
}

// The inert group still warns, and each says *why* — the reasons differ, and "nothing
// calls it" left the author unable to tell "wait for it" from "this can never work".
func TestOperatorMethod_InertOperatorsWarnWithTheirReason(t *testing.T) {
	for _, tc := range []struct{ decl, want string }{
		{"(_&&_): (Self, Self) -> Self", "short-circuit"},
		{"(_**_): (Self, Self) -> Self", "no `**` operator"},
		{"(!_): (Self) -> Self", "boolean negation"},
		{"(_++): (Self) -> Self", "no suffix"},
	} {
		ds := diagnosticsOf(t, "\n\ttrait T { "+tc.decl+" }\n\t")
		if !hasCode(ds, diag.CodeInertOperatorMethod) {
			t.Errorf("%s: expected the inert warning, got: %v", tc.decl, ds)
			continue
		}
		found := false
		for _, d := range ds {
			if strings.Contains(d.Message, tc.want) {
				found = true
			}
		}
		if !found {
			t.Errorf("%s: the warning must say why (%q), got: %v", tc.decl, tc.want, ds)
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
