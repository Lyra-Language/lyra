package collector_test

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/Lyra-Language/lyra/pkg/analyzer/collector"
	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/ast/symbols"
	"github.com/Lyra-Language/lyra/pkg/parser"
)

// collectPrelude walks the real shipped prelude as one multi-file module — which is what
// it is — rather than a fixture, because the thing under test is the documentation that
// actually ships.
func collectPrelude(t *testing.T) (*ast.Program, *symbols.SymbolTable, []error) {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", "..", "..", "..", "std", "prelude"))
	if err != nil {
		t.Fatalf("resolving the prelude path: %v", err)
	}
	files, err := filepath.Glob(filepath.Join(root, "*.lyra"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no prelude sources under %s (err=%v)", root, err)
	}
	// Sorted, because the module's documentation is the files' headers joined in walk
	// order — an unstable order would make the joined text differ between runs.
	sort.Strings(files)

	c := collector.NewCollector(nil)
	for _, f := range files {
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("reading %s: %v", f, err)
		}
		tree, err := parser.Parse(string(src))
		if err != nil {
			t.Fatalf("parsing %s: %v", f, err)
		}
		c.AddFile(tree.RootNode(), src, f, "std.prelude")
	}
	program, table, _, errs := c.Finish()
	return program, table, errs
}

// Every public declaration in the prelude carries documentation, and none of it is
// stray. This is a coverage guard rather than a spot check: the prelude is what a doc
// generator will render first, and an undocumented export there is the one gap every
// user sees.
func TestPrelude_EveryDeclarationIsDocumented(t *testing.T) {
	program, _, errs := collectPrelude(t)
	for _, e := range errs {
		// Any diagnostic at all, but the stray-doc warning is the one this test is
		// most likely to catch — it fires when a `//` note is written *between* a
		// doc block and its declaration, which detaches the documentation while
		// leaving the file looking documented.
		t.Errorf("prelude diagnostic: %v", e)
	}

	var undocumented []string
	for _, stmt := range program.Statements {
		var doc *ast.Doc
		var name string
		switch s := stmt.(type) {
		case *ast.VarDeclStmt:
			doc, name = s.Doc, s.Name
		case *ast.TypeDeclStmt:
			doc, name = s.Doc, s.Name
		case *ast.TraitDeclStmt:
			doc, name = s.Doc, s.Name
		case *ast.TraitImplStmt:
			doc, name = s.Doc, "impl "+s.TraitName
		default:
			continue
		}
		if doc == nil {
			undocumented = append(undocumented, name)
		}
	}
	if len(undocumented) > 0 {
		t.Errorf("undocumented prelude declarations: %v", undocumented)
	}
}

// The members — struct fields, data constructors, trait method signatures, impl methods —
// are the half that a plain "does it parse" check cannot see, and the half whose
// attachment depends on the two CST placement rules DocFor absorbs.
func TestPrelude_MembersAreDocumented(t *testing.T) {
	program, _, _ := collectPrelude(t)

	// Spot-checked by name rather than counted, so the test says which member it
	// means. A count would pass while documenting the wrong things.
	wantTypeMembers := map[string][]string{
		"Maybe":    {"None", "Some"},
		"Result":   {"Ok", "Err"},
		"Ordering": {"Less", "Equal", "Greater"},
		"Rng":      {"state"},
	}
	for _, stmt := range program.Statements {
		decl, ok := stmt.(*ast.TypeDeclStmt)
		if !ok {
			continue
		}
		for _, member := range wantTypeMembers[decl.Name] {
			if decl.MemberDoc(member) == nil {
				t.Errorf("%s.%s is undocumented", decl.Name, member)
			}
		}
		delete(wantTypeMembers, decl.Name)
	}
	for name := range wantTypeMembers {
		t.Errorf("type %q was not found in the prelude", name)
	}

	// A trait's method signature carries the *contract*'s documentation. `Needle` is
	// the interesting one: its `found_at` is the only method a user type must write.
	for _, stmt := range program.Statements {
		decl, ok := stmt.(*ast.TraitDeclStmt)
		if !ok {
			continue
		}
		for _, m := range decl.Methods {
			if m.Doc == nil {
				t.Errorf("trait %s: method %s is undocumented", decl.Name, m.GetName())
			}
		}
	}
}

// The `# Panics` section is the one a trap-by-default language most needs indexed, and
// it is also the section most likely to be quietly missing — the trap is in the callee,
// so nothing in a caller's signature hints at it.
func TestPrelude_PanicsSectionsAreClassified(t *testing.T) {
	program, _, _ := collectPrelude(t)

	traps := map[string]bool{}
	for _, stmt := range program.Statements {
		decl, ok := stmt.(*ast.VarDeclStmt)
		if !ok || decl.Doc == nil {
			continue
		}
		if _, has := decl.Doc.Section(ast.DocSectionPanics); has {
			traps[decl.Name] = true
		}
	}

	// Every prelude function that can trap, and nothing else needs one.
	for _, name := range []string{"expect", "unwrap", "below", "between", "random_below", "random_between", "split"} {
		if !traps[name] {
			t.Errorf("%s can trap but has no `# Panics` section", name)
		}
	}
	// A function with no failure mode must not claim one.
	for _, name := range []string{"is_some", "map", "filter", "trim", "starts_with", "contains"} {
		if traps[name] {
			t.Errorf("%s cannot trap but documents a `# Panics` section", name)
		}
	}
}

// The module's own documentation is every file's `//!` header joined, led by the file
// named for the module.
//
// The summary is the joined text's first paragraph, so without a designated lead it is
// whichever file the walk reached first — alphabetically `array.lyra`, which made the
// standard library describe itself as "Combinators over []t." `prelude.lyra` exists to
// hold the opening paragraph, and `leadsModule` is what puts it first.
func TestPrelude_ModuleDocIsJoinedAcrossFiles(t *testing.T) {
	_, table, _ := collectPrelude(t)

	doc := table.ModuleDocs["std.prelude"]
	if doc == nil {
		t.Fatal("the prelude module has no documentation")
	}
	if want := "The standard library's implicitly imported module."; doc.Summary != want {
		t.Errorf("Summary = %q, want %q (prelude.lyra leads)", doc.Summary, want)
	}
	// Every file contributed, joined with a blank line between each.
	for _, fragment := range []string{
		"Combinators over `[]t`.",
		"a value that may be absent",
		"the `Ord` and `Eq` traits",
		"Parsing text into values.",
		"Random number generation.",
		"the language's error channel",
		"rendering a value as text",
		"String helpers",
	} {
		if !strings.Contains(doc.Text, fragment) {
			t.Errorf("the joined module doc is missing %q", fragment)
		}
	}
}
