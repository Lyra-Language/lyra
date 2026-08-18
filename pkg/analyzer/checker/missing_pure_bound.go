package checker

import (
	"fmt"

	"github.com/Lyra-Language/lyra/pkg/ast"
	diag "github.com/Lyra-Language/lyra/pkg/diagnostic"
)

// missingPureBounds reports (lyra-W018) every top-level function and trait-impl
// method whose inferred effect set is empty and which does not say `pure`.
//
// It is the inverse of its parent pass. CheckPurity reads an annotation and checks
// the body against it; this reads the body and asks whether the annotation is
// missing. Both consume the same fixpoint — the effect row is computed once, in
// CheckPurity, and handed here — so nothing is re-derived.
//
// **What the missing bound costs is not what it looks like.** The obvious argument
// is that an unmarked callee blocks a `pure` caller, and in this compiler that is
// simply false: purity is *inferred* whole-program, so a `pure` function may call
// an unannotated one whose body the fixpoint found effect-free, method or free
// function alike. Nothing is refused today.
//
// The cost is paid on the *next* edit, and it is the same failure CheckGenericParams
// exists to close — the diagnostic lands somewhere else:
//
//	let helper = (n: i64) -> i64 => { println("added later"); n * 2 }
//	let caller = pure (n: i64) -> i64 => helper(n)
//
// The `println` is reported at `caller`, because `caller` is the only thing in the
// program that promised anything. Write `pure` on `helper` and it is reported at the
// `println` too — at the edit, where the author is, in the function they are looking
// at. So the bound is not documentation of what the body already does; it is where
// the blame goes when the body stops doing it, and an inferred-pure function is one
// unremarkable edit away from moving that blame to a caller it has never met.
//
// Scope is deliberately narrow, and each exclusion earns its place:
//
//   - **Top-level bindings and impl methods only.** The purity fixpoint covers
//     every lambda in the program, inline closure arguments included, and
//     "consider marking this `pure`" on the `(x) => x * 2` inside an `xs.map(…)`
//     is advice about an expression, not about an interface. A nested named
//     `let helper` is left out for the same reason at lower confidence: it has
//     one caller, in the same body, and blame cannot travel far.
//   - **`main` never warns.** It is the program's entry point, called by nothing,
//     so there is no caller for the blame to move to.
//   - **`pure` only.** See CodeMissingPureBound for why `det` and `noalloc` are
//     not reported here.
//
// A higher-order function *is* reported. Marking one `pure` does not forbid
// impure callbacks: a callback's effects are charged to the call site that
// supplies it (see the callbacks map), so an impure caller passing an impure
// function is unaffected and only a *pure* caller is held to it. That is exactly
// how the prelude's `map`/`filter`/`flat_map` are `pure noalloc` today.
func (c *purityChecker) missingPureBounds(program *ast.Program) []diag.Diagnostic {
	var diags []diag.Diagnostic
	for _, node := range program.Statements {
		switch decl := node.(type) {
		case *ast.VarDeclStmt:
			lam, ok := decl.Value.(*ast.LambdaExpr)
			if !ok || decl.Name == "main" || lam.IsPure {
				continue
			}
			if c.impureLambdas[lam]&PurityEffects != 0 {
				continue
			}
			diags = append(diags, missingPureBound(decl.NameLocation, decl.Name, c.impureLambdas[lam]))
		case *ast.TraitImplStmt:
			for i := range decl.Methods {
				m := &decl.Methods[i]
				if isPure, _, _ := c.effectiveMethodBounds(decl, m); isPure {
					continue
				}
				if c.impureMethods[m]&PurityEffects != 0 {
					continue
				}
				// `Trait::method` is the spelling the language has for naming one,
				// and the impl's own location is where the annotation goes.
				diags = append(diags, missingPureBound(m.Clause.GetLocation(),
					fmt.Sprintf("%s::%s", decl.TraitName, m.Name.Value), c.impureMethods[m]))
			}
		}
	}
	return diags
}

// missingPureBound builds the diagnostic for one callable. The message names the
// consequence rather than the property: that the body has no effect is something
// the author can see by reading it, and what they cannot see is that the omission
// is silent right up until it isn't, and then reports somewhere else.
//
// A function that allocates gets a second sentence. `pure` permits EffectAlloc by
// design (allocation is a resource concern, orthogonal to purity — see
// PurityEffects), but a reader who has just been told their allocating function is
// effect-free will reasonably suspect the compiler of missing something, so the
// diagnostic says the quiet part.
func missingPureBound(loc ast.Location, name string, effects Effect) diag.Diagnostic {
	msg := fmt.Sprintf(
		"%q has no observable effect; mark it `pure`. Nothing is refused today — purity is inferred — but until the bound is written, an effect added here later is reported at whatever calls %q rather than at the edit",
		name, name)
	if effects.Has(EffectAlloc) {
		msg += ". It allocates, which `pure` permits: allocation is a `noalloc` concern, not a purity one"
	}
	return diag.Diagnostic{
		Location: loc,
		Severity: diag.SeverityWarning,
		Code:     diag.CodeMissingPureBound,
		Message:  msg,
	}
}
