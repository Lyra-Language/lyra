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

// **A doc comment on an `extern` attaches to it.** It did not until 08/19: `attachDoc`
// switches over the top-level declaration kinds and an extern is the kind added last, so
// `ExternDeclStmt.Doc` was a field nothing ever wrote — and every `///` above a foreign
// declaration was reported as documenting nothing (lyra-W017), which is the loudest
// possible way for a missing switch case to present and still went unnoticed, since no
// program had documented an extern yet. Hazard 8.
func TestDocComment_AttachesToAnExtern(t *testing.T) {
	program, _, _, _ := parseAndCollect(t, `/// The C library's absolute value.
///
/// # Panics
/// Traps on i32::MIN, which has no positive counterpart.
unsafe extern pure abs: (n: i32) -> i32
`)
	for _, stmt := range program.Statements {
		ext, ok := stmt.(*ast.ExternDeclStmt)
		if !ok {
			continue
		}
		if ext.Doc == nil {
			t.Fatal("an `extern` must carry the doc comment above it")
		}
		if want := "The C library's absolute value."; ext.Doc.Summary != want {
			t.Errorf("Summary = %q; want %q", ext.Doc.Summary, want)
		}
		if len(ext.Doc.Sections) != 1 || ext.Doc.Sections[0].Kind != ast.DocSectionPanics {
			t.Errorf("a `# Panics` heading should be classified; got %v", ext.Doc.Sections)
		}
		return
	}
	t.Fatal("no ExternDeclStmt collected")
}

// **Where each type name is written**, which is the question a type value cannot answer
// for itself: `TypesEqual` is structural, so a `Location` on `types.UnresolvedType` would
// make `Point` on one line unequal to `Point` on another. The span goes in a side table.
//
// Every position here is a *different* collector path, and one working says nothing about
// the others: a generic argument recurses through `parseType`, a parameterized **head** is
// read straight from its field and had to be recorded separately, and a trait method's
// signature is a `*types.LambdaType` hanging off the declaration.
func TestTypeRefs_EveryWrittenPositionIsRecorded(t *testing.T) {
	_, table, _, _ := parseAndCollect(t, `module main
struct Point { x: i64 }
struct Holder { p: Point, ps: []Point, m: Maybe<Point> }
type Coord = Point
trait Shown { pure show: (Self) -> Point }
data Wrap = W Point
tuple Pair(Point, i64)
let param = pure (p: Point) -> Point => p
let ptr = unsafe (q: ^Point) -> i64 => 0
let fn = pure (f: (Point) -> Point) -> i64 => 0
unsafe extern pure ext: (buf: ^Point) -> i64
let ann = pure () -> i64 => { let z: Point = Point { x: 1 }; z.x }
impl Shown for Point { show = pure (self) => Point { x: 0 } }
`)
	byLine := map[int]int{}
	for _, ref := range table.TypeRefs.Refs("") {
		byLine[ref.Loc.StartLine]++
	}
	for _, want := range []struct {
		line, count int
		what        string
	}{
		{3, 4, "a field, an array element, a generic head and its argument"},
		{4, 1, "an alias's target"},
		{5, 1, "a trait method's signature"},
		{6, 1, "a data constructor's payload"},
		{7, 1, "a named tuple's element"},
		{8, 2, "a parameter and a return type"},
		{9, 1, "a raw pointer's pointee"},
		{10, 2, "a function type's parameter and return"},
		{11, 1, "an extern's signature"},
		{12, 1, "a local annotation (the struct *literal* is an expression, not a type ref)"},
		{13, 2, "an impl's trait and its target"},
	} {
		if got := byLine[want.line]; got != want.count {
			t.Errorf("line %d (%s): recorded %d references, want %d", want.line, want.what, got, want.count)
		}
	}
}

// The innermost span wins, so a cursor inside `Maybe<Point>` resolves to the argument
// rather than to whatever encloses it.
func TestTypeRefs_TheInnermostSpanWins(t *testing.T) {
	src := `module main
struct Point { x: i64 }
let f = pure (m: Maybe<Point>) -> i64 => 0
`
	_, table, _, _ := parseAndCollect(t, src)
	// Column of the `P` in `Maybe<Point>`, 1-based.
	col := strings.Index(strings.Split(src, "\n")[2], "Point>") + 1
	ref, ok := table.TypeRefs.At("", 3, col)
	if !ok {
		t.Fatal("no reference under a cursor on Point")
	}
	if ref.Name != "Point" {
		t.Errorf("cursor on Point resolved to %q", ref.Name)
	}
	// And on the head, the head.
	headCol := strings.Index(strings.Split(src, "\n")[2], "Maybe<") + 1
	if head, ok := table.TypeRefs.At("", 3, headCol); !ok || head.Name != "Maybe" {
		t.Errorf("cursor on Maybe resolved to %v (ok=%v)", head, ok)
	}
}
