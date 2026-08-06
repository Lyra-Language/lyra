package typechecker

import (
	"fmt"

	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/types"
)

// inferTryExpr type-checks a `?` (try) postfix operator and returns the
// unwrapped success payload (the T in Result<T, E> / Maybe<T>).
//
// Three things are enforced here, the type-aware half of the try checks:
//   - the operand must be a Result or Maybe;
//   - propagation is same-kind only — a Result may only be `?`-propagated from a
//     Result-returning function, a Maybe only from a Maybe-returning function.
//     Crossing kinds requires an explicit conversion (e.g. maybe.ok_or(err) /
//     result.ok()), which keeps the lossy step visible.
//
// The "used outside a Result/Maybe-returning function" context error is reported
// by checker.CheckTryOutsideResult, so when the enclosing return kind is unknown
// (top level, or a non-Result/Maybe return) this function stays silent to avoid
// duplicate diagnostics and simply yields the unwrapped payload.
func (tc *TypeChecker) inferTryExpr(e *ast.TryExpr) types.Type {
	operandT := tc.inferExprType(e.Operand)
	if operandT == nil {
		return nil // operand already failed to infer; it was reported elsewhere
	}

	kind, payload, operandErr, ok := tc.resultOrMaybeKind(operandT, e.GetLocation())
	if !ok {
		if msg, shadowed := tc.shadowedCanonicalMessage(operandT, e.GetLocation()); shadowed {
			tc.addError(e.GetLocation(), SeverityError, "%s", msg)
			return nil
		}
		tc.addError(e.GetLocation(), SeverityError,
			"`?` operand must be a Result or Maybe, got %s", operandT.GetName())
		return nil
	}

	enclKind, enclErr, found := tc.enclosingReturnKind(e.GetLocation())
	if found && enclKind != kind {
		tc.addError(e.GetLocation(), SeverityError,
			"cannot propagate %s with `?` from a %s-returning function; convert it explicitly",
			kind, enclKind)
		return nil
	}
	// Same kind: a propagated Result's error type must be able to flow into the
	// enclosing function's error type. Until a From-style conversion trait
	// exists, that means assignable (usually the same nominal error type); a
	// mismatch (e.g. propagating a `Result<_, IoError>` out of a `Result<_,
	// ParseError>` function) must be converted explicitly at the call site.
	if found && kind == "Result" && operandErr != nil && enclErr != nil &&
		!errorTypesCompatible(operandErr, enclErr) {
		tc.addError(e.GetLocation(), SeverityError,
			"cannot propagate Result with `?`: error type %s is not convertible to the enclosing function's error type %s; convert it explicitly",
			operandErr, enclErr)
		return nil
	}

	tc.typeTable.Set(e, payload)
	return payload
}

// resultOrMaybeKind reports whether t is a Result<T, E> or Maybe<T> and, if so,
// its kind ("Result"/"Maybe"), success payload type T, and — for a Result — the
// error type E (nil for a Maybe, which has no error payload).
//
// Identity comes from the canonical-type resolution done at collection time, not
// from re-matching name and shape here: canonicalKind resolves the type's name
// to its declaration and reads the stamped CanonicalKind (which already accounts
// for an `@builtin` marker and the shape check), so a same-named-but-differently-
// shaped `data Result<a,b> = Foo a | Bar b` — stamped with no CanonicalKind — is
// correctly not recognized, while an `@builtin(Result)`-marked `Either` is. The
// arity guard stays local: it protects the TypeArguments indexing below.
func (tc *TypeChecker) resultOrMaybeKind(t types.Type, loc ast.Location) (kind string, payload, errType types.Type, ok bool) {
	p, isParam := t.(types.ParameterizedType)
	if !isParam {
		return "", nil, nil, false
	}
	switch tc.canonicalKind(p.Name, loc) {
	case "Result":
		if len(p.TypeArguments) == 2 {
			return "Result", p.TypeArguments[0], p.TypeArguments[1], true
		}
	case "Maybe":
		if len(p.TypeArguments) == 1 {
			return "Maybe", p.TypeArguments[0], nil, true
		}
	}
	return "", nil, nil, false
}

// shadowedCanonicalMessage returns the message for the one case where "`?` operand
// must be a Result or Maybe, got Maybe" is technically true and useless: the operand
// *is* named Maybe (or Result), but it is the user's own declaration, and the kind
// belongs to the `@builtin`-marked one in the prelude. Naming the answer as the
// problem is what made the old message unreadable; this says which of the two
// mistakes it is and what to do about it.
//
// The identity work is not repeated here — the collector stamps
// ShadowedCanonical/ShapeMatchesCanonical alongside CanonicalKind, so the shape test
// has one home (CLAUDE.md rule 4).
//
// **The advice deliberately never suggests `@builtin`.** Marking the shadow is
// `lyra-E017` ("duplicate `@builtin(Maybe)`"), because the prelude already claims
// the kind — so recommending it would walk the author straight into a second error.
// A program can have exactly one canonical Maybe, and it is the prelude's.
func (tc *TypeChecker) shadowedCanonicalMessage(t types.Type, loc ast.Location) (string, bool) {
	name := t.GetName()
	if p, isParam := t.(types.ParameterizedType); isParam {
		name = p.Name
	}
	decl, ok := tc.symTable.LookupTypeFrom(name, loc)
	if !ok || decl.ShadowedCanonical == "" {
		return "", false
	}
	kind := decl.ShadowedCanonical
	if decl.ShapeMatchesCanonical {
		// They re-declared the prelude's type, most likely without knowing it was
		// already in scope (it is implicitly imported). Deleting it is the fix that
		// makes `?` work; renaming keeps their type but not `?`.
		return fmt.Sprintf(
			"`?` works on the prelude's %s, and %q here is your own declaration at %s, not that one. "+
				"Remove it to use the prelude's %s, or rename it if you meant a separate type",
			kind, name, decl.NameLocation.Pretty(), kind), true
	}
	// A differently-shaped type wearing the name: `?` was never going to apply, and
	// the name is what made that look like a contradiction.
	return fmt.Sprintf(
		"`?` works on the prelude's %s, and %q here is your own declaration at %s — a different type "+
			"that happens to share the name. Rename it, or return the prelude's %s instead",
		kind, name, decl.NameLocation.Pretty(), kind), true
}

// canonicalKind returns the canonical kind ("Result"/"Maybe") a type name
// resolves to, or "" for an ordinary type. A declared type is authoritative —
// its stamped CanonicalKind is returned verbatim (possibly ""), so a declaration
// that lost the name to an `@builtin`-marked type, or never had the shape, is
// not recognized. Only a name with *no* declaration falls back to the ambient
// legacy behavior (a bare Result/Maybe annotation with no `data` declaration
// anywhere), matching checker.canonicalKindOfName.
func (tc *TypeChecker) canonicalKind(name string, loc ast.Location) string {
	if decl, ok := tc.symTable.LookupTypeFrom(name, loc); ok {
		return decl.CanonicalKind
	}
	if name == "Result" || name == "Maybe" {
		return name
	}
	return ""
}

// canonicalTypeName is canonicalKind's inverse: given a kind ("Result"/"Maybe"),
// it returns the *source name* of the declaration stamped with that kind. It is
// what a compiler-synthesized type of canonical kind must be built from — a
// builtin whose signature mentions `Maybe<T>` (today `read_line`) has a kind in
// hand and needs a name to put in a ParameterizedType.
//
// Two things it deliberately does not do. It does not assume the name is
// "Maybe": the `@builtin(Maybe)` marker confers the identity and the spelling is
// free, so an `@builtin(Maybe) data Option<t>` must yield "Option". And it reads
// `decl.Name` rather than the map key, per CLAUDE.md rule 4 — a key is
// module-qualified for a private or prelude-shadowing declaration, so using it
// would produce a "type" name no source file could ever write.
//
// The scan is over all declarations rather than a lookup, because there is no
// name to look up — that is the whole question being asked. It is O(types) on a
// path taken once per `read_line` call site, and the alternative (an index built
// at collection) is a second place for the canonical identity to live, which
// rule 4 is specifically about not having.
func (tc *TypeChecker) canonicalTypeName(kind string, loc ast.Location) (string, bool) {
	if tc.symTable == nil {
		return "", false
	}
	for _, decl := range tc.symTable.Types {
		if decl != nil && decl.CanonicalKind == kind {
			return decl.Name, true
		}
	}
	// No declaration carries the kind. Fall back to the bare name only if it also
	// resolves to that kind — matching canonicalKind's own legacy fallback for a
	// program with no `data Maybe` declaration anywhere.
	if tc.canonicalKind(kind, loc) == kind {
		return kind, true
	}
	return "", false
}

// enclosingReturnKind returns the kind ("Result"/"Maybe") and — for a Result —
// the error type E of the return type of the lambda body currently being
// checked, or found=false when there is no enclosing function or its return type
// is neither Result nor Maybe.
func (tc *TypeChecker) enclosingReturnKind(loc ast.Location) (kind string, errType types.Type, found bool) {
	if tc.enclosingRet == nil {
		return "", nil, false
	}
	k, _, e, ok := tc.resultOrMaybeKind(tc.enclosingRet.Type, loc)
	return k, e, ok
}

// errorTypesCompatible reports whether a Result's error type `from` can flow
// into the enclosing function's error type `to` when propagated with `?`.
//
// This is the interim, assignability-only convertibility rule (there is no
// From-style conversion trait yet): `from` must be assignable to `to` — the same
// nominal error type, an untyped-literal widening, or a constrained/base
// relation. A genuine type variable on either side (a function polymorphic over
// its error, `Result<t, e>`) accepts any error, since the caller instantiates
// it. Two *different* nominal error types (e.g. `IoError` vs `ParseError`) are
// not compatible: the mismatch must be made explicit at the call site (map the
// error). When a From-style trait lands, this widens to also accept a declared
// `from`→`to` conversion — see todo Pit-of-Success #1.
func errorTypesCompatible(from, to types.Type) bool {
	if _, ok := from.(types.GenericType); ok {
		return true
	}
	if _, ok := to.(types.GenericType); ok {
		return true
	}
	return isAssignable(from, to)
}

// orderingType resolves the prelude's `Ordering` — the result type of `<=>`.
//
// Looked up by name rather than synthesized, so the type the expression carries is
// the *same* declaration a `match` arm's `Less`/`Equal`/`Greater` patterns resolve
// against; a fresh DataType built here would compare unequal to it and every
// three-way match would fail on its own arms.
//
// Unlike Maybe and Result there is no `@builtin` marker, so this is a plain
// lookup: `Ordering` is an ordinary prelude type that `<=>` happens to name. A
// program without a prelude gets a diagnostic saying so rather than a type
// pointing at nothing.
func (tc *TypeChecker) orderingType(loc ast.Location) types.Type {
	if decl, ok := tc.symTable.LookupTypeFrom("Ordering", loc); ok && decl != nil {
		return tc.resolveType(types.UnresolvedType{Name: decl.Name}, loc)
	}
	tc.addError(loc, SeverityError,
		"`<=>` produces an Ordering, and this program has no Ordering type (it is normally the prelude's)")
	return nil
}
