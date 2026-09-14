package main

import (
	"strings"

	"github.com/Lyra-Language/lyra/pkg/ast"
)

// Finding every use of a **name**, in every file of the program, for references and rename.
//
// Types had this from the start, because the collector indexes every written type name
// with its file. A binding has no such index: its uses are identifier expressions, and the
// server walked only the open document's statements to find them. So "find references" on a
// function reported the one file being looked at, and renaming a function used by a sibling
// file edited that file alone and left the rest of the program calling a name that no longer
// existed — while a cross-file declaration declined the rename silently.
//
// The walk here is the single-file one, run once per file. Each file gets a *view* — its own
// statements over the shared tables — so a position resolves exactly as it would were that
// file the open document, and an occurrence is kept only when it resolves to the same
// declaration the cursor did. Matching by name alone would take another module's same-named
// function.

// programViews splits an analysis into one per unit file, each carrying only that file's
// statements and its own module scope. An analysis with no whole program (the single-unit
// fallback for an unsaved buffer) is its own only view.
func programViews(analysis *docAnalysis) []*docAnalysis {
	if analysis.fullProgram == nil || analysis.file == "" {
		return []*docAnalysis{analysis}
	}
	var files []string
	seen := map[string]bool{}
	for _, stmt := range analysis.fullProgram.Statements {
		if stmt == nil {
			continue
		}
		f := stmt.GetLocation().File
		if f == "" || seen[f] {
			continue
		}
		seen[f] = true
		files = append(files, f)
	}
	views := make([]*docAnalysis, 0, len(files))
	for _, f := range files {
		v := *analysis
		v.program = docProgram(analysis.fullProgram, f)
		v.file = f
		v.moduleScope = moduleScopeOf(analysis.symTable, f)
		views = append(views, &v)
	}
	return views
}

// bindingOccurrences finds every use of the top-level or local binding named — the
// declaration `decl` resolves to — across views. Three spellings count:
//
//   - an identifier that resolves to it;
//   - a member reached through a namespace import of the declaring module, `shapes.double`;
//   - the property of a method-style call the typechecker resolved to its function,
//     `n.double()`. Neither of these is an identifier, and a rename that skipped them would
//     leave calls to a function that no longer exists;
//   - a member of an `import m.{ name }` naming the declaring module, for the same reason.
//
// The declaration's own name is not included; callers add it.
func bindingOccurrences(views []*docAnalysis, name string, decl ast.Named) []ast.Location {
	declLoc := decl.GetLocation()
	var fnLoc ast.Location
	if vd, ok := decl.(*ast.VarDeclStmt); ok {
		if lam, ok := vd.Value.(*ast.LambdaExpr); ok {
			fnLoc = lam.GetLocation()
		}
	}
	module := topLevelModule(views, name, decl)
	var out []ast.Location
	for _, v := range views {
		walkExprs(v.program, func(e ast.Expression) {
			switch x := e.(type) {
			case *ast.MemberExpr:
				// `shapes.double`: the typechecker resolves a namespace member without
				// recording a callee, so it is matched here by what it names — the
				// namespace must be an import of the declaring module, and not a local
				// binding that happens to share the import's name.
				ns, ok := x.Object.(*ast.IdentifierExpr)
				if !ok || x.Property.Name != name || module == "" || v.symTable == nil {
					return
				}
				if _, isBinding := resolveDeclNamed(ns.Name, ns.GetLocation().StartLine, ns.GetLocation().StartCol, v); isBinding {
					return
				}
				if imp, ok := v.symTable.NamespaceImport(v.file, ns.Name); ok && imp.Path == module {
					out = append(out, x.Property.GetLocation())
				}
			case *ast.IdentifierExpr:
				if x.Name != name {
					return
				}
				if dl, ok := resolveDeclLocation(name, x.GetLocation().StartLine, x.GetLocation().StartCol, v); ok && dl == declLoc {
					out = append(out, x.GetLocation())
				}
			case *ast.FunctionCallExpr:
				m, ok := x.Function.(*ast.MemberExpr)
				if !ok || m.Property.Name != name || fnLoc == (ast.Location{}) || v.typeTable == nil {
					return
				}
				if fn, ok := v.typeTable.Callee(x); ok && fn != nil && fn.GetLocation() == fnLoc {
					out = append(out, m.Property.GetLocation())
				}
			}
		})
	}
	return append(out, importMemberOccurrences(views, name, declLoc.File)...)
}

// topLevelModule is the module decl is a top-level binding of, or "" when it is not one —
// a local cannot be reached through a namespace, whatever its name.
func topLevelModule(views []*docAnalysis, name string, decl ast.Named) string {
	if len(views) == 0 || views[0].symTable == nil {
		return ""
	}
	st := views[0].symTable
	module := st.ModuleOfFile[decl.GetLocation().File]
	scope := st.ModuleScopes[module]
	if module == "" || scope == nil {
		return ""
	}
	if top, ok := scope.Symbols[name]; ok && top.GetLocation() == decl.GetLocation() {
		return module
	}
	return ""
}

// importMemberOccurrences finds `import m.{ name }` members that name a declaration of the
// module declared in declFile. The member's own span covers an alias too, so it is narrowed
// to the name: `import m.{ double as d }` renames `double` and leaves `d` and its uses alone.
func importMemberOccurrences(views []*docAnalysis, name, declFile string) []ast.Location {
	if len(views) == 0 || views[0].symTable == nil || declFile == "" {
		return nil
	}
	module := views[0].symTable.ModuleOfFile[declFile]
	if module == "" {
		return nil
	}
	var out []ast.Location
	for _, v := range views {
		for _, node := range v.program.Statements {
			imp, ok := node.(*ast.ImportStmt)
			if !ok || importPath(imp) != module {
				continue
			}
			for _, m := range imp.Members {
				if m.Name == name && m.Location != (ast.Location{}) {
					out = append(out, nameSpanAt(m.Location, name))
				}
			}
		}
	}
	return out
}

// importPath is an import's module path as the symbol table keys modules: `a.b.c`.
func importPath(imp *ast.ImportStmt) string {
	parts := make([]string, len(imp.Path))
	for i, p := range imp.Path {
		parts[i] = p.Name
	}
	return strings.Join(parts, ".")
}

// resolveDeclNamed is resolveDeclLocation's declaration rather than its location.
func resolveDeclNamed(name string, line, col int, analysis *docAnalysis) (ast.Named, bool) {
	scope := findScopeAtPos(analysis.program, analysis.scopeTable, analysis.fileScope(), line, col)
	return scope.Lookup(name)
}

// isStdFile reports whether a declaration lives in the standard library, which a rename
// must not edit: every program on the machine depends on it, and none of them is in the
// workspace being searched.
func isStdFile(analysis *docAnalysis, file string) bool {
	if analysis.symTable == nil || file == "" {
		return false
	}
	module := analysis.symTable.ModuleOfFile[file]
	return module == "std" || strings.HasPrefix(module, "std.")
}
