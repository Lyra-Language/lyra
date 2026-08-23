package ast_test

import (
	goast "go/ast"
	"go/parser"
	"go/token"
	"sort"
	"strings"
	"testing"
)

// **The checklist that adding a node kind has never had.**
//
// Rule 8 says a switch over AST node kinds must have a case for every kind that can hold a
// child, and that a missing one is silent with a remote symptom. It has been true more than
// a dozen times. Two node kinds in three days show why saying it is not enough:
//
//   - `ExternDeclStmt` (08/18) was found missing from **ten** switches over top-level
//     declaration kinds, in ten files sharing no package: name resolution, doc attachment,
//     closure captures, docgen, and five LSP surfaces.
//   - `UnsafeBlockExpr` (08/18) was missing from the LSP's expression walker, so hover,
//     go-to-definition and rename silently returned nothing inside `unsafe { … }` — the
//     whole of a program's FFI and raw-pointer code. Chasing that found **thirteen more**
//     expression kinds missing from the same switch, including `ForLoopExpr`, so
//     navigation was dead inside every loop body in every program.
//
// None of that is a failure of care. It is that "grep for the switches over it" requires
// knowing which switches exist, and nothing enumerates them. These tests do: each one names
// a switch and the set it must be exhaustive over, so a new node kind fails here — at the
// edit, with a message naming the file — rather than months later as a feature that quietly
// does nothing.
//
// **What this does not do.** It cannot find a switch nobody registered. Adding an entry
// below when you add a consumer is still a manual act; what it buys is that adding a *node*
// is not.

// canonicalWalkers are the two switches in this package that define what a node's children
// are. Everything else that walks the AST is measured against them.
const (
	walkFile     = "walk.go"
	walkExprFunc = "walkExprChildren"
	walkStmtFunc = "walkStmtChildren"
	repoRoot     = "../.."
)

// mirrors are switches elsewhere that must cover every case a canonical walker has.
//
// A mirror may legitimately omit a kind — `scopeInExpr` only cares about the constructs
// that *introduce a scope* — so each entry carries its exclusions, in writing, next to the
// reason. An exclusion is a claim; an omission is a bug.
var mirrors = []struct {
	name      string // what to call it in a failure
	file, fn  string
	canonical string // walkExprFunc or walkStmtFunc
	excused   map[string]string
}{
	{
		name:      "the LSP's expression walker",
		file:      repoRoot + "/cmd/lyra-lsp/hover.go",
		fn:        "findInChildren",
		canonical: walkExprFunc,
		excused:   map[string]string{
			// Every position-based LSP feature — hover, definition, rename, highlight —
			// descends through this switch, so a kind missing from it is that construct
			// being invisible to all of them at once. There is no construct a user does
			// not navigate inside, which is why nothing is excused here.
		},
	},
}

// TestMirrorWalkersAreExhaustive fails when a switch that mirrors one of this package's
// canonical walkers has fallen behind it.
func TestMirrorWalkersAreExhaustive(t *testing.T) {
	canonical := map[string]map[string]bool{
		walkExprFunc: switchCases(t, walkFile, walkExprFunc),
		walkStmtFunc: switchCases(t, walkFile, walkStmtFunc),
	}
	for _, m := range mirrors {
		t.Run(m.fn, func(t *testing.T) {
			have := switchCases(t, m.file, m.fn)
			var missing []string
			for kind := range canonical[m.canonical] {
				if have[kind] || m.excused[kind] != "" {
					continue
				}
				missing = append(missing, kind)
			}
			sort.Strings(missing)
			if len(missing) > 0 {
				t.Errorf("%s (%s, %s) has no case for %d node kind(s) that %s handles:\n  %s\n\n"+
					"A missing case here is silent: the construct is simply skipped, and the "+
					"symptom is a feature doing nothing in that syntax. Add the case, or excuse "+
					"it in `mirrors` with the reason.",
					m.name, m.file, m.fn, len(missing), m.canonical, strings.Join(missing, "\n  "))
			}
			// The reverse is a smaller problem but still drift: a kind the mirror handles
			// and the canonical walker does not means the canonical one is behind.
			for kind := range have {
				if !canonical[m.canonical][kind] && m.excused[kind] == "" {
					t.Errorf("%s handles %s, which %s does not — the canonical walker is behind",
						m.name, kind, m.canonical)
				}
			}
		})
	}
}

// declarationConsumers are the switches that must handle every **top-level declaration
// kind**. This is the family `ExternDeclStmt` was missing from ten times.
var declarationConsumers = []struct {
	name, file, fn string
	excused        map[string]string
}{
	{"symbol-table visibility", repoRoot + "/pkg/ast/symbols/table.go", "declIsPublic", map[string]string{
		// Both fall to the documented default — "an unrecognised node counts as
		// exported", the conservative direction, since a wrongly *qualified* key hides a
		// declaration from every module including its own. Excused rather than added
		// because writing `return true` explicitly would say the opposite of what the
		// default's comment argues. This is exactly the judgment the test exists to
		// force into the open: `ExternDeclStmt` took the same default and was **wrong**
		// to, which is how two modules each declaring `extern abs` came to collide.
		"TraitImplStmt":  "an impl has no `pub` of its own — its visibility is the trait's and the type's",
		"ModuleDeclStmt": "a module header declares no name to key",
	}},
	{"doc attachment", repoRoot + "/pkg/analyzer/collector/collector.go", "attachDoc", nil},
	{"LSP hover docs", repoRoot + "/cmd/lyra-lsp/hoverdoc.go", "docOf", map[string]string{
		"ModuleDeclStmt": "a module's doc is rendered by docgen, not hovered — there is no name to hover",
		"TraitImplStmt":  "an impl has no name to hover; its methods are hovered individually",
	}},
	{"closure captures", repoRoot + "/pkg/analyzer/captures/captures.go", "globalNames", map[string]string{
		"ModuleDeclStmt": "declares no value a lambda body could reference",
		"TraitDeclStmt":  "a trait's name is a type; types are collected from the symbol table below",
		"TraitImplStmt":  "an impl declares no name at all",
	}},
	{"documentation model", repoRoot + "/pkg/docgen/docgen.go", "declFor", map[string]string{
		"ModuleDeclStmt": "the module's own doc is the page header, not a declaration on it",
	}},
	{"LSP document outline", repoRoot + "/cmd/lyra-lsp/documentsymbol.go", "stmtToSymbol", map[string]string{
		"ModuleDeclStmt": "the file's module header is not a symbol in it",
		"TraitImplStmt":  "an impl has no name; its methods would be the symbols, and are not listed yet",
	}},
	{"LSP workspace symbols", repoRoot + "/cmd/lyra-lsp/workspacesymbols.go", "stmtToSymbolInfo", map[string]string{
		"ModuleDeclStmt": "not a searchable symbol",
		"TraitImplStmt":  "an impl has no name to search for",
	}},
	{"LSP rename anchors", repoRoot + "/cmd/lyra-lsp/rename.go", "namedNameLoc", map[string]string{
		"ModuleDeclStmt": "a module is renamed by moving the file, not by this server",
		"TraitImplStmt":  "an impl has no name of its own to rename",
	}},
	// Registered 08/22, after `ExternDeclStmt` and `TraitDeclStmt` were both found
	// missing from it — an eleventh and twelfth instance of the family, and the first
	// found by using the language rather than by the sweep. The question it answers is
	// narrower than the others': which declarations mention a type somewhere the
	// *expression* walk cannot reach. A LambdaExpr's signature it already sees; a
	// `*types.LambdaType` hanging off a declaration it does not.
	{"unused-import references", repoRoot + "/pkg/analyzer/checker/unused_imports.go", "collectRefsByFile", map[string]string{
		"TraitImplStmt":  "an impl's methods are LambdaExprs, reached by the expression walk below",
		"ModuleDeclStmt": "a module header names no type",
	}},
}

// declarationKinds is every AST node that declares a name at the top level of a program.
//
// **Adding one here is the checklist.** Every consumer above then fails until it has a case
// or an excuse, which is the whole mechanism: the tax that used to be paid weeks later, in
// features that silently did nothing, is paid at the edit instead.
var declarationKinds = []string{
	"VarDeclStmt",
	"TypeDeclStmt",
	"TraitDeclStmt",
	"TraitImplStmt",
	"ExternDeclStmt",
	"ModuleDeclStmt",
}

// TestDeclarationKindsAreComplete guards the list above against the failure mode it exists
// to prevent — someone adding a declaration node and not listing it here, which would leave
// every consumer test passing vacuously.
//
// The definition it checks against is **carrying a `Doc` field**: documentation attaches to
// declarations and to nothing else (see CLAUDE.md), so a statement node with one is a
// declaration by the language's own rule rather than by this file's opinion.
func TestDeclarationKindsAreComplete(t *testing.T) {
	listed := map[string]bool{}
	for _, k := range declarationKinds {
		listed[k] = true
	}
	for name, fields := range structFields(t) {
		hasDoc := false
		for _, f := range fields {
			if f == "Doc" {
				hasDoc = true
			}
		}
		if !hasDoc || !strings.HasSuffix(name, "Stmt") {
			continue
		}
		if !listed[name] {
			t.Errorf("%s carries a Doc field, so it is a declaration, but is not in "+
				"declarationKinds.\n\nAdd it there, then give every consumer in "+
				"declarationConsumers a case for it (or an excuse with a reason). That is "+
				"the sweep this file exists to make automatic.", name)
		}
	}
}

// TestDeclarationConsumersAreExhaustive is the sweep: every registered switch must handle
// every declaration kind.
func TestDeclarationConsumersAreExhaustive(t *testing.T) {
	for _, c := range declarationConsumers {
		t.Run(c.fn, func(t *testing.T) {
			have := switchCases(t, c.file, c.fn)
			if len(have) == 0 {
				t.Fatalf("no type switch found in %s (%s) — the entry in declarationConsumers "+
					"is stale, which makes this check pass while testing nothing", c.file, c.fn)
			}
			for _, kind := range declarationKinds {
				if have[kind] || c.excused[kind] != "" {
					continue
				}
				t.Errorf("%s (%s, %s) has no case for %s.\n\n"+
					"A missing declaration kind here is silent — the declaration is skipped and "+
					"the feature quietly omits it. Add the case, or excuse it in "+
					"declarationConsumers with the reason it does not apply.",
					c.name, c.file, c.fn, kind)
			}
		})
	}
}

// switchCases returns the set of type names appearing as type-switch cases anywhere in the
// named function, with any `*` and package qualifier stripped so `*ast.BlockExpr` and
// `*BlockExpr` compare equal.
//
// Source parsing rather than reflection because the question is about *code*: reflection
// can say what fields a node has and never what a switch does with it.
func switchCases(t *testing.T, file, fn string) map[string]bool {
	t.Helper()
	fset := token.NewFileSet()
	parsed, err := parser.ParseFile(fset, file, nil, 0)
	if err != nil {
		t.Fatalf("parsing %s: %v", file, err)
	}
	out := map[string]bool{}
	goast.Inspect(parsed, func(n goast.Node) bool {
		decl, ok := n.(*goast.FuncDecl)
		if !ok || decl.Name.Name != fn {
			return true
		}
		goast.Inspect(decl, func(m goast.Node) bool {
			if cc, ok := m.(*goast.CaseClause); ok {
				for _, e := range cc.List {
					if name := bareTypeName(e); name != "" {
						out[name] = true
					}
				}
			}
			return true
		})
		return false
	})
	return out
}

// bareTypeName renders a case's type as its unqualified name, or "" for anything that is
// not a type name (a value case in a non-type switch).
func bareTypeName(e goast.Expr) string {
	switch v := e.(type) {
	case *goast.Ident:
		if v.Name == "nil" {
			return ""
		}
		return v.Name
	case *goast.StarExpr:
		return bareTypeName(v.X)
	case *goast.SelectorExpr:
		return v.Sel.Name
	}
	return ""
}

// structFields maps each struct type declared in this package to its field names.
func structFields(t *testing.T) map[string][]string {
	t.Helper()
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", nil, 0)
	if err != nil {
		t.Fatalf("parsing pkg/ast: %v", err)
	}
	out := map[string][]string{}
	for _, p := range pkgs {
		for _, f := range p.Files {
			goast.Inspect(f, func(n goast.Node) bool {
				ts, ok := n.(*goast.TypeSpec)
				if !ok {
					return true
				}
				st, ok := ts.Type.(*goast.StructType)
				if !ok {
					return true
				}
				for _, field := range st.Fields.List {
					for _, name := range field.Names {
						out[ts.Name.Name] = append(out[ts.Name.Name], name.Name)
					}
				}
				return true
			})
		}
	}
	return out
}
