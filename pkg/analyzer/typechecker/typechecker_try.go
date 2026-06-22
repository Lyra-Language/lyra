package typechecker

import (
	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/ast/symbols"
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

	kind, payload, ok := tc.resultOrMaybeKind(operandT)
	if !ok {
		tc.addError(e.GetLocation(), SeverityError,
			"`?` operand must be a Result or Maybe, got %s", operandT.GetName())
		return nil
	}

	if enclKind, found := tc.enclosingReturnKind(); found && enclKind != kind {
		tc.addError(e.GetLocation(), SeverityError,
			"cannot propagate %s with `?` from a %s-returning function; convert it explicitly",
			kind, enclKind)
		return nil
	}
	// TODO(Result error conversion): when the operand's E differs from the
	// enclosing function's Err type, check convertibility (à la Rust's From)
	// rather than requiring identical E, once a conversion trait exists.

	tc.typeTable.Set(e, payload)
	return payload
}

// resultOrMaybeKind reports whether t is a Result<T, E> or Maybe<T> and, if so,
// its kind ("Result"/"Maybe") and success payload type T.
//
// Recognition is by name and arity first (matching checker.isResultOrMaybeName),
// then — only when a program actually declares its own `data Result`/`data
// Maybe` — confirmed against that declaration's shape via
// hasCanonicalResultOrMaybeShape. This is what tells a same-named-but-
// differently-shaped type (e.g. a user's own `data Result<a,b> = Foo a | Bar
// b`) apart from the real thing: it still resolves to a type literally named
// "Result", but isn't treated as `?`/must-use-eligible since its constructors
// aren't Ok/Err. A real canonical identity (independent of whatever happens to
// be declared under that name) is the longer-term fix once a prelude/module
// system exists — see todo.
func (tc *TypeChecker) resultOrMaybeKind(t types.Type) (kind string, payload types.Type, ok bool) {
	p, isParam := t.(types.ParameterizedType)
	if !isParam {
		return "", nil, false
	}
	switch p.Name {
	case "Result":
		if len(p.TypeArguments) == 2 && hasCanonicalResultOrMaybeShape(tc.symTable, "Result") {
			return "Result", p.TypeArguments[0], true
		}
	case "Maybe":
		if len(p.TypeArguments) == 1 && hasCanonicalResultOrMaybeShape(tc.symTable, "Maybe") {
			return "Maybe", p.TypeArguments[0], true
		}
	}
	return "", nil, false
}

// canonicalResultOrMaybeGenericArity gives the expected number of generic
// parameters for kind ("Result" or "Maybe"): Result needs an independent type
// for each of Ok/Err (so `data Result<t> = Ok t | Err t`, which reuses one
// parameter for both branches, is rejected); Maybe needs just the one payload
// type.
var canonicalResultOrMaybeGenericArity = map[string]int{"Result": 2, "Maybe": 1}

// canonicalResultOrMaybeShape gives the expected constructor-name → payload-arity
// map for kind ("Result" or "Maybe"): Result needs single-payload Ok/Err,
// Maybe needs a single-payload Some and a zero-payload None. This is the
// minimal structural check available without a real canonical/prelude
// identity — see hasCanonicalResultOrMaybeShape.
func canonicalResultOrMaybeShape(kind string) map[string]int {
	switch kind {
	case "Result":
		return map[string]int{"Ok": 1, "Err": 1}
	case "Maybe":
		return map[string]int{"Some": 1, "None": 0}
	}
	return nil
}

// hasCanonicalResultOrMaybeShape reports whether the `data` type registered
// under kind ("Result" or "Maybe") in symTable actually has the constructor
// shape `?`/must-use rely on, rather than merely sharing the name. There is no
// prelude, so most programs never declare `data Result`/`data Maybe` at all —
// the bare name is still treated as canonical in that case, preserving today's
// ambient usage. It is only when a program *does* declare one (registering it
// in symTable.Types) that the shape is actually checked: the right number of
// generic parameters (canonicalResultOrMaybeGenericArity) and exactly the
// constructors canonicalResultOrMaybeShape expects, each with the right
// payload arity. This is what catches the "clashing declaration" case the
// name-only match couldn't: a same-named-but-differently-shaped `data Result`
// no longer masquerades as the real thing.
func hasCanonicalResultOrMaybeShape(symTable *symbols.SymbolTable, kind string) bool {
	decl, ok := symTable.Types[kind]
	if !ok {
		return true
	}
	dt, ok := decl.Type.(types.DataType)
	if !ok {
		return false
	}
	if len(decl.GenericParams) != canonicalResultOrMaybeGenericArity[kind] {
		return false
	}
	expected := canonicalResultOrMaybeShape(kind)
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

// enclosingReturnKind returns the kind ("Result"/"Maybe") of the return type of
// the lambda body currently being checked, or found=false when there is no
// enclosing function or its return type is neither Result nor Maybe.
func (tc *TypeChecker) enclosingReturnKind() (kind string, found bool) {
	if tc.enclosingRet == nil {
		return "", false
	}
	k, _, ok := tc.resultOrMaybeKind(tc.enclosingRet.Type)
	return k, ok
}
