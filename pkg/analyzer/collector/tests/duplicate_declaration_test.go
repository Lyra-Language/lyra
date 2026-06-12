package collector_test

import (
	"testing"

	diag "github.com/Lyra-Language/lyra/pkg/diagnostic"
)

// ---------------------------------------------------------------------------
// Same-scope duplicate variable declarations
// ---------------------------------------------------------------------------

func TestDuplicate_SameScope_TwoLet(t *testing.T) {
	errors := parseAndCollectErrors(t, `
		let x = 1
		let x = 2
	`)
	assertCollectorErrorContains(t, errors, "x is already declared in this scope")
}

func TestDuplicate_SameScope_LetThenVar(t *testing.T) {
	errors := parseAndCollectErrors(t, `
		let x = 1
		var x = 2
	`)
	assertCollectorErrorContains(t, errors, "x is already declared in this scope")
}

func TestDuplicate_SameScope_VarThenLet(t *testing.T) {
	errors := parseAndCollectErrors(t, `
		var x = 1
		let x = 2
	`)
	assertCollectorErrorContains(t, errors, "x is already declared in this scope")
}

func TestDuplicate_SameScope_ThreeDeclarations(t *testing.T) {
	errors := parseAndCollectErrors(t, `
		let x = 1
		let y = 2
		let x = 3
	`)
	assertCollectorErrorContains(t, errors, "x is already declared in this scope")
	// y is fine
	for _, e := range errors {
		if e.Error() != "" {
			break
		}
	}
}

func TestDuplicate_ErrorMessageContainsOriginalLocation(t *testing.T) {
	// The error should carry RelatedInformation pointing to line 1 (the original declaration).
	errors := parseAndCollectErrors(t, `let x = 1
let x = 2`)
	for _, e := range errors {
		d, ok := e.(diag.Diagnostic)
		if !ok {
			continue
		}
		if len(d.RelatedInformation) > 0 && d.RelatedInformation[0].Location.StartLine == 1 {
			return
		}
	}
	t.Errorf("expected a duplicate-declaration diagnostic with RelatedInformation pointing to line 1, got: %v", errors)
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

func TestDuplicate_Destructuring_RedeclaresLet(t *testing.T) {
	errors := parseAndCollectErrors(t, `
		let x = 1
		let [x, y] = some_array
	`)
	assertCollectorErrorContains(t, errors, "cannot re-declare let x")
}

func TestDuplicate_Destructuring_RedeclaresVar(t *testing.T) {
	// var is mutable but re-declaring it via destructuring is still a duplicate.
	errors := parseAndCollectErrors(t, `
		var x = 1
		let [x, y] = some_array
	`)
	assertCollectorErrorContains(t, errors, "x is already declared in this scope")
}

func TestDuplicate_Destructuring_TupleRedeclaresLet(t *testing.T) {
	errors := parseAndCollectErrors(t, `
		let a = 1
		let (a, b) = some_tuple
	`)
	assertCollectorErrorContains(t, errors, "cannot re-declare let a")
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
