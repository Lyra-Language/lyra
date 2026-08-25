package modules

import (
	"sort"

	sitter "github.com/tree-sitter/go-tree-sitter"

	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/cst"
)

// The import graph has to be known *before* collection, because collection needs
// every unit at once — so these read the two module constructs straight off the CST
// rather than going through the collector. They are deliberately shallow: only
// top-level children are examined, which is where the grammar puts both (a module
// declaration must be the first item in a file, and imports are top-level statements).
//
// Reading the CST twice — once here, once during collection — is the cost of resolving
// the graph before walking it. It is cheap next to parsing, and the alternative
// (collect, discover imports, collect again) would mean collecting a file before
// knowing the types it depends on.

// ImportGraph maps each module path to the modules its files import, deduplicated and
// sorted so a consumer reading it cannot depend on unit order.
//
// It exists because the collector needs the graph **before it walks anything**. A
// module's own declaration of an imported name is keyed apart from the imported one
// (symbols.declKeyIn), and a type is registered under that key *during* the walk — so a
// graph assembled per file as it is walked would key the types of a multi-file module by
// whether the file carrying the `import` happened to come first. Handing the whole graph
// over up front, the way the prelude's path already is, removes the ordering question
// rather than answering it.
//
// The units are already parsed, and this reads the same shallow top-level slice of the
// CST that Resolve itself walked, so it costs nothing next to re-deriving the graph.
func ImportGraph(units []Unit) map[string][]string {
	seen := map[string]map[string]bool{}
	for _, u := range units {
		if seen[u.Path] == nil {
			seen[u.Path] = map[string]bool{}
		}
		for _, imp := range importsOf(u) {
			if path := joinPath(imp.Path); path != "" {
				seen[u.Path][path] = true
			}
		}
	}
	graph := make(map[string][]string, len(seen))
	for module, imports := range seen {
		paths := make([]string, 0, len(imports))
		for path := range imports {
			paths = append(paths, path)
		}
		sort.Strings(paths)
		graph[module] = paths
	}
	return graph
}

// importsOf returns the imports declared at the top level of a unit.
func importsOf(u Unit) []*ast.ImportStmt {
	if u.Imports != nil {
		return u.Imports
	}
	return scanImports(u)
}

// scanImports walks the CST for a file's `import` statements. Prefer Unit.Imports, which is
// this result computed once at load; a Unit built by hand (a test) has none and falls here.
func scanImports(u Unit) []*ast.ImportStmt {
	var out []*ast.ImportStmt
	forEachTopLevel(u.Root, "import_statement", func(node *sitter.Node) {
		path := cst.Field(node, "path")
		if path == nil {
			return
		}
		out = append(out, &ast.ImportStmt{
			AstBase: ast.AstBase{Location: nodeLocation(node)},
			Path:    modulePathOf(path, u.Source),
		})
	})
	if out == nil {
		// Non-nil so a file with no imports reads as extracted rather than unvisited.
		out = []*ast.ImportStmt{}
	}
	return out
}

// declaredModulePath returns the path a file declares with `module a.b`, or "".
func declaredModulePath(u Unit) string {
	var path string
	forEachTopLevel(u.Root, "module_declaration", func(node *sitter.Node) {
		if path != "" {
			return // a file declares at most one module; the first wins
		}
		if p := cst.Field(node, "path"); p != nil {
			path = joinPath(modulePathOf(p, u.Source))
		}
	})
	return path
}

func forEachTopLevel(root *sitter.Node, kind string, fn func(*sitter.Node)) {
	if root == nil {
		return
	}
	for i := uint(0); i < root.ChildCount(); i++ {
		child := root.Child(i)
		if child != nil && child.Kind() == kind {
			fn(child)
		}
	}
}

// modulePathOf reads the dotted segments of a module_path node.
func modulePathOf(path *sitter.Node, source []byte) []ast.ModuleName {
	var names []ast.ModuleName
	for i := uint(0); i < path.ChildCount(); i++ {
		child := path.Child(i)
		if child == nil || child.Kind() != "module_name" {
			continue
		}
		names = append(names, ast.ModuleName{Name: string(source[child.StartByte():child.EndByte()])})
	}
	return names
}

// nodeLocation converts tree-sitter's 0-based points to a 1-based ast.Location, the
// same convention the collector uses.
func nodeLocation(node *sitter.Node) ast.Location {
	start, end := node.StartPosition(), node.EndPosition()
	return ast.Location{
		StartLine: int(start.Row) + 1,
		StartCol:  int(start.Column) + 1,
		EndLine:   int(end.Row) + 1,
		EndCol:    int(end.Column) + 1,
	}
}
