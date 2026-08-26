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
	// No typechecker pass here, so no UFCS resolutions to consult — the syntactic
	// behaviour these tests pin is unchanged by that map being absent.
	return checker.CheckUnusedImports(program, checker.ImportUse{})
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

// ── An imported *type* is used in type positions, not as an identifier (08/14) ──
//
// The walk counted `IdentifierExpr` and `SpreadExpr` only, so the two ways an imported type
// is actually used were both invisible: a struct literal names it (`Complex { re: … }`, a
// StructInstanceExpr) and a signature names it (`(c: Complex<f64>)`, a *type* — not an
// expression at all, so no amount of expression walking could reach it).
//
// The result was advice to delete a load-bearing import: `import std.math.{ Complex }`
// warned as unused in a program that fails to compile without it, with
// `undefined struct type "Complex"`. That is the failure the check's own UFCS note already
// describes, arriving by a second route.

func TestUnusedImports_TypeUsedInAStructLiteral(t *testing.T) {
	assertNoUnusedImports(t, parseAndCheckUnusedImports(t, `
import std.math.{ Complex }
let main = () => {
  let c = Complex { re: 1.0, im: 2.0 }
  println(c.re)
}
`))
}

func TestUnusedImports_TypeUsedOnlyInASignature(t *testing.T) {
	assertNoUnusedImports(t, parseAndCheckUnusedImports(t, `
import std.math.{ Complex }
let re_of = (c: Complex<f64>) -> f64 => 0.0
let main = () => println(re_of)
`))
}

// A type mentioned only inside another type's declaration is still a use.
func TestUnusedImports_TypeUsedOnlyInAFieldType(t *testing.T) {
	assertNoUnusedImports(t, parseAndCheckUnusedImports(t, `
import std.math.{ Complex }
struct Pair { a: Complex<f64>, b: i64 }
let main = () => println(1)
`))
}

// Nested in a composite: the walk has to descend, not just match a leaf.
func TestUnusedImports_TypeNestedInAComposite(t *testing.T) {
	assertNoUnusedImports(t, parseAndCheckUnusedImports(t, `
import std.math.{ Complex }
let grid = (xs: [][]Complex<f64>) -> i64 => 0
let main = () => println(grid)
`))
}

// **The check must still bite.** Widening what counts as a reference risks silencing it
// everywhere, which is worse than the false positive it replaced: a warning that never
// fires is indistinguishable from a check that was deleted.
func TestUnusedImports_GenuinelyUnusedTypeStillWarns(t *testing.T) {
	diags := parseAndCheckUnusedImports(t, `
import std.math.{ Complex }
let main = () => println("no complex here")
`)
	if len(diags) != 1 {
		t.Fatalf("want one warning for a genuinely unused type import, got %d: %v", len(diags), diags)
	}
	if !slices.ContainsFunc(diags, func(d diag.Diagnostic) bool {
		return d.Code == diag.CodeUnusedImport
	}) {
		t.Errorf("want lyra-W004, got %v", diags)
	}
}

// **An extern's signature is a use**, and it is the *only* place an imported type can
// appear at the boundary — an extern has no body. Missed until 08/22, which made
// `import std.ffi.{ CLong }` beside `unsafe extern pure labs: (CLong) -> CLong` advise
// deleting the import the program cannot compile without: the exact failure — advice to
// break working code — that widening this walk in the first place was for.
func TestUnusedImports_TypeUsedOnlyInAnExternSignature(t *testing.T) {
	assertNoUnusedImports(t, parseAndCheckUnusedImports(t, `
import std.math.{ Complex }
unsafe extern pure conj: (Complex<f64>) -> Complex<f64>
let main = () => println(1)
`))
}

// The same shape one declaration kind over: a trait's method signatures are LambdaTypes
// hanging off the declaration, not LambdaExprs in the tree, so the expression walk never
// reached them either. A trait is where an imported type is most likely to be named with
// no body mentioning it at all.
func TestUnusedImports_TypeUsedOnlyInATraitMethodSignature(t *testing.T) {
	assertNoUnusedImports(t, parseAndCheckUnusedImports(t, `
import std.math.{ Complex }
trait Scales { pure scale: (Self, Complex<f64>) -> Self }
let main = () => println(1)
`))
}
