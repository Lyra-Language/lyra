package checker

import (
	"fmt"
	"sort"
	"strings"

	"github.com/Lyra-Language/lyra/pkg/ast"
	diag "github.com/Lyra-Language/lyra/pkg/diagnostic"
	"github.com/Lyra-Language/lyra/pkg/types"
)

// CheckGenericParams reconciles a binding's **written** generic parameter list
// against the type variables its signature actually mentions, in both directions:
// a signature variable missing from the list is an error (lyra-E031), a declared
// parameter the signature never mentions is a warning (lyra-W013).
//
// **The list stays optional; written, it is authoritative.** Type variables are
// lexical — a lowercase type name is a variable wherever it appears — so
//
//	let unbox = (b: Box<t>, fb: t) -> t => …
//
// is generic with no list at all, and that follows from the lexical rule rather
// than being an oversight. What does not follow is the list being *unchecked when
// written*, which is what this pass fixes: before it, both of these compiled and
// ran.
//
//	let unbox = (b: Box<t>, fb: t) -> t => …   // no <t> — generic anyway
//	let mismatch<t> = (a: u) -> u => a         // declares t, is generic in u
//
// *The hazard is a typo, and it is a Pit-of-Success inversion.* A misspelled
// lowercase type name does not fail — it silently becomes a *new* type variable,
// and the function becomes generic in something its author never meant. The
// signature still type-checks; what changes is that callers must now solve a
// variable that should have been a fixed type, so the diagnostic (if any) lands
// at the call site, or the error surfaces only in the backend. That is how the
// prelude's `ok`/`err` shipped without their `<t, e>` and drew no diagnostic at
// all. Uppercase names never had the hole: an unknown one is an UnresolvedType
// and is reported at the declaration.
//
// **Why an error and not a warning.** Making a written list authoritative gives
// `<t>` somewhere for the typo to be caught, which a warning also does; what only
// an error buys is that a bound cannot be quietly inert. The list is the only
// place a bound can be written (`<t: Show>`), so a list that need not agree with
// its signature means a constraint can silently constrain nothing. That is what
// makes this worth settling *before* bound enforcement rather than after — an
// unenforced bound and a bound on the wrong variable are indistinguishable from
// the outside, and only one of them stops being a problem when enforcement lands.
//
// This pass runs before typechecking: it needs only the collector's types, and
// reporting at the declaration is the entire point — the whole failure mode being
// closed is a diagnostic that lands somewhere else.
func CheckGenericParams(program *ast.Program) []diag.Diagnostic {
	var diags []diag.Diagnostic
	// Every binding, not only the top-level ones: `declaration` is an ordinary
	// statement, so a generic function may be declared inside any block, and a
	// nested one is exactly as able to carry the typo.
	for _, node := range program.Statements {
		stmt, ok := node.(ast.Statement)
		if !ok {
			continue
		}
		ast.WalkStmt(stmt, func(s ast.Statement) bool {
			decl, ok := s.(*ast.VarDeclStmt)
			if !ok || len(decl.GenericParams) == 0 {
				// No list written, so there is nothing to reconcile. A binding
				// with type variables and no list is generic and legal,
				// unchanged.
				return true
			}
			diags = append(diags, checkBindingGenericParams(decl)...)
			return true
		}, nil)
	}
	return diags
}

// checkBindingGenericParams compares one binding's declared list against the
// variables its signature mentions.
func checkBindingGenericParams(decl *ast.VarDeclStmt) []diag.Diagnostic {
	declared := make(map[string]bool, len(decl.GenericParams))
	for _, p := range decl.GenericParams {
		declared[p.Name] = true
	}

	used := map[string]bool{}
	// The signature is the type annotation and — for a function binding — the
	// lambda's parameters and return type. Both, not either: a binding may carry
	// an annotation *and* a lambda value (`let f<t>: (t) -> t = (x: t) -> t => x`),
	// and a variable mentioned in only one of them is still mentioned.
	types.CollectTypeVars(decl.Type, used)
	if lambda, ok := decl.Value.(*ast.LambdaExpr); ok {
		for _, p := range lambda.Parameters {
			types.CollectTypeVars(p.Type, used)
		}
		types.CollectTypeVars(lambda.ReturnType.Type, used)
	}

	var diags []diag.Diagnostic
	for _, name := range sortedNames(used) {
		if declared[name] {
			continue
		}
		diags = append(diags, diag.Diagnostic{
			Location: undeclaredVarLocation(decl, name),
			Severity: diag.SeverityError,
			Code:     diag.CodeUndeclaredTypeVariable,
			Message: fmt.Sprintf(
				"type variable %q is not declared in %s's generic parameter list — add it (`<%s>`), or write a concrete type if %q is a misspelling. A lowercase type name is always a type variable, so an undeclared one silently makes this binding generic in something it never meant to be",
				name, quoteName(decl.Name), strings.Join(withVar(decl.GenericParams, name), ", "), name),
		})
	}

	for _, p := range decl.GenericParams {
		if used[p.Name] {
			continue
		}
		msg := fmt.Sprintf("generic parameter %q of %s is never mentioned in its signature; remove it", p.Name, quoteName(decl.Name))
		if len(p.Constraints) > 0 {
			// The case the error half exists to prevent: a bound on a variable
			// nothing is solved for constrains nothing at all, which is the
			// opposite of what writing it says.
			msg = fmt.Sprintf(
				"generic parameter %q of %s is never mentioned in its signature, so its bound (%s) constrains nothing; use %q in the signature or remove the parameter",
				p.Name, quoteName(decl.Name), strings.Join(p.Constraints, " + "), p.Name)
		}
		diags = append(diags, diag.Diagnostic{
			Location: genericParamLocation(decl, p),
			Severity: diag.SeverityWarning,
			Code:     diag.CodeUnusedTypeParameter,
			Message:  msg,
			Tags:     []diag.Tag{diag.TagUnnecessary},
		})
	}
	return diags
}

// undeclaredVarLocation points at the parameter whose type introduced the
// undeclared variable, which is where the fix goes. types.Type carries no
// location of its own, so the parameter is the finest anchor available; a
// variable that appears only in the return type (or only in the annotation) falls
// back to the bound name.
func undeclaredVarLocation(decl *ast.VarDeclStmt, name string) ast.Location {
	if lambda, ok := decl.Value.(*ast.LambdaExpr); ok {
		for _, p := range lambda.Parameters {
			vars := map[string]bool{}
			types.CollectTypeVars(p.Type, vars)
			if vars[name] {
				return p.GetLocation()
			}
		}
	}
	return declNameLocation(decl)
}

// genericParamLocation points at the entry in the `<…>` list. The list is written
// by hand, so its span is always available; the fallback covers a parameter merged
// in from elsewhere.
func genericParamLocation(decl *ast.VarDeclStmt, p ast.GenericParam) ast.Location {
	if p.Location.StartLine != 0 {
		return p.Location
	}
	return declNameLocation(decl)
}

func declNameLocation(decl *ast.VarDeclStmt) ast.Location {
	if decl.NameLocation.StartLine != 0 {
		return decl.NameLocation
	}
	return decl.GetLocation()
}

// withVar renders the list the fix would produce — the declared parameters plus
// the missing one, in written order — so the message shows the edit rather than
// describing it.
func withVar(params []ast.GenericParam, name string) []string {
	out := make([]string, 0, len(params)+1)
	for _, p := range params {
		out = append(out, p.Name)
	}
	return append(out, name)
}

// quoteName renders a binding's name for a message, tolerating the anonymous case
// rather than producing a stray empty `""`.
func quoteName(name string) string {
	if name == "" {
		return "this binding"
	}
	return fmt.Sprintf("%q", name)
}

// sortedNames keeps the diagnostic order stable: map iteration is randomized, and
// two variables missing from one list would otherwise report in a different order
// per run.
func sortedNames(set map[string]bool) []string {
	names := make([]string, 0, len(set))
	for n := range set {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}
