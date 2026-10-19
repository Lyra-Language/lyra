package driver

import (
	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/ast/symbols"
)

// eraseConstructorAliases rewrites every use of a constructor imported under an alias —
// `import owner.{ OfMap as Theirs }` — to the constructor's own name, in the modules that
// import it so, before any pass reads the program. It returns, per file, the declared names
// of the constructors it rewrote there, which is what counts the import as used (lyra-W004).
//
// **A desugaring, as an `impl` block is**, because a constructor is resolved by name in a
// dozen places (construction, payloads, patterns, exhaustiveness, the backend's tags), each
// of which compares `c.Name` against the declaration — and an alias honoured at one of them
// and not another is a program that checks and then miscompiles. Until 10/19 none honoured
// it: `Theirs` was lyra-E035 "undefined constructor or type" and `Big(3)` "undefined tuple
// type", while a type or a function took an alias like any import.
//
// The rewritten name resolves through the import (symbols.importsConstructor admits a
// member by its original name), so writing the original name in such a module resolves too;
// the alias is what the program is expected to say. A module that declares its own name the
// alias would take is left alone — its own declaration wins, as it does over any import.
func eraseConstructorAliases(program *ast.Program, st *symbols.SymbolTable) map[string]map[string]bool {
	aliases := constructorAliases(st)
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
	renamePatterns := func(node ast.AstNode) {
		for _, p := range ast.PatternsOf(node) {
			ast.WalkPattern(p, func(q ast.Pattern) bool {
				if dp, ok := q.(*ast.DataPattern); ok {
					rename(&dp.Name, dp.GetLocation())
				}
				return true
			})
		}
	}
	for _, node := range program.Statements {
		stmt, ok := node.(ast.Statement)
		if !ok {
			continue
		}
		ast.WalkStmt(stmt, func(s ast.Statement) bool {
			renamePatterns(s)
			return true
		}, func(e ast.Expression) bool {
			switch ex := e.(type) {
			case *ast.DataConstructorExpr:
				rename(&ex.Constructor, ex.GetLocation())
			case *ast.TupleLiteralExpr:
				// An applied constructor, `Big(3)`, is this node kind.
				rename(&ex.Name, ex.GetLocation())
			}
			renamePatterns(e)
			return true
		})
	}
	return used
}

// constructorAliases is, per importing module, each alias a selective import gives a
// constructor of the imported module, mapped to the constructor's own name.
func constructorAliases(st *symbols.SymbolTable) map[string]map[string]string {
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
				if local == original || !declaresConstructor(st, imp.Path, original) {
					continue
				}
				// This module's own name wins, as it does over any import.
				if st.ModuleDeclares(module, local) || declaresConstructor(st, module, local) {
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

// declaresConstructor reports whether one of module's own data types has a constructor
// named name.
func declaresConstructor(st *symbols.SymbolTable, module, name string) bool {
	_, ok := ownConstructor(st, module, name)
	return ok
}
