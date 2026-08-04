package collector

import (
	"fmt"

	"github.com/Lyra-Language/lyra/pkg/ast"
	diag "github.com/Lyra-Language/lyra/pkg/diagnostic"
	"github.com/Lyra-Language/lyra/pkg/types"
)

// canonicalKinds are the compiler-known sum types that get special treatment
// (`?` propagation, must-use, `??`). Their identity is resolved here, once, so
// no downstream pass has to re-derive it from a name.
var canonicalKinds = []string{"Result", "Maybe"}

// canonicalArity is the generic-parameter count each canonical kind requires:
// Result needs an independent type for each of Ok/Err; Maybe needs one payload.
var canonicalArity = map[string]int{"Result": 2, "Maybe": 1}

// canonicalShape maps a kind to its expected constructor-name → payload-arity:
// Result is single-payload Ok/Err; Maybe is single-payload Some and nullary None.
func canonicalShape(kind string) map[string]int {
	switch kind {
	case "Result":
		return map[string]int{"Ok": 1, "Err": 1}
	case "Maybe":
		return map[string]int{"Some": 1, "None": 0}
	}
	return nil
}

// canonicalShapeDesc renders a human-readable form of a kind's expected shape,
// for diagnostics.
func canonicalShapeDesc(kind string) string {
	switch kind {
	case "Result":
		return "Ok(_) | Err(_)"
	case "Maybe":
		return "Some(_) | None"
	}
	return ""
}

// matchesCanonicalShape reports whether decl is a `data` type with exactly the
// generic arity and constructors that kind requires. This is the structural
// gate: a same-named-but-differently-shaped `data Result<a,b> = Foo a | Bar b`
// does not match, so it is never granted canonical identity.
func matchesCanonicalShape(decl *ast.TypeDeclStmt, kind string) bool {
	dt, ok := decl.Type.(types.DataType)
	if !ok {
		return false
	}
	if len(decl.GenericParams) != canonicalArity[kind] {
		return false
	}
	expected := canonicalShape(kind)
	if len(dt.Constructors) != len(expected) {
		return false
	}
	for _, ctor := range dt.Constructors {
		arity, declared := expected[ctor.Name]
		if !declared || arity != len(ctor.Params) {
			return false
		}
	}
	return true
}

// resolveCanonicalTypes stamps ast.TypeDeclStmt.CanonicalKind for every
// declaration that is the canonical Result/Maybe, and reports malformed
// `@builtin` markers. It runs once after the whole program is collected.
//
// Identity is conferred one of two ways, marker taking precedence:
//
//   - Explicit: a `@builtin(Result)` / `@builtin(Maybe)` attribute names the
//     canonical type regardless of what it is *called* — a type named `Either`
//     can be the canonical Result. The marker is honored only if the type has
//     the required shape; a mismatched-shape marked type is an error and is not
//     stamped. A second declaration claiming an already-taken kind is an error.
//
//   - Fallback (no marker for that kind anywhere): a declaration literally named
//     "Result"/"Maybe" with the canonical shape is stamped. This preserves
//     pre-marker behavior for a program that just declares `data Result`/`data
//     Maybe` directly, with no prelude in the search roots. Once a kind is
//     claimed by a marker, the bare name is no longer load-bearing — a
//     same-named unmarked type is left an ordinary type.
//
// That last rule has a sharp edge now that `std/prelude.lyra` marks its own
// types: a user declaration named `Maybe` is left an ordinary type, and `?` on it
// used to report "operand must be a Result or Maybe, got Maybe" — a message that
// names the answer as the problem. The rule is right, so what changed is the
// diagnostic: such a declaration is stamped `ShadowedCanonical` (plus whether its
// shape would have qualified) and `?` reports what actually happened. Note the fix
// that reads as obvious is not available — marking the shadow `@builtin(Maybe)`
// too is `lyra-E017`, a duplicate claim — so the advice is to drop the declaration
// or rename it, never to add a marker.
//
// Truly ambient use (a Result/Maybe annotation with no declaration at all) has
// no declaration to stamp; the recognition sites keep a name+arity fallback for
// that case.
func (c *Collector) resolveCanonicalTypes() {
	// Group every @builtin-marked declaration by the kind it claims.
	marked := map[string][]*ast.TypeDeclStmt{}
	for _, stmt := range c.ast.Statements {
		td, ok := stmt.(*ast.TypeDeclStmt)
		if !ok || td.Builtin == "" {
			continue
		}
		marked[td.Builtin] = append(marked[td.Builtin], td)
	}

	known := map[string]bool{}
	for _, k := range canonicalKinds {
		known[k] = true
	}
	// A @builtin naming something that isn't a canonical kind is a mistake.
	for kind, decls := range marked {
		if known[kind] {
			continue
		}
		for _, d := range decls {
			c.addCanonicalError(d.NameLocation, "unknown `@builtin` type %q; expected one of Result, Maybe", kind)
		}
	}

	for _, kind := range canonicalKinds {
		decls := marked[kind]
		if len(decls) == 0 {
			// Fallback: an unmarked, canonically-shaped type of the same name.
			if d, ok := c.table.Types[kind]; ok && d.Builtin == "" && matchesCanonicalShape(d, kind) {
				d.CanonicalKind = kind
			}
			continue
		}
		// The kind is claimed by a marker, so a same-named *unmarked* declaration is
		// not it. Record that, and whether it would otherwise have qualified, so the
		// `?` diagnostic can say which of the two mistakes this is rather than
		// reporting "must be a Maybe, got Maybe". Stamped here because the shape test
		// lives here; a consumer re-deriving it would be a second copy to drift.
		//
		// Walked over the statements rather than looked up in `c.table.Types`, for a
		// reason that is easy to get wrong: a declaration shadowing a prelude name is
		// registered under a *qualified* key so the prelude keeps the bare one
		// (symbols' declKeyIn), so `Types[kind]` returns the prelude's declaration —
		// exactly the one this is not about.
		for _, stmt := range c.ast.Statements {
			d, ok := stmt.(*ast.TypeDeclStmt)
			if !ok || d.Name != kind || d.Builtin != "" {
				continue
			}
			d.ShadowedCanonical = kind
			d.ShapeMatchesCanonical = matchesCanonicalShape(d, kind)
		}
		// One or more explicit markers: the first well-shaped one wins; a
		// wrong-shaped marker and any subsequent claimant are errors.
		var chosen *ast.TypeDeclStmt
		for _, d := range decls {
			if !matchesCanonicalShape(d, kind) {
				c.addCanonicalError(d.NameLocation,
					"`@builtin(%s)` requires the canonical %s shape (%s), but %q does not match",
					kind, kind, canonicalShapeDesc(kind), d.Name)
				continue
			}
			if chosen == nil {
				chosen = d
				d.CanonicalKind = kind
			} else {
				c.addCanonicalError(d.NameLocation,
					"duplicate `@builtin(%s)`: %q is already the canonical %s", kind, chosen.Name, kind)
			}
		}
	}
}

// addCanonicalError appends a CodeMalformedBuiltin diagnostic at loc.
func (c *Collector) addCanonicalError(loc ast.Location, format string, args ...any) {
	c.errors = append(c.errors, diag.Diagnostic{
		Message:  fmt.Sprintf(format, args...),
		Location: loc,
		Severity: diag.SeverityError,
		Code:     diag.CodeMalformedBuiltin,
	})
}
