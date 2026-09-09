package collector_test

import (
	"strings"
	"testing"

	diag "github.com/Lyra-Language/lyra/pkg/diagnostic"
)

// A union's body reuses the grammar's `struct_member` rule, because a union member *is*
// a name and a type and a second rule would be free to drift from the first. The two
// struct-only forms therefore arrive here and are refused by name (lyra-E072) — the
// admit-then-report trade lyra-E065 and lyra-E067 already make.

func collectUnionErrors(t *testing.T, src string) []diag.Diagnostic {
	t.Helper()
	var out []diag.Diagnostic
	for _, e := range parseAndCollectErrors(t, src) {
		if d, ok := e.(diag.Diagnostic); ok && d.Code == diag.CodeMalformedUnion {
			out = append(out, d)
		}
	}
	return out
}

// `readonly` freezes a field after construction, and a union has nothing to freeze:
// every member names the same bytes, so writing any other member rewrites this one.
func TestUnion_ReadonlyMemberIsRefused(t *testing.T) {
	errs := collectUnionErrors(t, "\nunion U { readonly a: u32, b: u32 }\n")
	if len(errs) == 0 {
		t.Fatalf("expected lyra-E072 for a readonly union member")
	}
	if !strings.Contains(errs[0].Message, "cannot be `readonly`") {
		t.Errorf("message does not name the rule: %q", errs[0].Message)
	}
}

// A default value presumes one member is the one initialized, which is exactly the fact
// a union does not record.
func TestUnion_DefaultValueMemberIsRefused(t *testing.T) {
	errs := collectUnionErrors(t, "\nunion U { a: u32 = 5, b: u32 }\n")
	if len(errs) == 0 {
		t.Fatalf("expected lyra-E072 for a union member with a default value")
	}
	if !strings.Contains(errs[0].Message, "default value") {
		t.Errorf("message does not name the rule: %q", errs[0].Message)
	}
}

// A well-formed union collects clean — the guard that the two rules above are not firing
// on every declaration.
func TestUnion_WellFormedCollectsClean(t *testing.T) {
	if errs := collectUnionErrors(t, "\nunion U { a: u32, b: f64 }\n"); len(errs) != 0 {
		t.Errorf("expected no lyra-E072, got: %v", errs)
	}
}
