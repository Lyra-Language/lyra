package checker

import (
	"fmt"

	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/ast/symbols"
	diag "github.com/Lyra-Language/lyra/pkg/diagnostic"
	"github.com/Lyra-Language/lyra/pkg/types"
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
	impureSiblings := c.traitMethodsWithEffects(program)
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
				// **A method whose trait takes a `mut` parameter is not reported.** The
				// `mut` is the trait saying its impls may change what they are given — a
				// bus's `read_word` advancing a device, an `acknowledge_interrupt` clearing
				// one pending — and `pure` forbids exactly that, so the trait bound would
				// contradict the signature and bind every impl against it. One impl that
				// happens not to mutate (a test's stub) is no evidence the method is pure.
				// Sheliak's `Bus` drew this on every stub until 09/28.
				tm := traitMethodDecl(c.symTable, decl, m.Name)
				if takesMut(tm) {
					continue
				}
				// `Trait::method` is the spelling the language has for naming one, and
				// the impl's own location is where the reader is looking.
				key := traitMethodKey{trait: traitDeclOf(c.symTable, decl), method: m.Name.Key()}
				diags = append(diags, missingTraitMethodBound(m.Clause.GetLocation(),
					decl.TraitName, m.Name.Value, c.impureMethods[m], !impureSiblings[key]))
			}
		}
	}
	return diags
}

// missingTraitMethodBound is W018 for a trait-impl method, where the advice is not the
// same as a free function's.
//
// **Marking the impl `pure` does not help a bound call.** A `where t: Speak` call is scored
// against every impl of `Speak::say`, so what it can rely on is the *trait's* bound; an
// impl says nothing about its siblings. Until 09/23 this diagnostic suggested the impl and
// nothing else, which is advice that leaves the case it was most needed for untouched — one
// impure impl elsewhere still blamed the generic that called it.
//
// The trait is named first because it is the single action that covers everything: an impl
// inherits its trait's bound (effectiveMethodBounds), so writing it there satisfies this
// warning as well, at every impl at once. The impl is still offered, since for a method
// reached only by direct dispatch it is the smaller commitment — and bounding a trait binds
// every future impl, including ones in code that does not exist yet.
//
// **Unless another impl of the method has effects**: the trait's bound would then be
// refused at that impl, so the advice is the impl alone (`traitBoundable` false). That is
// a smaller promise — it helps a call dispatched to this impl directly, not one through a
// bound — but it is the only one the program can keep.
func missingTraitMethodBound(loc ast.Location, traitName, methodName string, effects Effect, traitBoundable bool) diag.Diagnostic {
	qualified := fmt.Sprintf("%s::%s", traitName, methodName)
	var msg string
	if traitBoundable {
		msg = fmt.Sprintf(
			"%q has no observable effect; declare the bound on the trait (`trait %s { pure %s: … }`), which every impl inherits. Marking this impl `pure` binds only this one, and a call through `where t: %s` is scored against every impl — so the trait's bound is the one a generic caller can rely on. Nothing is refused today — purity is inferred — but until the bound is written, an effect added here later is reported at whatever calls %q rather than at the edit",
			qualified, traitName, methodName, traitName, qualified)
	} else {
		msg = fmt.Sprintf(
			"%q has no observable effect here; mark this impl `pure`. The trait cannot carry the bound — another impl of %q has effects — so it holds for calls that reach this impl directly. Nothing is refused today — purity is inferred — but until the bound is written, an effect added here later is reported at whatever calls it rather than at the edit",
			qualified, qualified)
	}
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

// traitMethodKey names one method of one trait **by its resolved declaration**, not its
// name — two modules may each declare a `Speak` (rule 4, and traitMethodDecl's history).
type traitMethodKey struct {
	trait  *ast.TraitDeclStmt
	method string
}

// traitMethodsWithEffects reports, per trait method, whether any impl of it in the program
// has an observable effect — which is what decides whether the trait could take a `pure`
// bound at all.
func (c *purityChecker) traitMethodsWithEffects(program *ast.Program) map[traitMethodKey]bool {
	out := map[traitMethodKey]bool{}
	for _, node := range program.Statements {
		impl, ok := node.(*ast.TraitImplStmt)
		if !ok {
			continue
		}
		trait := traitDeclOf(c.symTable, impl)
		for i := range impl.Methods {
			m := &impl.Methods[i]
			if c.impureMethods[m]&PurityEffects != 0 {
				out[traitMethodKey{trait: trait, method: m.Name.Key()}] = true
			}
		}
	}
	return out
}

// traitDeclOf is the trait an impl implements, resolved at the impl (nil if unknown).
func traitDeclOf(symTable *symbols.SymbolTable, impl *ast.TraitImplStmt) *ast.TraitDeclStmt {
	td, ok := symTable.LookupTraitFrom(impl.TraitName, impl.GetLocation())
	if !ok {
		return nil
	}
	return td
}

// takesMut reports whether a trait method's declared signature has a `mut` parameter,
// `mut Self` included.
func takesMut(tm *ast.TraitMethod) bool {
	if tm == nil || tm.Signature == nil {
		return false
	}
	for _, p := range tm.Signature.Parameters {
		if p.Borrow == types.Mut {
			return true
		}
	}
	return false
}
