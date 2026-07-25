package checker

import (
	"fmt"

	"github.com/Lyra-Language/lyra/pkg/ast"
	diag "github.com/Lyra-Language/lyra/pkg/diagnostic"
	"github.com/Lyra-Language/lyra/pkg/types"
)

// RecursiveTypeError is emitted when a struct, data type, or named tuple
// contains itself by value (directly or through a cycle of by-value
// references), which would give it unbounded size.
type RecursiveTypeError struct {
	Code     string
	Message  string
	Location ast.Location
}

func (e RecursiveTypeError) Error() string { return e.Message }

// CheckRecursiveTypes walks all type declarations in the program and reports
// any by-value recursive cycle. A cycle is safe only when a recursive
// field/constructor-parameter is annotated `shared` (heap-allocated,
// pointer-sized), which bounds the size — there is no declaration-level
// allocation flavor, so a `shared` field is the only cycle-breaker.
//
// The check does not require the typechecker output; it runs directly on the
// collected AST.
func CheckRecursiveTypes(program *ast.Program) []RecursiveTypeError {
	// Build a name→decl index of all struct/data/named-tuple declarations.
	decls := make(map[string]*ast.TypeDeclStmt)
	for _, s := range program.Statements {
		td, ok := s.(*ast.TypeDeclStmt)
		if !ok {
			continue
		}
		switch td.Type.(type) {
		case types.NamedStructType, types.DataType, types.TupleType:
			decls[td.Name] = td
		}
	}

	// Build a by-value dependency graph: for each type name, which other
	// user-defined type names does it contain by value (no shared indirection)?
	// We unwrap the declaration's own type here so that a NamedStructType's
	// *name* is never added as a dependency on itself — only its *field types*.
	byValueDeps := make(map[string][]string, len(decls))
	for name, decl := range decls {
		deps := byValueDepsOf(decl, decls)
		if len(deps) > 0 {
			byValueDeps[name] = deps
		}
	}

	// DFS cycle detection using three-color marking.
	//   white (0) — unvisited
	//   gray  (1) — on the current DFS path (in-stack)
	//   black (2) — fully processed; no new cycles reachable
	const (
		white = 0
		gray  = 1
		black = 2
	)
	color := make(map[string]int, len(decls))
	reported := make(map[string]bool)
	var errors []RecursiveTypeError

	var dfs func(name string)
	dfs = func(name string) {
		color[name] = gray
		for _, dep := range byValueDeps[name] {
			switch color[dep] {
			case white:
				dfs(dep)
			case gray:
				// Back-edge: `dep` is an ancestor on the current path — found
				// a by-value cycle rooted at `dep`. Report on `dep`'s
				// declaration, once.
				if !reported[dep] {
					reported[dep] = true
					decl := decls[dep]
					errors = append(errors, RecursiveTypeError{
						Code: diag.CodeRecursiveType,
						Message: fmt.Sprintf(
							"type %q is recursive without indirection: by-value self-reference gives it unbounded size; "+
								"annotate the recursive field/constructor parameter as `shared`",
							dep),
						Location: decl.GetLocation(),
					})
				}
				// black: already processed, no new cycle through this path.
			}
		}
		color[name] = black
	}

	for name := range decls {
		if color[name] == white {
			dfs(name)
		}
	}

	return errors
}

// byValueDepsOf returns the names of user-defined types that decl contains
// directly by value (not through shared or raw-pointer indirection). A cycle is
// broken only where a *field* takes the type by shared indirection (`next:
// shared Node`) — there is no declaration-level allocation flavor, so the whole
// type is always laid out by value here and only its fields' flavors matter.
func byValueDepsOf(decl *ast.TypeDeclStmt, decls map[string]*ast.TypeDeclStmt) []string {
	seen := map[string]bool{}
	var deps []string

	// Unwrap the top-level type and walk its *contents* — fields, constructor
	// params, or tuple elements — not the type value itself.
	switch t := decl.Type.(type) {
	case types.NamedStructType:
		for _, f := range t.Fields {
			collectByValueNames(f.Type, decls, seen, &deps)
		}
	case types.DataType:
		for _, ctor := range t.Constructors {
			for _, param := range ctor.Params {
				collectByValueNames(param, decls, seen, &deps)
			}
		}
	case types.TupleType:
		for _, e := range t.Elements {
			collectByValueNames(e, decls, seen, &deps)
		}
	}

	return deps
}

// collectByValueNames appends to deps every user-defined type name that t
// references by value. It recurses into anonymous structs and anonymous tuples,
// which are inline and nameless — their fields/elements can still reference
// named types by value.
func collectByValueNames(t types.Type, decls map[string]*ast.TypeDeclStmt, seen map[string]bool, deps *[]string) {
	switch t := t.(type) {
	case types.UnresolvedType:
		// This is the common case: a field annotated as a plain named type
		// (`field: Node`) or with an explicit allocation (`field: shared Node`).
		addIfByValue(t.Name, t.Allocation, decls, seen, deps)

	case types.NamedStructType:
		// Resolved struct reference appearing as a field type (rare in raw AST,
		// but handled defensively).
		addIfByValue(t.Name, t.Allocation, decls, seen, deps)

	case types.DataType:
		// Resolved data reference appearing as a field type (defensive).
		addIfByValue(t.Name, t.Allocation, decls, seen, deps)

	case types.AnonymousStructType:
		// Inline record inside a data constructor — no name of its own; walk fields.
		for _, f := range t.Fields {
			collectByValueNames(f.Type, decls, seen, deps)
		}

	case types.TupleType:
		// Anonymous tuple as a constructor parameter — no name; walk elements.
		for _, e := range t.Elements {
			collectByValueNames(e, decls, seen, deps)
		}

	case types.WeakType:
		// A `weak` field is a non-owning pointer (pointer-sized), so it breaks a
		// recursive size cycle exactly like a `shared` field — its referent is not
		// contained by value. Deliberately do NOT recurse into the inner type.

		// Primitives, generics, lambdas, pointers, void, fixed-point, constrained,
		// range, parameterized — either not nominal types or bounded by construction.
	}
}

// addIfByValue appends name to deps if it names a by-value user-defined type:
//   - it must be declared in this program (present in decls), and
//   - the field annotation must not be `shared` (fieldMod != Shared), which is the
//     only way a recursive cycle is broken (there is no declaration-level flavor).
func addIfByValue(name string, fieldMod types.AllocationModifier, decls map[string]*ast.TypeDeclStmt, seen map[string]bool, deps *[]string) {
	if fieldMod == types.Shared {
		return // field annotated `shared` → pointer-sized at the call site
	}
	if seen[name] {
		return
	}
	if _, ok := decls[name]; !ok {
		return // not a user-defined nominal type in this program
	}
	seen[name] = true
	*deps = append(*deps, name)
}
