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
		diags = append(diags, checkGenericParamsIn(stmt, nil)...)
		if decl, ok := stmt.(*ast.TypeDeclStmt); ok {
			diags = append(diags, checkTypeDeclGenericParams(decl)...)
		}
		if trait, ok := stmt.(*ast.TraitDeclStmt); ok {
			diags = append(diags, checkTraitGenericParams(trait)...)
		}
	}
	return diags
}

// checkTypeDeclGenericParams reports a type variable a type declaration's body mentions and
// its list does not declare (lyra-E031) — `struct Box<t> { v: u }`, `tuple Pair<t>(t, u)`,
// `type Lst = []t`.
//
// **For a type the list is not optional.** A binding with no list is generic in whatever its
// signature mentions, because a call solves the variables from its arguments. A type has no
// call: it is instantiated only by writing `Box<i64>`, and the list is what pairs that argument
// with a variable. So a variable missing from it can never receive a type. Until 09/13 each
// of these compiled, and the error surfaced at a use as a field "of type u" nothing could be
// assigned to — or, for an alias, as a backend crash.
//
// **An unused parameter is not reported.** A type's list is its interface — every use writes
// the argument — so `struct Id<t> { n: i64 }` is a phantom type (a `User` id that is not an
// `Order` id), which is a pattern rather than a slip. That is where types and bindings part:
// a binding's unused variable is solved by nothing and can carry a bound that constrains
// nothing.
func checkTypeDeclGenericParams(decl *ast.TypeDeclStmt) []diag.Diagnostic {
	declared := make(map[string]bool, len(decl.GenericParams))
	for _, p := range decl.GenericParams {
		declared[p.Name] = true
	}
	used := map[string]bool{}
	collectDeclBodyTypeVars(decl.Type, used)

	var diags []diag.Diagnostic
	for _, name := range sortedNames(used) {
		if declared[name] {
			continue
		}
		msg := fmt.Sprintf(
			"type variable %q is not declared in %q's generic parameter list — add it (`%s<%s>`), or write a concrete type if %q is a misspelling. A type is instantiated only through its list, so a variable missing from it can never be given a type",
			name, decl.Name, decl.Name, strings.Join(withVar(decl.GenericParams, name), ", "), name)
		if decl.IsAlias {
			msg = fmt.Sprintf(
				"type alias %q mentions the type variable %q, but an alias takes no parameters, so nothing can ever give %q a type — write a concrete type",
				decl.Name, name, name)
		}
		diags = append(diags, diag.Diagnostic{
			Location: typeDeclNameLocation(decl),
			Severity: diag.SeverityError,
			Code:     diag.CodeUndeclaredTypeVariable,
			Message:  msg,
		})
	}
	return diags
}

// collectDeclBodyTypeVars collects the type variables a declaration's own body mentions: a
// struct's or union's fields, a data type's constructor payloads, and anything else through
// CollectTypeVars — which stops at a nominal type, since in a *signature* that is another
// declaration's business. Here the nominal type is the declaration itself, one level down.
func collectDeclBodyTypeVars(t types.Type, vars map[string]bool) {
	switch body := t.(type) {
	case types.NamedStructType:
		for _, f := range body.Fields {
			types.CollectTypeVars(f.Type, vars)
		}
	case types.UnionType:
		for _, m := range body.Members {
			types.CollectTypeVars(m.Type, vars)
		}
	case types.DataType:
		for _, c := range body.Constructors {
			for _, p := range c.Params {
				collectDeclBodyTypeVars(p, vars)
			}
		}
	default:
		types.CollectTypeVars(t, vars)
	}
}

// checkTraitGenericParams reports a trait parameter no method signature mentions
// (lyra-W013).
//
// **The other direction is not checked for a trait**, deliberately: a method may be generic in
// a variable of its own — `map: (Self<a>, (a) -> b) -> Self<b>` — and there is no syntax for a
// method-level list to declare it in, so a variable absent from the trait's list is the only
// way to write one. A trait parameter that no method mentions is a different matter: an impl
// binds it (`impl Conv<i64> for X`) and nothing can use what it was bound to.
func checkTraitGenericParams(trait *ast.TraitDeclStmt) []diag.Diagnostic {
	if len(trait.GenericParams) == 0 {
		return nil
	}
	used := map[string]bool{}
	for _, m := range trait.Methods {
		if m.Signature != nil {
			types.CollectTypeVars(m.Signature, used)
		}
	}
	var diags []diag.Diagnostic
	for _, p := range trait.GenericParams {
		if used[p.Name] {
			continue
		}
		msg := fmt.Sprintf("generic parameter %q of trait %q is never mentioned in any of its methods; remove it", p.Name, trait.Name)
		if len(p.Constraints) > 0 {
			msg = fmt.Sprintf(
				"generic parameter %q of trait %q is never mentioned in any of its methods, so its bound (%s) constrains nothing; use %q in a method or remove the parameter",
				p.Name, trait.Name, strings.Join(p.Constraints, " + "), p.Name)
		}
		loc := p.Location
		if loc.StartLine == 0 {
			loc = trait.NameLocation
		}
		diags = append(diags, diag.Diagnostic{
			Location: loc,
			Severity: diag.SeverityWarning,
			Code:     diag.CodeUnusedTypeParameter,
			Message:  msg,
			Tags:     []diag.Tag{diag.TagUnnecessary},
		})
	}
	return diags
}

func typeDeclNameLocation(decl *ast.TypeDeclStmt) ast.Location {
	if decl.NameLocation.StartLine != 0 {
		return decl.NameLocation
	}
	return decl.GetLocation()
}

// checkGenericParamsIn checks every binding in stmt with the type variables of the bindings
// that enclose it in scope.
//
// **A binding nested inside a generic function sees that function's variables** — and the
// variables of every generic binding between it and the top level. `let both<t> = (a: t,
// b: u) -> …` inside `outer<u>` mentions `u`, which is in scope, and naming it in `both`'s
// own list would make it a second, different `u`. The chain matters one level down: `inner`
// inside a local `mid<m>` inside `outer<u>` may mention both `m` and `u` (09/13). The walk
// stops at a function binding and recurses into it with its variables added, so a sibling's
// variables never leak into scope.
func checkGenericParamsIn(stmt ast.Statement, inScope map[string]bool) []diag.Diagnostic {
	var diags []diag.Diagnostic
	ast.WalkStmt(stmt, func(s ast.Statement) bool {
		decl, ok := s.(*ast.VarDeclStmt)
		if !ok {
			return true
		}
		if len(decl.GenericParams) > 0 {
			// No list written means nothing to reconcile: a binding with type variables
			// and no list is generic and legal, unchanged.
			diags = append(diags, checkBindingGenericParams(decl, inScope)...)
		}
		lambda, isFunction := decl.Value.(*ast.LambdaExpr)
		if !isFunction || lambda.Body == nil {
			return true
		}
		// Its body sees its variables as well as the ones already in scope.
		inner := make(map[string]bool, len(inScope))
		for name := range inScope {
			inner[name] = true
		}
		for name := range typeVarsInScope(decl) {
			inner[name] = true
		}
		if block, isBlock := lambda.Body.(*ast.BlockExpr); isBlock {
			for _, bodyStmt := range block.Statements {
				diags = append(diags, checkGenericParamsIn(bodyStmt, inner)...)
			}
		} else {
			diags = append(diags, checkGenericParamsIn(&ast.ExpressionStmt{Expression: lambda.Body}, inner)...)
		}
		return false
	}, nil)
	return diags
}

// typeVarsInScope is the type variables a top-level function binding brings into scope for
// the bindings inside it: its written list, or — with none written — the variables its
// signature mentions, which is what makes it generic in the first place.
func typeVarsInScope(decl *ast.VarDeclStmt) map[string]bool {
	vars := map[string]bool{}
	for _, p := range decl.GenericParams {
		vars[p.Name] = true
	}
	if lambda, ok := decl.Value.(*ast.LambdaExpr); ok && len(decl.GenericParams) == 0 {
		for _, p := range lambda.Parameters {
			types.CollectTypeVars(p.Type, vars)
		}
		types.CollectTypeVars(lambda.ReturnType.Type, vars)
	}
	return vars
}

// checkBindingGenericParams compares one binding's declared list against the
// variables its signature mentions.
func checkBindingGenericParams(decl *ast.VarDeclStmt, inScope map[string]bool) []diag.Diagnostic {
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
		if declared[name] || inScope[name] {
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
