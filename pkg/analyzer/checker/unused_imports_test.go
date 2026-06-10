package checker_test

import (
	"slices"
	"testing"

	"github.com/Lyra-Language/lyra/pkg/analyzer/checker"
	"github.com/Lyra-Language/lyra/pkg/analyzer/collector"
	diag "github.com/Lyra-Language/lyra/pkg/diagnostic"
	"github.com/Lyra-Language/lyra/pkg/parser"
)

func parseAndCheckUnusedImports(t *testing.T, source string) []diag.Diagnostic {
	t.Helper()
	tree, err := parser.Parse(source)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	c := collector.NewCollector([]byte(source))
	program, _, _, _ := c.Collect(tree.RootNode())
	return checker.CheckUnusedImports(program)
}

func assertNoUnusedImports(t *testing.T, diags []diag.Diagnostic) {
	t.Helper()
	if len(diags) > 0 {
		t.Errorf("expected no unused-import warnings, got %d: %v", len(diags), diags)
	}
}

func assertUnusedImportCount(t *testing.T, diags []diag.Diagnostic, count int) {
	t.Helper()
	if len(diags) != count {
		t.Errorf("expected %d unused-import warning(s), got %d: %v", count, len(diags), diags)
	}
}

func assertUnusedImportHasTags(t *testing.T, diags []diag.Diagnostic) {
	t.Helper()
	for i, d := range diags {
		if !slices.Contains(d.Tags, diag.TagUnnecessary) {
			t.Errorf("diagnostic[%d] missing TagUnnecessary: %v", i, d)
		}
	}
}

// TestUnusedImport_NoDiag_MemberUsed verifies that a used member import is not flagged.
func TestUnusedImport_NoDiag_MemberUsed(t *testing.T) {
	src := `
import foo.{ bar }
let x = bar(1)
`
	assertNoUnusedImports(t, parseAndCheckUnusedImports(t, src))
}

// TestUnusedImport_Diag_MemberUnused verifies that an unused member import is flagged.
func TestUnusedImport_Diag_MemberUnused(t *testing.T) {
	src := `
import foo.{ bar }
let x = 42
`
	diags := parseAndCheckUnusedImports(t, src)
	assertUnusedImportCount(t, diags, 1)
	assertUnusedImportHasTags(t, diags)
}

// TestUnusedImport_NoDiag_AliasedMemberUsed verifies that a used aliased member is not flagged.
func TestUnusedImport_NoDiag_AliasedMemberUsed(t *testing.T) {
	src := `
import foo.{ bar as baz }
let x = baz(1)
`
	assertNoUnusedImports(t, parseAndCheckUnusedImports(t, src))
}

// TestUnusedImport_Diag_AliasedMemberUnused verifies that an unused aliased member is flagged
// under its alias name (not the original name).
func TestUnusedImport_Diag_AliasedMemberUnused(t *testing.T) {
	src := `
import foo.{ bar as baz }
let x = 42
`
	diags := parseAndCheckUnusedImports(t, src)
	assertUnusedImportCount(t, diags, 1)
	assertUnusedImportHasTags(t, diags)
}

// TestUnusedImport_Diag_OriginalNameDoesNotCountForAlias verifies that using the original
// name `bar` does not count as using the alias `baz`.
func TestUnusedImport_Diag_OriginalNameDoesNotCountForAlias(t *testing.T) {
	src := `
import foo.{ bar as baz }
let x = bar(1)
`
	diags := parseAndCheckUnusedImports(t, src)
	assertUnusedImportCount(t, diags, 1)
}

// TestUnusedImport_NoDiag_ModuleAliasUsed verifies that a used module alias is not flagged.
func TestUnusedImport_NoDiag_ModuleAliasUsed(t *testing.T) {
	src := `
import foo.bar as fb
let x = fb.thing()
`
	assertNoUnusedImports(t, parseAndCheckUnusedImports(t, src))
}

// TestUnusedImport_Diag_ModuleAliasUnused verifies that an unused module alias is flagged.
func TestUnusedImport_Diag_ModuleAliasUnused(t *testing.T) {
	src := `
import foo.bar as fb
let x = 42
`
	diags := parseAndCheckUnusedImports(t, src)
	assertUnusedImportCount(t, diags, 1)
	assertUnusedImportHasTags(t, diags)
}

// TestUnusedImport_NoDiag_PlainImportUsed verifies that a used plain import is not flagged.
func TestUnusedImport_NoDiag_PlainImportUsed(t *testing.T) {
	src := `
import foo.bar
let x = bar.thing()
`
	assertNoUnusedImports(t, parseAndCheckUnusedImports(t, src))
}

// TestUnusedImport_Diag_PlainImportUnused verifies that a plain import whose last path
// component never appears as an identifier is flagged.
func TestUnusedImport_Diag_PlainImportUnused(t *testing.T) {
	src := `
import foo.bar
let x = 42
`
	diags := parseAndCheckUnusedImports(t, src)
	assertUnusedImportCount(t, diags, 1)
	assertUnusedImportHasTags(t, diags)
}

// TestUnusedImport_NoDiag_UnderscorePrefixedMember verifies that _foo member names
// are silently skipped (intentionally-unused convention).
func TestUnusedImport_NoDiag_UnderscorePrefixedMember(t *testing.T) {
	src := `
import foo.{ _bar }
let x = 42
`
	assertNoUnusedImports(t, parseAndCheckUnusedImports(t, src))
}

// TestUnusedImport_Diag_MultipleUnusedMembers verifies that each unused member
// in a multi-member import is reported separately.
func TestUnusedImport_Diag_MultipleUnusedMembers(t *testing.T) {
	src := `
import foo.{ a, b, c }
let x = a(1)
`
	diags := parseAndCheckUnusedImports(t, src)
	assertUnusedImportCount(t, diags, 2)
	assertUnusedImportHasTags(t, diags)
}

// TestUnusedImport_NoDiag_AllMembersUsed verifies that all-used multi-member imports
// produce no warnings.
func TestUnusedImport_NoDiag_AllMembersUsed(t *testing.T) {
	src := `
import foo.{ a, b, c }
let x = a(b(c(1)))
`
	assertNoUnusedImports(t, parseAndCheckUnusedImports(t, src))
}
