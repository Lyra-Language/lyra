package collector_test

import (
	"testing"

	diag "github.com/Lyra-Language/lyra/pkg/diagnostic"
)

// `@builtin(Ord)` / `@builtin(Eq)` on a trait (08/08) — the marker that confers
// compiler-known identity, exactly as `@builtin(Maybe)` does for a type.
//
// It matters because the compiler **owns the operators that dispatch to these two**:
// `<`/`<=`/`>`/`>=`/`<=>` derive from Ord's `compare` and `==`/`!=` are overridden by
// Eq's `eq`. Before the marker the *name* was the identity, so a program's own
// `trait Ord` was silently taken for the prelude's.

func TestCanonicalTrait_MarkerIsAccepted(t *testing.T) {
	ds := diagnosticsOf(t, `
	@builtin(Ord)
	trait Comparable { compare: (Self, Self) -> Ordering }
	`)
	for _, d := range ds {
		if d.Severity == diag.SeverityError {
			t.Errorf("a well-shaped marked trait must draw no error: %v", d)
		}
	}
}

// The shape gate: the marker is honored only if the trait declares the method the
// compiler is going to call.
func TestCanonicalTrait_WrongShapeIsRefused(t *testing.T) {
	ds := diagnosticsOf(t, `
	@builtin(Ord)
	trait Bad { f: (Self) -> i64 }
	`)
	if !hasCode(ds, diag.CodeMalformedBuiltin) {
		t.Errorf("a marked trait without `compare` must be refused, got: %v", ds)
	}
}

// A one-parameter `compare` is the near-miss worth catching: the method exists but
// cannot be called as `(Self, Self)`.
func TestCanonicalTrait_WrongArityIsRefused(t *testing.T) {
	ds := diagnosticsOf(t, `
	@builtin(Ord)
	trait Bad { compare: (Self) -> Ordering }
	`)
	if !hasCode(ds, diag.CodeMalformedBuiltin) {
		t.Errorf("a marked trait whose compare takes one parameter must be refused, got: %v", ds)
	}
}

func TestCanonicalTrait_UnknownKindIsRefused(t *testing.T) {
	ds := diagnosticsOf(t, `
	@builtin(Nope)
	trait T { f: (Self) -> i64 }
	`)
	if !hasCode(ds, diag.CodeMalformedBuiltin) {
		t.Errorf("an unknown `@builtin` trait kind must be refused, got: %v", ds)
	}
}

// Two claimants for one kind: the first well-shaped one wins and the rest are errors,
// the same rule the type marker follows.
func TestCanonicalTrait_DuplicateClaimIsRefused(t *testing.T) {
	ds := diagnosticsOf(t, `
	@builtin(Eq)
	trait A { eq: (Self, Self) -> bool }
	@builtin(Eq)
	trait B { eq: (Self, Self) -> bool }
	`)
	if !hasCode(ds, diag.CodeMalformedBuiltin) {
		t.Errorf("a duplicate `@builtin(Eq)` claim must be refused, got: %v", ds)
	}
}

// An unmarked trait draws nothing — the marker is opt-in, and the overwhelmingly
// common case is a trait that is not canonical at all.
func TestCanonicalTrait_UnmarkedTraitIsSilent(t *testing.T) {
	ds := diagnosticsOf(t, `
	trait Show { show: (Self) -> string }
	`)
	if len(ds) != 0 {
		t.Errorf("an unmarked trait must draw no diagnostic, got: %v", ds)
	}
}
