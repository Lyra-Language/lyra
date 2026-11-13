package driver

import (
	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/ast/symbols"
)

// eraseTraitAliases rewrites every trait named under an import's alias — `import shapes.{
// Shape as Figure }` — to the trait's own name, in the modules that import it so, before any
// pass reads the program: an impl's head (`impl Figure for Square`), a bound (`where s:
// Figure`, on a function, a type, a trait or an impl), a supertrait, and a `Figure::area` path.
//
// **A desugaring, as eraseConstructorAliases is**, for the same reason: a trait is resolved
// by its declaration (symbols.declKeyIn follows the alias), but compared by name wherever an
// impl is matched to a bound — the impl registered under `Figure` and the bound asking for
// `Shape` were two traits, so the impl satisfied nothing (lyra-E036) and the backend found
// no impl to call (11/13). Rewriting the spelling once, here, makes every comparison see one
// name. The import gate reads the names as written (TypeRefs, recorded by the collector), so
// it still sees the alias the program wrote. A module declaring the alias itself keeps its
// own, as it would over any import.
//
// It returns, per file, the declared names of the traits it rewrote there, which is what
// counts the import as used (lyra-W004): after it, `impl Figure` reads `impl Shape`.
func eraseTraitAliases(program *ast.Program, st *symbols.SymbolTable) map[string]map[string]bool {
	aliases := traitAliases(st)
	if len(aliases) == 0 {
		return nil
	}
	used := map[string]map[string]bool{}
	rename := func(name *string, loc ast.Location) {
		original, ok := aliases[st.ModuleOfFile[loc.File]][*name]
		if !ok {
			return
		}
		*name = original
		if used[loc.File] == nil {
			used[loc.File] = map[string]bool{}
		}
		used[loc.File][original] = true
	}
	// A bound list can be shared — a lambda's GenericBounds holds its binding's
	// GenericParams' slices — and renaming one twice would undo a swap (`A as B, B as A`).
	done := map[*string]bool{}
	renameAll := func(names []string, loc ast.Location) {
		for i := range names {
			if !done[&names[i]] {
				done[&names[i]] = true
				rename(&names[i], loc)
			}
		}
	}
	renameParams := func(params []ast.GenericParam, loc ast.Location) {
		for i := range params {
			renameAll(params[i].Constraints, loc)
		}
	}
	for _, node := range program.Statements {
		stmt, ok := node.(ast.Statement)
		if !ok {
			continue
		}
		ast.WalkStmt(stmt, func(s ast.Statement) bool {
			loc := s.GetLocation()
			switch d := s.(type) {
			case *ast.TraitImplStmt:
				if !done[&d.TraitName] {
					done[&d.TraitName] = true
					rename(&d.TraitName, loc)
				}
				renameParams(d.GenericParams, loc)
				for i := range d.Constraints {
					renameAll(d.Constraints[i].TraitBounds, loc)
				}
			case *ast.TraitDeclStmt:
				renameAll(d.Bounds, loc)
				renameParams(d.GenericParams, loc)
			case *ast.TypeDeclStmt:
				renameParams(d.GenericParams, loc)
			case *ast.VarDeclStmt:
				renameParams(d.GenericParams, loc)
			}
			return true
		}, func(e ast.Expression) bool {
			loc := e.GetLocation()
			switch ex := e.(type) {
			case *ast.LambdaExpr:
				renameParams(ex.GenericParams, loc)
				renameParams(ex.ImplConstParams, loc)
				for _, bounds := range ex.GenericBounds {
					renameAll(bounds, loc)
				}
			case *ast.TraitMethodPathExpr:
				rename(&ex.TraitName, loc)
			}
			return true
		})
	}
	return used
}

// traitAliases is, per importing module, each alias a selective import gives a trait of the
// imported module, mapped to the trait's own name.
func traitAliases(st *symbols.SymbolTable) map[string]map[string]string {
	if st == nil {
		return nil
	}
	var out map[string]map[string]string
	for module, imports := range st.Imports {
		for _, imp := range imports {
			if imp.IsNamespace() {
				continue
			}
			for local, original := range imp.Members {
				if local == original {
					continue
				}
				if _, ok := st.LookupTraitIn(imp.Path, original); !ok {
					continue
				}
				// This module's own name wins, as it does over any import.
				if st.ModuleDeclares(module, local) {
					continue
				}
				if out == nil {
					out = map[string]map[string]string{}
				}
				if out[module] == nil {
					out[module] = map[string]string{}
				}
				out[module][local] = original
			}
		}
	}
	return out
}
