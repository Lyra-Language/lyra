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
			if d, ok := c.table.LookupType(kind); ok && d.Builtin == "" && matchesCanonicalShape(d, kind) {
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

// canonicalTraitKinds are the compiler-known **traits**. Unlike Result/Maybe, which
// the compiler knows because control flow (`?`, `??`, must-use) is written against
// them, these two are known because the compiler *owns the operators that dispatch to
// them*: `<`/`<=`/`>`/`>=`/`<=>` all derive from Ord's `compare`, and `==`/`!=` are
// overridden by Eq's `eq`.
//
// That ownership is why the marker matters more here than it looks. Arithmetic
// operator overloading (08/07) deliberately keys on the *method name* and lets the
// author pick the trait, because `+` on a matrix and `+` on a duration share no
// invariant. Comparison cannot do that — `<` and `<=>` must agree, so one trait owns
// them — and a trait the compiler must find by identity is exactly what `@builtin`
// is for.
var canonicalTraitKinds = []string{"Ord", "Eq"}

// canonicalTraitShape is the method a kind's trait must declare: its name and its
// parameter count. Both are (Self, Self) — two parameters, the receiver first.
//
// The **return** type is deliberately not gated. `Ord::compare` yields the prelude's
// `Ordering`, but that is a `data` type resolved later, and the backend already reads
// it off the matched impl's own signature by name (`ordDataType`, `findConstructor`)
// rather than assuming it — so re-deriving it here would be a second answer to a
// settled question, and one that runs before the type it needs is resolved.
func canonicalTraitShape(kind string) (method string, params int) {
	switch kind {
	case "Ord":
		return "compare", 2
	case "Eq":
		return "eq", 2
	}
	return "", 0
}

// matchesCanonicalTraitShape reports whether decl declares the kind's method with the
// right arity. Extra methods are allowed: what the compiler needs is that `compare`
// (or `eq`) is there and callable, not that the trait declares nothing else.
func matchesCanonicalTraitShape(decl *ast.TraitDeclStmt, kind string) bool {
	method, params := canonicalTraitShape(kind)
	if method == "" {
		return false
	}
	for i := range decl.Methods {
		m := &decl.Methods[i]
		if m.Name.Kind != ast.MethodNameKindIdentifier || m.Name.Value != method {
			continue
		}
		return m.Signature != nil && len(m.Signature.Parameters) == params
	}
	return false
}

// resolveCanonicalTraits is resolveCanonicalTypes for traits, and follows the same two
// rules in the same order: an explicit `@builtin(Ord)` marker confers the identity
// whatever the trait is *called*, and with no marker anywhere for a kind, a trait
// literally named "Ord"/"Eq" with the right shape is stamped.
//
// The fallback is what the compiler did *only* before 08/08, and keeping it is what
// makes this change carry no migration: a program that declares its own `trait Ord`
// and no prelude still gets comparison dispatch. What the marker adds is that the
// prelude's claim is explicit, so a user's own `trait Ord` in the entry module is left
// an ordinary trait instead of silently becoming the one `<` dispatches through.
func (c *Collector) resolveCanonicalTraits() {
	marked := map[string][]*ast.TraitDeclStmt{}
	for _, stmt := range c.ast.Statements {
		td, ok := stmt.(*ast.TraitDeclStmt)
		if !ok || td.Builtin == "" {
			continue
		}
		marked[td.Builtin] = append(marked[td.Builtin], td)
	}

	known := map[string]bool{}
	for _, k := range canonicalTraitKinds {
		known[k] = true
	}
	for kind, decls := range marked {
		if known[kind] {
			continue
		}
		for _, d := range decls {
			c.addCanonicalError(d.NameLocation,
				"unknown `@builtin` trait %q; expected one of Ord, Eq", kind)
		}
	}

	for _, kind := range canonicalTraitKinds {
		decls := marked[kind]
		if len(decls) == 0 {
			if d, ok := c.table.LookupTrait(kind); ok && d.Builtin == "" && matchesCanonicalTraitShape(d, kind) {
				d.CanonicalKind = kind
			}
			continue
		}
		// A same-named *unmarked* trait is not the canonical one, and saying so is the
		// whole point of the marker. Recorded rather than reported, so a later
		// diagnostic can explain a comparison that did not dispatch.
		for _, stmt := range c.ast.Statements {
			d, ok := stmt.(*ast.TraitDeclStmt)
			if !ok || d.Name != kind || d.Builtin != "" {
				continue
			}
			d.ShadowedCanonical = kind
		}
		var chosen *ast.TraitDeclStmt
		for _, d := range decls {
			method, params := canonicalTraitShape(kind)
			if !matchesCanonicalTraitShape(d, kind) {
				c.addCanonicalError(d.NameLocation,
					"`@builtin(%s)` requires a `%s` method taking %d parameters, but %q does not declare one",
					kind, method, params, d.Name)
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

// canonicalTraitName is the *declared* name of the trait carrying kind, for a consumer
// that has to write the name down rather than merely find it — `@derive(Ord)`
// synthesizes an `impl <name> for X`, and naming the kind would produce an impl of a
// trait that does not exist when the canonical Ord is called something else.
//
// Falls back to the kind, which is right for a program with no such trait at all: the
// synthesized impl then names an undeclared trait and is reported as one, rather than
// being silently dropped.
func (c *Collector) canonicalTraitName(kind string) string {
	for _, decl := range c.table.Traits {
		if decl != nil && decl.CanonicalKind == kind {
			return decl.Name
		}
	}
	return kind
}
