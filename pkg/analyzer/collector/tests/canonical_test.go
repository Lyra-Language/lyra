package collector_test

import (
	"testing"

	"github.com/Lyra-Language/lyra/pkg/ast/symbols"
)

// canonicalKindOf looks up a declared type by name and returns its stamped
// CanonicalKind ("Result"/"Maybe"/""), failing if the type isn't declared.
func canonicalKindOf(t *testing.T, table *symbols.SymbolTable, name string) string {
	t.Helper()
	decl, ok := table.Types[name]
	if !ok {
		t.Fatalf("type %q was not declared", name)
	}
	return decl.CanonicalKind
}

// A `data Result` with the canonical Ok/Err shape is stamped Result even with no
// `@builtin` marker — the legacy name-plus-shape fallback, which every program
// relies on today (there is no prelude).
func TestCanonical_UnmarkedResultByShape(t *testing.T) {
	_, table, _, _ := parseAndCollect(t, `data Result<t, e> = Ok t | Err e`)
	if got := canonicalKindOf(t, table, "Result"); got != "Result" {
		t.Errorf("CanonicalKind = %q, want %q", got, "Result")
	}
}

// A same-named but differently-shaped `data Result` (Foo/Bar, not Ok/Err) is not
// canonical: the fallback checks the shape, so it is left an ordinary type.
func TestCanonical_UnmarkedWrongShape_NotStamped(t *testing.T) {
	_, table, _, _ := parseAndCollect(t, `data Result<a, b> = Foo a | Bar b`)
	if got := canonicalKindOf(t, table, "Result"); got != "" {
		t.Errorf("CanonicalKind = %q, want empty (wrong shape must not be canonical)", got)
	}
}

// The `@builtin(Result)` marker confers identity independent of the type's name:
// a type named `Either` with the canonical shape becomes the canonical Result.
func TestCanonical_BuiltinMarker_NameIndependent(t *testing.T) {
	_, table, _, _ := parseAndCollect(t, `
@builtin(Result)
data Either<t, e> = Ok t | Err e`)
	if got := canonicalKindOf(t, table, "Either"); got != "Result" {
		t.Errorf("Either CanonicalKind = %q, want %q", got, "Result")
	}
}

// Once a kind is claimed by a marker, the bare name is no longer load-bearing:
// an unmarked `data Result` alongside an `@builtin(Result)`-marked `Either` is
// left an ordinary type.
func TestCanonical_MarkerSupersedesName(t *testing.T) {
	_, table, _, _ := parseAndCollect(t, `
@builtin(Result)
data Either<t, e> = Ok t | Err e
data Result<t, e> = Ok t | Err e`)
	if got := canonicalKindOf(t, table, "Either"); got != "Result" {
		t.Errorf("Either CanonicalKind = %q, want %q", got, "Result")
	}
	if got := canonicalKindOf(t, table, "Result"); got != "" {
		t.Errorf("bare Result CanonicalKind = %q, want empty (kind already claimed by Either)", got)
	}
}

// `@builtin(Maybe)` on a type whose shape isn't Some/None is rejected and not
// stamped — the marker cannot override the structural requirement.
func TestCanonical_BuiltinMarker_WrongShape_Errors(t *testing.T) {
	errs := parseAndCollectErrors(t, `
@builtin(Maybe)
data Opt<t> = Just t | Nope t`)
	assertCollectorErrorContains(t, errs, "`@builtin(Maybe)` requires the canonical Maybe shape")
}

// A `@builtin` naming something that isn't a canonical kind is a mistake.
func TestCanonical_BuiltinMarker_UnknownKind_Errors(t *testing.T) {
	errs := parseAndCollectErrors(t, `
@builtin(Frobnicate)
data Frob<t> = A t | B t`)
	assertCollectorErrorContains(t, errs, "unknown `@builtin` type")
}
