package symbols

import "github.com/Lyra-Language/lyra/pkg/ast"

// Clone returns a deep copy of the table's own mutable state, sharing the AST it points at.
//
// **It exists so an editor can collect the unchanged part of a program once.** A language
// server re-analyzes the whole import graph on every keystroke, and for a small file with
// the standard prelude that is 12 units of which 11 cannot have changed — 99% of the work.
// Collection folds every unit into one Program and one SymbolTable, so the reusable thing is
// the *state after the unchanged prefix*, and reuse means cloning it rather than mutating
// it: the master stays pristine and each keystroke gets a fresh copy to add the edited file
// to. Undoing a mutation would be the alternative, and it has to be exactly right every time
// where a clone has to be right once.
//
// **The AST is shared, not copied**, which is both necessary and safe. Necessary because
// ScopeTable, TypeTable and MethodTable are all keyed by AST *pointer*, so copying nodes
// would invalidate every side table that refers to them. Safe because re-running the
// analysis passes over one collected AST is idempotent — checked directly, over the real
// prelude, in the driver's re-analysis test. That is worth stating because it is not
// obvious: the typechecker does rewrite the AST it analyzes (`desugarClauses` replaces a
// multi-clause body with a match and clears the clauses), and the reason that is not a
// problem is that the rewrite leaves a shape the next run reads the same way, not that the
// rewrite does not happen.
//
// The scope graph is cyclic (Parent/Children), so cloning goes through a visited map and
// every pointer into it — CurrentScope, ModuleScopes, the ScopeTable's values — is remapped
// through the same map, or two views of one scope would drift apart.
func (st *SymbolTable) Clone() (*SymbolTable, map[*Scope]*Scope) {
	if st == nil {
		return nil, nil
	}
	seen := map[*Scope]*Scope{}
	out := &SymbolTable{
		GlobalScope:     cloneScope(st.GlobalScope, seen),
		CurrentScope:    cloneScope(st.CurrentScope, seen),
		PreludeScope:    cloneScope(st.PreludeScope, seen),
		Types:           cloneMap(st.Types),
		Functions:       cloneMap(st.Functions),
		Traits:          cloneMap(st.Traits),
		ModuleOf:        cloneMap(st.ModuleOf),
		ModuleOfFile:    cloneMap(st.ModuleOfFile),
		PreludeModule:   st.PreludeModule,
		ModuleDocs:      cloneMap(st.ModuleDocs),
		Shadowed:        append([]ShadowedName(nil), st.Shadowed...),
		ImportedModules: cloneMapOfSlice(st.ImportedModules),
		PreludeNames:    cloneMap(st.PreludeNames),
		ModuleScopes:    cloneScopeMap(st.ModuleScopes, seen),
		ImportScopes:    cloneScopeMap(st.ImportScopes, seen),
		Imports:         cloneImports(st.Imports),
		OverloadSets:    cloneMap(st.OverloadSets),
	}
	return out, seen
}

// cloneScope copies a scope and everything reachable from it, memoized on `seen` so the
// Parent/Children cycle terminates and so two paths to one scope yield one copy.
func cloneScope(s *Scope, seen map[*Scope]*Scope) *Scope {
	if s == nil {
		return nil
	}
	if c, ok := seen[s]; ok {
		return c
	}
	c := &Scope{Kind: s.Kind, Symbols: make(map[string]ast.Named, len(s.Symbols))}
	// Registered before recursing, so a child reaching back through Parent finds this
	// copy rather than starting a second one.
	seen[s] = c
	for k, v := range s.Symbols {
		c.Symbols[k] = v
	}
	c.Parent = cloneScope(s.Parent, seen)
	c.Children = make([]*Scope, 0, len(s.Children))
	for _, ch := range s.Children {
		c.Children = append(c.Children, cloneScope(ch, seen))
	}
	return c
}

func cloneScopeMap(in map[string]*Scope, seen map[*Scope]*Scope) map[string]*Scope {
	out := make(map[string]*Scope, len(in))
	for k, v := range in {
		out[k] = cloneScope(v, seen)
	}
	return out
}

func cloneMap[K comparable, V any](in map[K]V) map[K]V {
	out := make(map[K]V, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func cloneMapOfSlice[K comparable, V any](in map[K][]V) map[K][]V {
	out := make(map[K][]V, len(in))
	for k, v := range in {
		out[k] = append([]V(nil), v...)
	}
	return out
}

// cloneImports copies the import records, including each one's Members map — an import's
// member list is mutated as a file's imports are collected, so sharing it would let one
// clone's imports appear in another's.
func cloneImports(in map[string][]Import) map[string][]Import {
	out := make(map[string][]Import, len(in))
	for k, list := range in {
		cp := make([]Import, len(list))
		for i, imp := range list {
			cp[i] = imp
			cp[i].Members = cloneMap(imp.Members)
		}
		out[k] = cp
	}
	return out
}

// CloneWith copies the scope table, remapping every scope through the map Clone returned so
// the two tables describe one graph rather than two.
func (st *ScopeTable) CloneWith(seen map[*Scope]*Scope) *ScopeTable {
	if st == nil {
		return nil
	}
	out := &ScopeTable{entries: make(map[ast.AstNode]*Scope, len(st.entries))}
	for node, scope := range st.entries {
		if c, ok := seen[scope]; ok {
			out.entries[node] = c
			continue
		}
		// A scope the symbol table could not reach — nothing points at it but this entry,
		// so it is cloned here and memoized for any sibling entry naming it.
		out.entries[node] = cloneScope(scope, seen)
	}
	return out
}
