package checker_test

import (
	"strings"
	"testing"

	"github.com/Lyra-Language/lyra/pkg/analyzer/checker"
	"github.com/Lyra-Language/lyra/pkg/analyzer/collector"
	diag "github.com/Lyra-Language/lyra/pkg/diagnostic"
	"github.com/Lyra-Language/lyra/pkg/parser"
)

// checkGenericParams runs just the collector (the pass is AST-only, and runs
// before typechecking for the same reason) and returns its diagnostics.
func checkGenericParams(t *testing.T, source string) []diag.Diagnostic {
	t.Helper()
	tree, err := parser.Parse(source)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	program, _, _, collectErrs := collector.NewCollector([]byte(source)).Collect(tree.RootNode())
	if len(collectErrs) > 0 {
		t.Fatalf("collector errors: %v", collectErrs)
	}
	return checker.CheckGenericParams(program)
}

func errorsOnly(diags []diag.Diagnostic) []diag.Diagnostic {
	var out []diag.Diagnostic
	for _, d := range diags {
		if d.Severity == diag.SeverityError {
			out = append(out, d)
		}
	}
	return out
}

func warningsOnly(diags []diag.Diagnostic) []diag.Diagnostic {
	var out []diag.Diagnostic
	for _, d := range diags {
		if d.Severity == diag.SeverityWarning {
			out = append(out, d)
		}
	}
	return out
}

func assertNoGenericParamDiags(t *testing.T, source string) {
	t.Helper()
	if got := checkGenericParams(t, source); len(got) != 0 {
		t.Fatalf("expected no diagnostics, got %d: %v", len(got), got)
	}
}

// assertUndeclared asserts exactly the given type-variable names are reported as
// undeclared, and nothing else is reported as an error.
func assertUndeclared(t *testing.T, source string, names ...string) []diag.Diagnostic {
	t.Helper()
	got := errorsOnly(checkGenericParams(t, source))
	if len(got) != len(names) {
		t.Fatalf("expected %d undeclared-type-variable error(s), got %d: %v", len(names), len(got), got)
	}
	for i, d := range got {
		if d.Code != diag.CodeUndeclaredTypeVariable {
			t.Errorf("wrong code: got %q, want %q", d.Code, diag.CodeUndeclaredTypeVariable)
		}
		if !strings.Contains(d.Message, `"`+names[i]+`"`) {
			t.Errorf("message %d should name %q: %q", i, names[i], d.Message)
		}
	}
	return got
}

// assertUnused asserts exactly the given parameters are reported as unused.
func assertUnused(t *testing.T, source string, names ...string) []diag.Diagnostic {
	t.Helper()
	got := warningsOnly(checkGenericParams(t, source))
	if len(got) != len(names) {
		t.Fatalf("expected %d unused-type-parameter warning(s), got %d: %v", len(names), len(got), got)
	}
	for i, d := range got {
		if d.Code != diag.CodeUnusedTypeParameter {
			t.Errorf("wrong code: got %q, want %q", d.Code, diag.CodeUnusedTypeParameter)
		}
		if !strings.Contains(d.Message, `"`+names[i]+`"`) {
			t.Errorf("message %d should name %q: %q", i, names[i], d.Message)
		}
		if len(d.Tags) == 0 || d.Tags[0] != diag.TagUnnecessary {
			t.Errorf("unused type parameter should carry TagUnnecessary, got %v", d.Tags)
		}
	}
	return got
}

// --- the list stays optional: no list, no reconciliation ---

// The lexical rule is unchanged — a lowercase type name is a type variable
// wherever it appears — so a binding with no list is generic and legal. This is
// the case option (c) would have broken, and it is the reason (b) was chosen.
func TestGenericParams_NoListIsStillGeneric(t *testing.T) {
	assertNoGenericParamDiags(t, `let identity = (x: t) -> t => x`)
}

func TestGenericParams_NoListWithParameterizedType(t *testing.T) {
	assertNoGenericParamDiags(t, `let unbox = (b: Box<t>, fb: t) -> t => fb`)
}

// --- a written list that agrees with its signature ---

func TestGenericParams_ListMatchesSignature(t *testing.T) {
	assertNoGenericParamDiags(t, `let identity<t> = (x: t) -> t => x`)
}

func TestGenericParams_MultipleParams(t *testing.T) {
	assertNoGenericParamDiags(t, `let pair<t, u> = (a: t, b: u) -> t => a`)
}

// The prelude's shape, and the regression this pass is named for: `Maybe<t>`
// mentions `t` only in a type argument.
func TestGenericParams_ParameterizedTypeCounts(t *testing.T) {
	assertNoGenericParamDiags(t, `let is_some<t> = pure noalloc (m: Maybe<t>) -> bool => true`)
}

// A variable mentioned only through a callback's own signature is mentioned.
func TestGenericParams_LambdaTypeParamCounts(t *testing.T) {
	assertNoGenericParamDiags(t, `let apply<t, u> = (f: (t) -> u, x: t) -> u => f(x)`)
}

func TestGenericParams_ArrayAndTupleCount(t *testing.T) {
	assertNoGenericParamDiags(t, `let first<t> = (xs: []t) -> t => xs[0]`)
	assertNoGenericParamDiags(t, `let swap<t, u> = (p: (t, u)) -> (u, t) => (p.1, p.0)`)
}

// A variable that appears only in the return type still counts as mentioned.
func TestGenericParams_ReturnTypeOnly(t *testing.T) {
	assertNoGenericParamDiags(t, `let make<t> = (n: i64) -> Maybe<t> => None`)
}

// --- used but not declared: the error half (lyra-E031) ---

// The exact case from todo.md: declares `t`, is generic in `u`.
func TestGenericParams_UndeclaredVariable(t *testing.T) {
	got := assertUndeclared(t, `let mismatch<t> = (a: u) -> u => a`, "u")
	if !strings.Contains(got[0].Message, "<t, u>") {
		t.Errorf("message should show the fixed list `<t, u>`: %q", got[0].Message)
	}
}

// The typo this is really for: `Strng` would be an UnresolvedType and reported,
// but a misspelled *lowercase* name silently became a fresh type variable.
func TestGenericParams_TypoBecomesTypeVariable(t *testing.T) {
	assertUndeclared(t, `let count<t> = (xs: []t, sep: strng) -> i64 => 0`, "strng")
}

func TestGenericParams_UndeclaredInsideParameterizedType(t *testing.T) {
	assertUndeclared(t, `let get<t> = (m: Maybe<u>) -> t => panic("x")`, "u")
}

func TestGenericParams_UndeclaredInReturnType(t *testing.T) {
	assertUndeclared(t, `let make<t> = (x: t) -> u => panic("x")`, "u")
}

// Two undeclared variables report in a stable (sorted) order, not map order.
func TestGenericParams_MultipleUndeclaredAreSorted(t *testing.T) {
	for range 8 {
		assertUndeclared(t, `let f<t> = (a: b, c: d) -> t => panic("x")`, "b", "d")
	}
}

// The error points at the parameter that introduced the variable, not at the
// whole declaration — that is the edit site.
func TestGenericParams_ErrorPointsAtParameter(t *testing.T) {
	src := "let f<t> = (a: t,\n             b: u) -> t => a"
	got := assertUndeclared(t, src, "u")
	if got[0].Location.StartLine != 2 {
		t.Errorf("error should point at the offending parameter on line 2, got line %d", got[0].Location.StartLine)
	}
}

// --- declared but not mentioned: the warning half (lyra-W013) ---

func TestGenericParams_UnusedParameter(t *testing.T) {
	assertUnused(t, `let identity<t, u> = (x: t) -> t => x`, "u")
}

// The case the error half exists to make impossible to write by accident: a bound
// on a variable nothing solves constrains nothing at all.
func TestGenericParams_UnusedParameterWithBoundSaysSo(t *testing.T) {
	got := assertUnused(t, `let identity<t, u: Show> = (x: t) -> t => x`, "u")
	if !strings.Contains(got[0].Message, "constrains nothing") {
		t.Errorf("message should say the bound constrains nothing: %q", got[0].Message)
	}
	if !strings.Contains(got[0].Message, "Show") {
		t.Errorf("message should name the bound: %q", got[0].Message)
	}
}

// A `where`-written bound reaches the same place, since the collector merges it
// into the list.
func TestGenericParams_UnusedParameterWithWhereBound(t *testing.T) {
	got := assertUnused(t, `let identity<t, u> where u: Show = (x: t) -> t => x`, "u")
	if !strings.Contains(got[0].Message, "constrains nothing") {
		t.Errorf("message should say the bound constrains nothing: %q", got[0].Message)
	}
}

// The warning points at the entry in the `<…>` list.
func TestGenericParams_WarningPointsAtListEntry(t *testing.T) {
	got := assertUnused(t, `let identity<t, u> = (x: t) -> t => x`, "u")
	if got[0].Location.StartLine != 1 {
		t.Fatalf("expected the warning on line 1, got %d", got[0].Location.StartLine)
	}
	// `u` is the 17th column of the source above; the point is that it is inside
	// the list, well past the binding name.
	if got[0].Location.StartCol < 14 {
		t.Errorf("warning should point at the list entry, got col %d", got[0].Location.StartCol)
	}
}

// --- both halves at once ---

func TestGenericParams_UndeclaredAndUnusedTogether(t *testing.T) {
	src := `let mismatch<t> = (a: u) -> u => a`
	assertUndeclared(t, src, "u")
	assertUnused(t, src, "t")
}

// --- non-function bindings ---

// A generic list on a plain binding is reconciled against its annotation.
func TestGenericParams_AnnotationOnlyBinding(t *testing.T) {
	assertNoGenericParamDiags(t, `let empty<t>: Maybe<t> = None`)
	assertUndeclared(t, `let empty<t>: Maybe<u> = None`, "u")
}

// A list on a binding with no signature at all mentions nothing.
func TestGenericParams_ListOnNonGenericBinding(t *testing.T) {
	assertUnused(t, `let x<t> = 5`, "t")
}

// --- nested bindings ---

// `declaration` is an ordinary statement, so a generic function can be declared
// inside any block — and a nested one carries the same typo just as well.
func TestGenericParams_NestedBindingIsChecked(t *testing.T) {
	assertUndeclared(t, `let outer = () -> i64 => {
  let inner<t> = (a: u) -> u => a
  0
}`, "u")
}

func TestGenericParams_NestedBindingUnusedParam(t *testing.T) {
	assertUnused(t, `let outer = () -> i64 => {
  let inner<t, u> = (a: t) -> t => a
  0
}`, "u")
}

// --- what this pass deliberately does not touch ---

// A concrete (uppercase) type is not a type variable and never was: an unknown
// one is an UnresolvedType, reported by the typechecker at the declaration.
func TestGenericParams_UppercaseNamesAreNotVariables(t *testing.T) {
	assertNoGenericParamDiags(t, `let f<t> = (a: t, b: Point) -> t => a`)
}

// A nominal type's own parameters are bound by its declaration, not by a
// signature that merely mentions it — descending into it would make every
// function touching a generic type spuriously generic.
func TestGenericParams_NominalTypeParamsAreNotThisSignatures(t *testing.T) {
	assertNoGenericParamDiags(t, `struct Box<t> { v: t }
let unwrap = (b: Box<i64>) -> i64 => b.v`)
}
