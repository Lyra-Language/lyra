package typechecker

import (
	"maps"

	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/ast/symbols"
)

// Name resolution inside a function body has two stores, and this file is the one place
// that says which of them a use means.
//
// Parameters and pattern bindings live in tc.paramTypes, installed per lambda and per arm;
// `let`/`var`/`for` bindings live in the collector's scopes. The collector fills the scopes
// **ahead of time**, so a scope holds every name its block declares — including one declared
// *after* the use being resolved — and sequential rebinding (`let x = x + 1`) leaves only
// the latest declaration in the map, the earlier ones reachable through
// VarDeclStmt.Shadows.
//
// Consulting paramTypes first made a parameter unshadowable: `for c in items { c.x }` read
// the parameter `c`, and so did the `c` after `let c = c.side + 1`, although the
// reassignment diagnostic ("Shadow it instead") promises shadowing works and LANGUAGE.md
// (§ Shadowing a binding) states the rule. Consulting
// the scope first is just as wrong the other way, since a use *before* a later `let` would
// resolve to that `let`. What a use means is positional: the nearest binding **in effect
// there**, which is what the backend's lowering does by construction (it binds each name as
// it reaches the declaration).
//
// So a use walks outward from the current scope through the function-local scopes, and the
// first `let`/`var`/`for` binding already declared at the use's position is the answer. The
// walk stops — handing the use to paramTypes — at the scope a parameter or pattern binding of
// the name was introduced in (paramHome), at the parameter or pattern binding's own entry in
// a scope, or at the first non-local scope (the module's, where order does not matter — a
// non-parameter use that gets there means the module's binding, lookupAt).

// localBindingAt answers the `let`/`var`/`for` binding of name in effect at the use `at`,
// searching only the function-local scopes between the current one and the scope any
// parameter or pattern binding of the name was introduced in. ok is false when there is
// none, and the name means the parameter, the pattern binding, or whatever the ordinary
// scope lookup finds.
//
// A declaration is in effect once it has **ended** before the use begins. That one rule
// covers the cases separately: a use inside the declaration's own initializer
// (`let c = c.side + 1`) is not after it, so it reaches the prior binding — the same-scope
// one through Shadows, or the parameter; a use before a later `let` in the same block is
// not after it either; and a `for` variable's declaration is its name in the header, which
// ends before the body (the iterable is checked outside the loop's scope).
func (tc *TypeChecker) localBindingAt(name string, at ast.Location) (ast.Named, bool) {
	local, _ := tc.walkLocals(name, at)
	return local, local != nil
}

// walkLocals is localBindingAt's walk. When it finds no binding in effect, outside is the
// first scope past the function-local ones if the walk got that far — no parameter,
// pattern binding or home stood in the way — and nil if one did.
func (tc *TypeChecker) walkLocals(name string, at ast.Location) (local ast.Named, outside *symbols.Scope) {
	home := tc.paramHome[name]
	s := tc.scope
	for ; s != nil && s != home && isFunctionLocal(s); s = s.Parent {
		sym, ok := s.Symbols[name]
		if !ok {
			continue
		}
		d, isVar := sym.(*ast.VarDeclStmt)
		if !isVar {
			// A parameter, a pattern binding, or the collector's placeholder for a
			// destructured name — which a successful `let (a, b) = …` overwrites with a
			// typed VarDeclStmt, so one still standing is a binder whose names live in
			// paramTypes (`if let`) or a destructuring that failed. Either way it is the
			// name's own binder, not a later declaration shadowing it.
			return nil, nil
		}
		for ; d != nil; d = d.Shadows {
			if endsBefore(d.GetLocation(), at) {
				return d, nil
			}
		}
	}
	if s == nil || s == home {
		return nil, nil
	}
	return nil, s
}

// paramAt reports whether name, used at `at`, means a parameter or pattern binding — the
// entries in paramTypes/paramMods/patternBound — rather than a local binding declared
// inside it. Every site that consults those maps asks this first, so a shadowing `let`
// changes the type, the write rules and the reassignment rules together.
func (tc *TypeChecker) paramAt(name string, at ast.Location) bool {
	_, typed := tc.paramTypes[name]
	_, moded := tc.paramMods[name]
	if !typed && !moded && !tc.patternBound[name] {
		return false
	}
	_, shadowed := tc.localBindingAt(name, at)
	return !shadowed
}

// lookupAt is tc.scope.Lookup made positional for function-local bindings: the binding in
// effect at the use when there is one, and the scope's answer otherwise. A caller that has
// already asked paramAt calls this for the non-parameter case.
//
// With no local binding in effect and nothing else of the name in the function, the name
// means what the module sees — a top-level `total` read before the block's own `let total`
// is the top-level one. Only when the module has nothing either does the plain lookup
// answer, finding the not-yet-declared local, which is what use-before-declaration
// (lyra-E002) reports.
func (tc *TypeChecker) lookupAt(name string, at ast.Location) (ast.Named, bool) {
	local, outside := tc.walkLocals(name, at)
	if local != nil {
		return local, true
	}
	if outside != nil {
		if sym, ok := outside.Lookup(name); ok {
			return sym, true
		}
	}
	return tc.scope.Lookup(name)
}

// installParamHome records where each name in names was introduced, over a copy of the
// enclosing map, and returns the restore. A nested lambda keeps its enclosing function's
// entries — it sees those parameters — under their own homes.
func (tc *TypeChecker) installParamHome(home *symbols.Scope, names []string) func() {
	old := tc.paramHome
	tc.paramHome = make(map[string]*symbols.Scope, len(old)+len(names))
	maps.Copy(tc.paramHome, old)
	for _, n := range names {
		tc.paramHome[n] = home
	}
	return func() { tc.paramHome = old }
}

// isFunctionLocal reports whether s is a scope inside a function body, where a binding is
// in effect only from its declaration on. A module's scope and those above it are
// order-free, and the walk stops there.
func isFunctionLocal(s *symbols.Scope) bool {
	switch s.Kind {
	case symbols.ScopeFunction, symbols.ScopeBlock, symbols.ScopeLoop:
		return true
	}
	return false
}

// endsBefore reports whether span a ends at or before the start of b, in the same file.
func endsBefore(a, b ast.Location) bool {
	if a.File != b.File || a.EndLine == 0 {
		return false
	}
	return a.EndLine < b.StartLine || (a.EndLine == b.StartLine && a.EndCol <= b.StartCol)
}
