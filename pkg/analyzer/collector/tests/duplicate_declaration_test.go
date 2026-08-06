package collector_test

import (
	"testing"

	diag "github.com/Lyra-Language/lyra/pkg/diagnostic"
)

// ---------------------------------------------------------------------------
// Same-scope sequential rebinding (allowed for let/var, ML-style)
// ---------------------------------------------------------------------------

// Sequential rebinding such as `let x = parse(x)` is idiomatic and safe in
// ML-family languages, so re-declaring a let/var in the same scope is allowed
// (no diagnostic). Only nested-scope shadowing is reported, by CheckShadowing.

func TestRebind_SameScope_TwoLet_NoError(t *testing.T) {
	errors := parseAndCollectErrors(t, `
		let x = 1
		let x = 2
	`)
	if len(errors) > 0 {
		t.Errorf("expected no errors for same-scope let rebinding, got: %v", errors)
	}
}

func TestRebind_SameScope_LetThenVar_NoError(t *testing.T) {
	errors := parseAndCollectErrors(t, `
		let x = 1
		var x = 2
	`)
	if len(errors) > 0 {
		t.Errorf("expected no errors for same-scope rebinding, got: %v", errors)
	}
}

func TestRebind_SameScope_VarThenLet_NoError(t *testing.T) {
	errors := parseAndCollectErrors(t, `
		var x = 1
		let x = 2
	`)
	if len(errors) > 0 {
		t.Errorf("expected no errors for same-scope rebinding, got: %v", errors)
	}
}

func TestRebind_SameScope_SequentialUsingPrior_NoError(t *testing.T) {
	// The motivating idiom: rebind a name using its own prior value.
	errors := parseAndCollectErrors(t, `
		let x = 1
		let x = x + 1
	`)
	if len(errors) > 0 {
		t.Errorf("expected no errors for `let x = x + 1`, got: %v", errors)
	}
}

// ---------------------------------------------------------------------------
// const may not be re-declared
// ---------------------------------------------------------------------------

// const names are SCREAMING_CASE and let/var names are lowercase, so a const can
// only ever collide with another const. const is immutable and may not be
// re-declared.
func TestDuplicate_SameScope_RedeclareConst(t *testing.T) {
	errors := parseAndCollectErrors(t, `
		const X = 1
		const X = 2
	`)
	assertCollectorErrorContains(t, errors, "cannot re-declare const X in this scope")
}

func TestDuplicate_ErrorMessageContainsOriginalLocation(t *testing.T) {
	// A const re-declaration error should carry RelatedInformation pointing to
	// line 1 (the original declaration).
	errors := parseAndCollectErrors(t, `const X = 1
const X = 2`)
	for _, e := range errors {
		d, ok := e.(diag.Diagnostic)
		if !ok {
			continue
		}
		if len(d.RelatedInformation) > 0 && d.RelatedInformation[0].Location.StartLine == 1 {
			return
		}
	}
	t.Errorf("expected a re-declaration diagnostic with RelatedInformation pointing to line 1, got: %v", errors)
}

// ---------------------------------------------------------------------------
// Different scopes — no error (shadowing is allowed)
// ---------------------------------------------------------------------------

func TestDuplicate_DifferentScopes_BlockShadowing_NoError(t *testing.T) {
	errors := parseAndCollectErrors(t, `
		let x = 1
		let y = {
			let x = 2
			x
		}
	`)
	if len(errors) > 0 {
		t.Errorf("expected no errors for shadowing in a nested block, got: %v", errors)
	}
}

func TestDuplicate_DifferentScopes_MultipleSiblingBlocks_NoError(t *testing.T) {
	// Two sibling blocks may both declare the same name independently.
	errors := parseAndCollectErrors(t, `
		let a = {
			let x = 1
			x
		}
		let b = {
			let x = 2
			x
		}
	`)
	if len(errors) > 0 {
		t.Errorf("expected no errors for same name in sibling blocks, got: %v", errors)
	}
}

// ---------------------------------------------------------------------------
// Distinct names — no error
// ---------------------------------------------------------------------------

func TestDuplicate_DistinctNames_NoError(t *testing.T) {
	errors := parseAndCollectErrors(t, `
		let x = 1
		let y = 2
		let z = 3
	`)
	if len(errors) > 0 {
		t.Errorf("expected no errors for distinct names, got: %v", errors)
	}
}

// ---------------------------------------------------------------------------
// Duplicate parameters
// ---------------------------------------------------------------------------

func TestDuplicate_Parameters_SameName(t *testing.T) {
	errors := parseAndCollectErrors(t, `let f = (x: i32, x: i32) -> i32 => x`)
	assertCollectorErrorContains(t, errors, "parameter x is already declared in this scope")
}

func TestDuplicate_Parameters_DistinctNames_NoError(t *testing.T) {
	errors := parseAndCollectErrors(t, `let f = (x: i32, y: i32) -> i32 => x + y`)
	if len(errors) > 0 {
		t.Errorf("expected no errors for distinct parameter names, got: %v", errors)
	}
}

// ---------------------------------------------------------------------------
// Destructuring re-declares an existing let / const / var binding
// ---------------------------------------------------------------------------

func TestRebind_Destructuring_RebindsLet_NoError(t *testing.T) {
	// Re-binding a let via destructuring is same-scope sequential rebinding and
	// is allowed.
	errors := parseAndCollectErrors(t, `
		let x = 1
		let [x, y] = some_array
	`)
	if len(errors) > 0 {
		t.Errorf("expected no errors for destructuring rebind of a let, got: %v", errors)
	}
}

func TestRebind_Destructuring_RebindsVar_NoError(t *testing.T) {
	errors := parseAndCollectErrors(t, `
		var x = 1
		let [x, y] = some_array
	`)
	if len(errors) > 0 {
		t.Errorf("expected no errors for destructuring rebind of a var, got: %v", errors)
	}
}

func TestDuplicate_Destructuring_WildcardNoError(t *testing.T) {
	// Wildcard _ should never trigger a duplicate error.
	errors := parseAndCollectErrors(t, `
		let _ = 1
		let [_, y] = some_array
	`)
	if len(errors) > 0 {
		t.Errorf("expected no errors for wildcard bindings, got: %v", errors)
	}
}

func TestDuplicate_Trait(t *testing.T) {
	errors := parseAndCollectErrors(t, `
		trait Show {
			show: (Self) -> string
		}
		trait Show {
			show: (Self) -> string
		}
	`)
	assertCollectorErrorContains(t, errors, `trait "Show" already defined`)
}

func TestDuplicate_Destructuring_NoConflict_NoError(t *testing.T) {
	errors := parseAndCollectErrors(t, `
		let a = 1
		let [b, c] = some_array
	`)
	if len(errors) > 0 {
		t.Errorf("expected no errors when names don't conflict, got: %v", errors)
	}
}
