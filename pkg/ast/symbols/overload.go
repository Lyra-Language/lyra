package symbols

import "github.com/Lyra-Language/lyra/pkg/ast"

// Receiver-keyed overloading — the symbol table's half. `ast.OverloadSet` says what a set
// is and which declarations may form one; this file says where one is *kept*.
//
// **The scope is the authority, and the lookup tables mirror it.** A set is built during
// the walk, when the second declaration of a name meets the first in its module's scope,
// because that is the only moment both are in hand and the alternative — sequential
// rebinding — would otherwise silently discard one. Finish then copies the finished set
// into OverloadSets. Deriving it the other way round (registering each member and
// reconstructing the set afterwards) would need the merge rule in two places, and the two
// would answer differently the first time one of them was taught a new receiver shape.
//
// **An overloaded name is deliberately absent from Functions.** That map answers "which
// declaration does this name mean", and for a set the honest answer is "not decidable
// from a name" — a receiver type is needed. Leaving a member under the bare key would
// make every pass that reads it silently pick one, which is the class of failure the
// whole design is trying to avoid; leaving the key empty makes those passes report the
// callee as unresolved and take their conservative path instead. The passes that must do
// better read the resolved callee the typechecker recorded (typetable.CalleeTable).

// DefineOverload merges decl into the binding this scope already holds for its name,
// returning whether it was accepted and, when it was not, why.
//
// A false with an empty reason means "this is not an overload situation at all" — the
// name is free, or what it holds is not a declaration that could pair with one — and the
// caller should proceed as it always has. A false *with* a reason means the two really
// are two same-named functions and one of the rules refused them, which is a message
// worth showing the reader.
func (s *Scope) DefineOverload(decl *ast.VarDeclStmt) (bool, string) {
	if s == nil || decl == nil {
		return false, ""
	}
	existing, ok := s.LookupLocal(decl.Name)
	if !ok {
		return false, ""
	}
	switch prior := existing.(type) {
	case *ast.OverloadSet:
		reason, ok := ast.OverloadableWith(prior, decl)
		if !ok {
			return false, reason
		}
		prior.Add(decl)
		return true, ""
	case *ast.VarDeclStmt:
		// Neither declaration is a set yet: build one from the pair, and only publish
		// it if the second is admissible — a refusal must leave the scope exactly as it
		// was, so the caller's existing path (sequential rebinding, or a duplicate
		// error) still sees the single declaration it expects.
		set := ast.NewOverloadSet(prior)
		reason, ok := ast.OverloadableWith(set, decl)
		if !ok {
			return false, reason
		}
		set.Add(decl)
		s.Symbols[decl.Name] = set
		return true, ""
	}
	return false, ""
}

// OverloadSetFor returns the set a name resolves to in the module that declared it, as
// the file at loc sees it.
//
// It asks the *scope* rather than the OverloadSets map because registration reads it
// during Finish, before that map is filled — and because the scope is where the merge
// actually happened, so this cannot disagree with it.
func (st *SymbolTable) OverloadSetFor(name string, loc ast.Location) (*ast.OverloadSet, bool) {
	if st == nil {
		return nil, false
	}
	scope := st.moduleScope(st.ModuleOfFile[loc.File])
	if scope == nil {
		return nil, false
	}
	set, ok := scope.LookupLocal(name)
	if !ok {
		return nil, false
	}
	overloads, ok := set.(*ast.OverloadSet)
	return overloads, ok
}

// LookupOverloadsFrom returns the overload set a name means to the file at loc — its own
// module's when it has one, otherwise the one the global or prelude scope holds.
//
// This is the overloaded counterpart of LookupFunctionFrom, and the two are exclusive by
// construction: a name is either one function or a set, never both, so a caller tries
// this one and falls through to the other.
func (st *SymbolTable) LookupOverloadsFrom(name string, loc ast.Location) (*ast.OverloadSet, bool) {
	if st == nil {
		return nil, false
	}
	if set, ok := st.OverloadSets[st.declKey(name, loc)]; ok {
		return set, true
	}
	return nil, false
}

// registerOverloadSet mirrors a scope's finished set into the lookup table, under the
// same key a single function of that name would have taken.
func (st *SymbolTable) registerOverloadSet(key string, set *ast.OverloadSet) {
	if st.OverloadSets == nil {
		st.OverloadSets = make(map[string]*ast.OverloadSet)
	}
	st.OverloadSets[key] = set
	for _, lam := range set.Lambdas() {
		if lam.IsPure {
			st.PureFuncs[key] = lam
		}
	}
}
