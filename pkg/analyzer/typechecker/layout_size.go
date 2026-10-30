package typechecker

import (
	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/types"
)

// ByValueSize is the bytes a value of t, written at loc, takes by value — what a bare
// parameter of it copies (lyra-W031) — and ok=false where that is not known: a type
// variable, a name that does not resolve. The engine is the backend's own
// (types.SizeAndAlignResolving); what this adds is resolution as layout needs it, each
// struct's, union's and `data` type's components from **its declaration** (rule 4's
// declarationSite, as hasCLayout reads them), newtypes seen through and a generic
// aggregate instantiated.
func (tc *TypeChecker) ByValueSize(t types.Type, loc ast.Location) (int, bool) {
	size, _, ok := types.SizeAndAlignResolving(t, tc.layoutResolver(loc))
	return size, ok
}

func (tc *TypeChecker) layoutResolver(loc ast.Location) types.Resolver {
	return func(t types.Type) (types.Type, types.Resolver) {
		// A generic aggregate's instance (`Pair<[32]i64>`) is its declaration with the
		// arguments substituted, as field access reads it.
		resolved := tc.resolveGenericAggregate(tc.stripNewtypeResolving(t, loc), loc)
		var name, key string
		switch v := resolved.(type) {
		case types.NamedStructType:
			name, key = v.Name, v.Key
		case types.UnionType:
			name, key = v.Name, v.Key
		case types.DataType:
			name, key = v.Name, v.Key
		default:
			return resolved, tc.layoutResolver(loc)
		}
		if decl, ok := tc.symTable.LookupTypeRef(name, key, loc); ok && decl != nil {
			return resolved, tc.layoutResolver(decl.GetLocation())
		}
		return resolved, tc.layoutResolver(loc)
	}
}
