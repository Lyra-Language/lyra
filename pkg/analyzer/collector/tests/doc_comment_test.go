package collector_test

import (
	"strings"
	"testing"

	"github.com/Lyra-Language/lyra/pkg/analyzer/collector"
	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/ast/symbols"
	"github.com/Lyra-Language/lyra/pkg/parser"
)

// docOfStatement pulls the Doc off whichever documentable statement kind this is.
func docOfStatement(stmt ast.AstNode) *ast.Doc {
	switch s := stmt.(type) {
	case *ast.VarDeclStmt:
		return s.Doc
	case *ast.TypeDeclStmt:
		return s.Doc
	case *ast.TraitDeclStmt:
		return s.Doc
	case *ast.TraitImplStmt:
		return s.Doc
	case *ast.ModuleDeclStmt:
		return s.Doc
	}
	return nil
}

// findDecl returns the first top-level statement whose name matches.
func findDecl(t *testing.T, program *ast.Program, name string) ast.AstNode {
	t.Helper()
	for _, stmt := range program.Statements {
		if named, ok := stmt.(ast.Named); ok && named.GetName() == name {
			return stmt
		}
	}
	t.Fatalf("no top-level declaration named %q", name)
	return nil
}

func assertDocText(t *testing.T, doc *ast.Doc, want string) {
	t.Helper()
	if doc == nil {
		t.Fatalf("expected documentation %q, got none", want)
	}
	if doc.Text != want {
		t.Errorf("doc text = %q, want %q", doc.Text, want)
	}
}

func TestDocComment_AttachesToTopLevelBinding(t *testing.T) {
	program, _, _, errs := parseAndCollect(t, `
/// Adds two numbers.
///
/// # Panics
///
/// Traps if the sum overflows.
pub let add = pure (a: i64, b: i64) -> i64 => a + b
`)
	assertNoStrayDocWarning(t, errs)

	doc := docOfStatement(findDecl(t, program, "add"))
	if doc == nil {
		t.Fatal("`add` is undocumented")
	}
	if doc.Summary != "Adds two numbers." {
		t.Errorf("Summary = %q", doc.Summary)
	}
	if s, ok := doc.Section(ast.DocSectionPanics); !ok || s.Body != "Traps if the sum overflows." {
		t.Errorf("Panics section = %+v ok=%v", s, ok)
	}
}

func TestDocComment_AttachesToStructAndItsFields(t *testing.T) {
	program, _, _, errs := parseAndCollect(t, `
/// A point in the plane.
pub struct Point {
  /// Distance along the x axis.
  x: f64,
  /// Distance along the y axis.
  y: f64,
}
`)
	assertNoStrayDocWarning(t, errs)

	decl := findDecl(t, program, "Point").(*ast.TypeDeclStmt)
	assertDocText(t, decl.Doc, "A point in the plane.")
	assertDocText(t, decl.MemberDoc("x"), "Distance along the x axis.")
	assertDocText(t, decl.MemberDoc("y"), "Distance along the y axis.")
	if decl.MemberDoc("nonexistent") != nil {
		t.Error("MemberDoc returned a doc for a field that does not exist")
	}
}

func TestDocComment_AttachesToDataConstructors(t *testing.T) {
	program, _, _, errs := parseAndCollect(t, `
/// A cardinal direction.
pub data Dir =
  /// Towards the top of the map.
  North
  /// Towards the bottom of the map.
  | South
`)
	assertNoStrayDocWarning(t, errs)

	decl := findDecl(t, program, "Dir").(*ast.TypeDeclStmt)
	assertDocText(t, decl.Doc, "A cardinal direction.")
	assertDocText(t, decl.MemberDoc("North"), "Towards the top of the map.")
	assertDocText(t, decl.MemberDoc("South"), "Towards the bottom of the map.")
}

// The first member of a trait or impl body is the case tree-sitter places differently
// from the rest: its doc comment attaches to the enclosing node rather than to the
// member list, because the `{` belongs to the declaration. A regression here documents
// every method *except* the first, which is the one most likely to carry the block
// explaining the whole trait.
func TestDocComment_AttachesToEveryTraitMethodIncludingTheFirst(t *testing.T) {
	program, _, _, errs := parseAndCollect(t, `
/// Things that can be rendered.
pub trait Show {
  /// Renders self as a string.
  show: (Self) -> string
  /// Renders self for a developer.
  debug: (Self) -> string
}
`)
	assertNoStrayDocWarning(t, errs)

	decl := findDecl(t, program, "Show").(*ast.TraitDeclStmt)
	assertDocText(t, decl.Doc, "Things that can be rendered.")
	if len(decl.Methods) != 2 {
		t.Fatalf("got %d methods, want 2", len(decl.Methods))
	}
	assertDocText(t, decl.Methods[0].Doc, "Renders self as a string.")
	assertDocText(t, decl.Methods[1].Doc, "Renders self for a developer.")
}

func TestDocComment_AttachesToEveryImplMethodIncludingTheFirst(t *testing.T) {
	program, _, _, errs := parseAndCollect(t, `
pub struct Pt { x: f64 }

/// Points render as a coordinate pair.
impl Show for Pt {
  /// Uses two decimal places.
  show = (self) => "pt"
  /// Includes the type name.
  debug = (self) => "Pt"
}
`)
	assertNoStrayDocWarning(t, errs)

	var impl *ast.TraitImplStmt
	for _, stmt := range program.Statements {
		if s, ok := stmt.(*ast.TraitImplStmt); ok {
			impl = s
		}
	}
	if impl == nil {
		t.Fatal("no impl statement collected")
	}
	assertDocText(t, impl.Doc, "Points render as a coordinate pair.")
	if len(impl.Methods) != 2 {
		t.Fatalf("got %d methods, want 2", len(impl.Methods))
	}
	assertDocText(t, impl.Methods[0].Doc, "Uses two decimal places.")
	assertDocText(t, impl.Methods[1].Doc, "Includes the type name.")
}

func TestDocComment_InnerDocDocumentsTheModule(t *testing.T) {
	program, table, _, errs := parseAndCollect(t, `//! Arithmetic helpers.
//!
//! Everything here traps on overflow.
module demo.math

let x = 1
`)
	assertNoStrayDocWarning(t, errs)

	var mod *ast.ModuleDeclStmt
	for _, stmt := range program.Statements {
		if s, ok := stmt.(*ast.ModuleDeclStmt); ok {
			mod = s
		}
	}
	if mod == nil {
		t.Fatal("no module declaration collected")
	}
	if mod.Doc == nil || mod.Doc.Summary != "Arithmetic helpers." {
		t.Fatalf("module doc = %+v", mod.Doc)
	}
	if !mod.Doc.IsInner {
		t.Error("a `//!` doc should be marked IsInner")
	}
	// The table is keyed by the module path the *resolver* supplies, which is empty
	// for a single-file collect — the file's own `module` line does not name the key.
	if table.ModuleDocs[""] == nil {
		t.Error("the module doc was not recorded on the symbol table")
	}
}

// The stray-doc warning. Each of these is a `///` that documents nothing, and each is a
// spelling a person actually writes.
func TestDocComment_StrayIsWarned(t *testing.T) {
	const strayOuter = "documents nothing"
	const strayInner = "must sit in the file's header"

	cases := map[string]struct{ src, want string }{
		"blank line before the declaration": {"/// documents nothing\n\nlet a = 1\n", strayOuter},
		"end of file":                       {"let a = 1\n\n/// trailing thought\n", strayOuter},
		"on a local binding":                {"let f = () -> i64 => {\n  /// not a declaration\n  let a = 1\n  a\n}\n", strayOuter},
		"inner doc after the first decl":    {"let a = 1\n//! too late to be a header\n", strayInner},
	}
	for name, tc := range cases {
		src, want := tc.src, tc.want
		t.Run(name, func(t *testing.T) {
			errs := parseAndCollectErrors(t, src)
			assertCollectorErrorContains(t, errs, want)
		})
	}
}

func TestDocComment_DividerRuleIsNotDocumentation(t *testing.T) {
	// `////////` lexes as an ordinary comment, so it neither documents the
	// declaration below it nor trips the stray warning.
	program, _, _, errs := parseAndCollect(t, `
////////////////////////////////////
// Section: arithmetic
////////////////////////////////////
let add = (a: i64, b: i64) -> i64 => a + b
`)
	assertNoStrayDocWarning(t, errs)

	if doc := docOfStatement(findDecl(t, program, "add")); doc != nil {
		t.Errorf("a divider rule became documentation: %q", doc.Text)
	}
}

func assertNoStrayDocWarning(t *testing.T, errs []error) {
	t.Helper()
	for _, e := range errs {
		if strings.Contains(e.Error(), "lyra-W017") || strings.Contains(e.Error(), "documents nothing") {
			t.Errorf("unexpected stray-doc warning: %v", e)
		}
	}
}

// `//!` may sit under the `module` line as well as above it. The line naming the module
// is the obvious thing for its documentation to follow, and only Rust's lack of a module
// header makes top-of-file the only spelling elsewhere.
func TestDocComment_InnerDocMayFollowTheModuleHeader(t *testing.T) {
	program, _, _, errs := parseAndCollect(t, `module demo.math
//! Arithmetic helpers.

let x = 1
`)
	assertNoStrayDocWarning(t, errs)

	for _, stmt := range program.Statements {
		if s, ok := stmt.(*ast.ModuleDeclStmt); ok {
			if s.Doc == nil || s.Doc.Summary != "Arithmetic helpers." {
				t.Fatalf("module doc = %+v", s.Doc)
			}
			return
		}
	}
	t.Fatal("no module declaration collected")
}

// A multi-file module's summary is the first paragraph of its files' `//!` headers
// joined, so which file leads decides how the module describes itself. Without a rule
// that is whichever file the walk reached first — a fact about the filesystem. A file
// named for the module's last path segment leads.
func TestDocComment_ModuleDocLedByTheFileNamedForTheModule(t *testing.T) {
	files := []struct{ file, src string }{
		// Deliberately walked in an order where the lead is not first.
		{"/tmp/m/array.lyra", "module demo.util\n//! Arrays.\n"},
		{"/tmp/m/util.lyra", "module demo.util\n//! The utility module.\n"},
		{"/tmp/m/zzz.lyra", "module demo.util\n//! Odds and ends.\n"},
	}
	table := collectFilesIntoModule(t, "demo.util", files)

	doc := table.ModuleDocs["demo.util"]
	if doc == nil {
		t.Fatal("no module documentation collected")
	}
	if want := "The utility module."; doc.Summary != want {
		t.Errorf("Summary = %q, want %q — util.lyra should lead", doc.Summary, want)
	}
	// Leading reorders; it never drops. The other files' text is still there.
	for _, want := range []string{"Arrays.", "Odds and ends."} {
		if !strings.Contains(doc.Text, want) {
			t.Errorf("the join lost %q:\n%s", want, doc.Text)
		}
	}
}

// With no file named for the module, the join order is unchanged — the convention costs
// nothing when nobody uses it.
func TestDocComment_ModuleDocKeepsWalkOrderWithoutALeadFile(t *testing.T) {
	files := []struct{ file, src string }{
		{"/tmp/m/array.lyra", "module demo.util\n//! Arrays.\n"},
		{"/tmp/m/zzz.lyra", "module demo.util\n//! Odds and ends.\n"},
	}
	table := collectFilesIntoModule(t, "demo.util", files)
	if want := "Arrays."; table.ModuleDocs["demo.util"].Summary != want {
		t.Errorf("Summary = %q, want %q (walk order)", table.ModuleDocs["demo.util"].Summary, want)
	}
}

// collectFilesIntoModule walks several in-memory files as one module and returns the
// resulting symbol table.
func collectFilesIntoModule(t *testing.T, modulePath string, files []struct{ file, src string }) *symbols.SymbolTable {
	t.Helper()
	c := collector.NewCollector(nil)
	for _, f := range files {
		tree, err := parser.Parse(f.src)
		if err != nil {
			t.Fatalf("parsing %s: %v", f.file, err)
		}
		c.AddFile(tree.RootNode(), []byte(f.src), f.file, modulePath)
	}
	_, table, _, _ := c.Finish()
	return table
}
