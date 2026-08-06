package collector_test

import (
	"strings"
	"testing"

	"github.com/Lyra-Language/lyra/pkg/printer"
)

// A **bare jump** as a match arm body — `None => break`, `_ => continue`,
// `Err e => return e`.
//
// The jump forms are statements and an arm body is an expression, so before 08/06
// the bare spelling parsed `break` as an identifier and reported `undefined
// identifier "break"`. The *braced* form `None => { break }` already worked, since
// a block holds statements — only the spelling was missing.
//
// The collector therefore **erases** it: a bare jump becomes exactly the
// single-statement block the braced form produces. These tests pin that erasure
// rather than the feature's behaviour, because the erasure is what makes the
// feature cheap — if the two spellings ever stop collecting alike, the bare one has
// acquired a meaning of its own and every pass downstream (typechecker, purity,
// ownership, the four backend arm-body sites) becomes a place it can differ.
func TestMatchArm_BareJumpCollectsAsTheBracedForm(t *testing.T) {
	cases := []struct{ name, bare, braced string }{
		{
			"break",
			"for {\n  match m {\n    A => break,\n    _ => 1,\n  }\n}",
			"for {\n  match m {\n    A => { break },\n    _ => 1,\n  }\n}",
		},
		{
			"continue",
			"for {\n  match m {\n    A => continue,\n    _ => 1,\n  }\n}",
			"for {\n  match m {\n    A => { continue },\n    _ => 1,\n  }\n}",
		},
		{
			"return with a value",
			"let f = () -> i64 => {\n  match m {\n    A => return 7,\n    _ => 1,\n  }\n}",
			"let f = () -> i64 => {\n  match m {\n    A => { return 7 },\n    _ => 1,\n  }\n}",
		},
		{
			"bare return",
			"let f = () -> void => {\n  match m {\n    A => return,\n    _ => 1,\n  }\n}",
			"let f = () -> void => {\n  match m {\n    A => { return },\n    _ => 1,\n  }\n}",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			bareProg, _, _, bareErrs := parseAndCollect(t, c.bare)
			if len(bareErrs) != 0 {
				t.Fatalf("bare form failed to collect: %v", bareErrs)
			}
			bracedProg, _, _, bracedErrs := parseAndCollect(t, c.braced)
			if len(bracedErrs) != 0 {
				t.Fatalf("braced form failed to collect: %v", bracedErrs)
			}
			// Locations differ (the braces move the columns), so compare the printed
			// structure with location lines dropped — the claim is that the two build
			// the same *shape*, not that they occupy the same source span.
			bare := stripLocations(printer.PrintAST(bareProg))
			braced := stripLocations(printer.PrintAST(bracedProg))
			if bare != braced {
				t.Errorf("bare and braced forms collect differently:\n--- bare ---\n%s\n--- braced ---\n%s",
					bare, braced)
			}
			// And it really is a block wrapping the jump, not the jump smuggled in as
			// an expression — which would be the shape that leaks downstream.
			if !strings.Contains(bare, "BlockExpr") {
				t.Errorf("expected the arm body to be a BlockExpr, got:\n%s", bare)
			}
		})
	}
}

// stripLocations removes the printer's Location lines so two ASTs built from
// different source spellings can be compared on structure alone.
func stripLocations(s string) string {
	var out []string
	for _, line := range strings.Split(s, "\n") {
		if strings.Contains(line, "Location:") {
			continue
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}
