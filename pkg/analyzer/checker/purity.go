package checker

import (
	"fmt"

	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/ast/symbols"
	diag "github.com/Lyra-Language/lyra/pkg/diagnostic"
	"github.com/Lyra-Language/lyra/pkg/types"
	"github.com/Lyra-Language/lyra/pkg/typetable"
)

// PurityError reports an observable side effect that occurs inside a function
// marked `pure`.
type PurityError struct {
	Code     string
	Message  string
	Location ast.Location
}

func (e PurityError) Error() string {
	return fmt.Sprintf("%s: %s", e.Location.Pretty(), e.Message)
}

// CheckPurity walks the program and reports observable side effects that occur
// inside any function marked `pure`.
//
// Model — purity is about effects crossing the function boundary, NOT about
// whether mutation happens at all. A `pure` function may freely mutate values it
// owns *locally* (its own parameters and any binding it declares in its body);
// internal mutation is invisible to callers and keeps pure code fast. An effect
// is reported only when it is observable from outside the call:
//
//   - reassigning / compound-assigning a *captured* (outer-scope) binding
//   - writing through a pointer (`*p = v`)
//   - calling a non-pure function
//   - awaiting (`await e`) — suspends on external I/O
//
// The membrane is one-way: pure code may not perform these; impure code may
// freely call pure code. Nested lambdas re-establish their own context (a `pure`
// lambda nested in an impure one is still checked, and vice-versa), mirroring how
// CheckAwaitOutsideAsync scopes `inAsync` per lambda.
//
// Interior mutation (`p.x = v`, `arr[i] = v`) is an effect when the value being
// mutated is not a local the function owns: it escapes through a `mut`-borrowed
// parameter (a borrow of the caller's value) or a captured outer binding. An
// `own` parameter and any `var`/`let mut` declared in the body are owned locally,
// so mutating them is invisible to callers and allowed.
//
// Calls to *user-defined* impure functions are caught via inferImpureLambdas
// (transitive, fixpoint) — for a function declared at any nesting depth, not
// just the top level. When methodTable is non-nil (the type-checker has
// already run and resolved method dispatch), a method call — `obj.method(…)`
// or the fully-qualified `Trait::method(…)` — gets the same treatment: it is
// flagged unless the resolved method is itself marked `pure`. A trait-impl
// method marked `pure` is also checked for its own internal violations, the
// method-level counterpart of the per-statement walk over pure lambdas below.
// Reading captured *mutable* state (`var` / `let mut`, whose value can change
// between calls) is also reported — it breaks referential transparency even
// though no effect escapes — for state captured from any enclosing scope,
// again not just the top level. Both resolve names via the capture stack of
// scopeBindings frames built as the walk descends through lambda boundaries.
// Not yet handled (needs symbol-table backing; see todo items #3/#4):
//   - bottom-up purity *inference* for methods — today only an explicit
//     `pure` marker on the method itself is trusted; an unannotated method is
//     always treated as potentially impure, unlike a free function
func CheckPurity(program *ast.Program, scopeTable *symbols.ScopeTable, typeTable *typetable.TypeTable, methodTable *typetable.MethodTable) []PurityError {
	base := []scopeBindings{{mutable: mutableGlobals(program), functions: topLevelFunctions(program)}}
	frames := newScopeFrames(program, scopeTable)
	boundGroups := collectTraitMethodGroups(program)
	impureLambdas, impureMethods := inferImpurity(collectFuncBindings(program, base, frames), collectMethodImpls(program), base, methodTable, boundGroups, buildAllocContext(program, typeTable), frames)
	c := &purityChecker{
		impureLambdas: impureLambdas,
		impureMethods: impureMethods,
		assignTargets: map[*ast.IdentifierExpr]bool{},
		methodTable:   methodTable,
		boundGroups:   boundGroups,
		traitDecls:    collectTraitDecls(program),
		frames:        frames,
	}
	for _, node := range program.Statements {
		if stmt, ok := node.(ast.Statement); ok {
			// Top level is an impure context: a nil scope means "not inside a pure
			// function, don't check".
			ast.WalkStmt(stmt, c.stmtVisitor(nil), c.exprVisitor(nil, base))
		}
		if impl, ok := node.(*ast.TraitImplStmt); ok {
			c.checkTraitMethodBounds(impl, base)
		}
	}
	return c.errors
}

// checkTraitMethodBounds checks each method in impl against its declared effect
// bounds: `det`/`noalloc` for every method (against its full inferred effect
// set, like checkBoundedEffects for lambdas), plus the fine-grained `pure` walk
// for pure-marked methods — the method-level counterpart of the main loop's
// per-statement walk over pure lambdas. A non-pure method's body is not walked
// for purity (it's an impure context, same as any other non-pure binding).
func (c *purityChecker) checkTraitMethodBounds(impl *ast.TraitImplStmt, base []scopeBindings) {
	for i := range impl.Methods {
		m := &impl.Methods[i]
		// The effective bound is the impl method's own annotation OR the one the
		// trait declares on that method (`trait Show { pure show: … }`) — a bound
		// on the trait is a contract every impl must satisfy.
		isPure, isDet, isNoAlloc := m.IsPure, m.IsDet, m.IsNoAlloc
		if td := traitMethodDecl(c.traitDecls, impl.TraitName, m.Name); td != nil {
			isPure = isPure || td.IsPure
			isDet = isDet || td.IsDet
			isNoAlloc = isNoAlloc || td.IsNoAlloc
		}
		c.checkBoundedEffects(isDet, isNoAlloc, c.impureMethods[m], m.Clause.GetLocation())
		if !isPure {
			continue
		}
		scope := directScopeBindingsForClause(&m.Clause)
		locals := make(map[string]bool, len(scope.mutable))
		for name := range scope.mutable {
			locals[name] = true
		}
		// Method parameters have no mut/own/ref modifier syntax yet (unlike a
		// lambda's), so there is no mutBorrows set to populate here.
		sc := &funcScope{locals: locals, mutBorrows: map[string]bool{}}
		childCapture := pushScope(base, scope)
		ast.WalkExpr(m.Clause.Body, c.stmtVisitor(sc), c.exprVisitor(sc, childCapture))
	}
}

// InferredPureFunctions returns, for every top-level `let`/`var name = <lambda>`
// binding in program, whether it is pure — either explicitly annotated `pure`
// or, the purity-inference goal (FP/Imperative todo #3), implicitly pure
// because the same fixpoint CheckPurity uses to flag impure callees found no
// observable effect in its body or transitive callees. This makes *pure* (not
// just impure) a recorded, queryable result for any top-level function, not
// only ones explicitly marked `pure` or ones a `pure` caller happens to call —
// e.g. for IDE tooling or a future auto-parallelism pass that wants to know
// "is this callable safe to run as pure" without re-deriving the analysis.
//
// Scoped to top-level bindings only, matching topLevelFunctions/mutableGlobals
// (a name-keyed map can't distinguish same-named functions in different
// scopes); a non-top-level function's purity is still checked structurally by
// CheckPurity, just not exposed here by name.
func InferredPureFunctions(program *ast.Program, scopeTable *symbols.ScopeTable) map[string]bool {
	effects := InferredEffects(program, scopeTable)
	result := make(map[string]bool, len(effects))
	for name, e := range effects {
		// Mask with PurityEffects, not IsPure(): a function that only allocates
		// (EffectAlloc) is still pure — allocation is orthogonal to purity (see
		// PurityEffects). Use InferredEffects directly to see the alloc bit.
		result[name] = e&PurityEffects == 0
	}
	return result
}

// InferredEffects is InferredPureFunctions generalized to the full effect
// row (FP/Imperative todo #5): for every top-level `let`/`var name = <lambda>`
// binding in program, the set of effects inferred for it — EffectNone for a
// function with no detected effect (the purity-inference result
// InferredPureFunctions exposes as a bool), or the specific bit(s) found
// otherwise. Exists so a caller that cares about *which* effect a function
// has (e.g. "deterministic but allocates" tooling, once EffectAlloc
// detection lands) doesn't have to re-derive the analysis once that
// distinction is wired up — today only EffectMut/EffectIO are ever set,
// matching what CheckPurity already detected before this existed.
//
// Same scoping caveat as InferredPureFunctions: top-level bindings only, and
// no MethodTable (this entry point only has the AST), so a function whose
// only "effect" is calling a trait method resolved by the type-checker is
// not picked up here — see CheckPurity for the MethodTable-aware version.
// EffectAlloc is likewise never set here: allocation is a use-site flavor read
// off the TypeTable, which this AST-only entry point doesn't have (its sole
// consumer, InferredPureFunctions, masks with PurityEffects and so ignores the
// alloc bit anyway) — CheckPurity is the alloc-aware path.
func InferredEffects(program *ast.Program, scopeTable *symbols.ScopeTable) map[string]Effect {
	base := []scopeBindings{{mutable: mutableGlobals(program), functions: topLevelFunctions(program)}}
	frames := newScopeFrames(program, scopeTable)
	// No TypeTable here (AST-only entry point), so alloc detection is disabled —
	// consistent with this helper's documented limited analysis.
	impure, _ := inferImpurity(collectFuncBindings(program, base, frames), collectMethodImpls(program), base, nil, collectTraitMethodGroups(program), buildAllocContext(program, nil), frames)
	result := make(map[string]Effect, len(base[0].functions))
	for name, lam := range base[0].functions {
		result[name] = impure[lam]
	}
	return result
}

// scopeBindings holds everything declared directly within one lexical scope (a
// lambda body, or the top level): which names are interior-mutable bindings
// (`var`/`let mut` — the per-scope analogue of mutableGlobals) and which names
// are bound to a function value declared directly here (the per-scope analogue
// of topLevelFunctions). Both maps are searched together: a name's presence in
// either one marks it as "declared at this frame", which is what lets a closer
// declaration correctly shadow a farther one of the *other* kind too (e.g. a
// nested `let helper` blocking a farther-out `var helper`).
type scopeBindings struct {
	mutable   map[string]bool
	functions map[string]*ast.LambdaExpr
}

// pushScope appends scope onto capture, always returning a freshly backed
// slice (never aliasing capture's backing array) so that sibling lambdas built
// from the same parent frame — and capture-stack snapshots retained long-term
// by collectFuncBindings — never observe each other's writes.
func pushScope(capture []scopeBindings, scope scopeBindings) []scopeBindings {
	child := make([]scopeBindings, len(capture)+1)
	copy(child, capture)
	child[len(capture)] = scope
	return child
}

// capturedMutable reports whether name resolves, via the given enclosing-scope
// capture stack (outermost first), to a binding whose value can change without
// this function reassigning it — a `var` or `let mut`. The stack is searched
// from the innermost frame outward so a closer declaration correctly shadows a
// farther one of either kind.
func capturedMutable(capture []scopeBindings, name string) bool {
	for i := len(capture) - 1; i >= 0; i-- {
		if mutable, declared := capture[i].mutable[name]; declared {
			return mutable
		}
	}
	return false
}

// resolveFunction resolves name to the function literal it is bound to,
// searching the capture stack innermost-out. A name declared at some frame as
// a non-function binding (present in .mutable but absent from .functions)
// shadows any function of the same name farther out, so the search stops
// there rather than continuing.
func resolveFunction(capture []scopeBindings, name string) (*ast.LambdaExpr, bool) {
	for i := len(capture) - 1; i >= 0; i-- {
		if lam, ok := capture[i].functions[name]; ok {
			return lam, true
		}
		if _, declared := capture[i].mutable[name]; declared {
			return nil, false
		}
	}
	return nil, false
}

// funcScope describes the binding environment of the pure function currently
// being walked. A nil *funcScope means "not inside a pure function". locals holds
// every name owned locally (parameters + body declarations); mutBorrows is the
// subset of parameters declared `mut` — owned for *name* reassignment (rebinding
// the borrow is local) but a borrow for *interior* mutation (writing through it
// escapes to the caller's value).
type funcScope struct {
	locals     map[string]bool
	mutBorrows map[string]bool
}

func (s *funcScope) isLocal(name string) bool { return s != nil && s.locals[name] }

type purityChecker struct {
	errors []PurityError
	// impureLambdas holds the inferred effect set for each function literal,
	// keyed by pointer (not name) so that two unrelated functions in different
	// scopes that happen to share a name are never confused with each other.
	// EffectNone means inferred pure. Populated by inferImpurity (purity
	// inference, todo #3; generalized to a full effect row, todo #5).
	impureLambdas map[*ast.LambdaExpr]Effect
	// impureMethods is impureLambdas' counterpart for trait-impl methods,
	// populated by the same call to inferImpurity.
	impureMethods map[*ast.TraitMethodImpl]Effect
	// assignTargets records IdentifierExpr nodes that are the root of an assignment
	// target (the LHS of `x += …`, or the base of `x.f = …`). The write is reported
	// by the mutation checks, so the same node must not be re-reported as a read.
	assignTargets map[*ast.IdentifierExpr]bool
	// methodTable maps a call site to the trait-impl method the type-checker
	// resolved it to (nil-safe: a nil table behaves as "no resolutions", so
	// this is optional — passing nil into CheckPurity simply skips
	// method-call purity checking, e.g. for callers that haven't run the
	// typechecker first).
	methodTable *typetable.MethodTable
	// boundGroups maps a (trait, method) to every impl providing it, so a call
	// resolved by abstract bound dispatch (methodTable.GetBound) can be scored as
	// the join over those impls' effects.
	boundGroups map[typetable.BoundMethodRef][]*ast.TraitMethodImpl
	// traitDecls indexes trait declarations by name, so an impl method inherits
	// and is checked against the effect bounds its trait declares on that method.
	traitDecls map[string]*ast.TraitDeclStmt
	// frames builds a lambda's flat scope-bindings frame from the collector's
	// Scope tree (see scopeFrames), replacing the earlier AST re-walk.
	frames *scopeFrames
}

// stmtVisitor returns a statement callback. sc is non-nil exactly when we are
// inside a `pure` function body. A mutation of a name not owned locally is a
// mutation of captured outer state; interior mutation through a `mut`-borrowed
// parameter escapes to the caller's value. (Unlike exprVisitor, this needs no
// capture stack — statement-level mutation checks resolve names via sc alone.)
func (c *purityChecker) stmtVisitor(sc *funcScope) func(ast.Statement) bool {
	return func(stmt ast.Statement) bool {
		if sc == nil {
			return true // impure context: descend, but don't flag anything
		}
		switch s := stmt.(type) {
		case *ast.VarReassignmentStmt:
			if !sc.isLocal(s.Name) {
				c.report(s.GetLocation(),
					"pure function reassigns captured binding %q; mutation must not escape the function", s.Name)
			}
		case *ast.LValueAssignmentStmt:
			c.checkInteriorMutation(sc, s.Target, s.GetLocation())
			// The base of the target (`origin` in `origin.x = v`) is a write, not a
			// read; suppress the read-check on it. A mutable global used as an *index*
			// (`grid[i]`) is left untouched, so reading it is still flagged.
			if id := rootIdentExpr(s.Target); id != nil {
				c.assignTargets[id] = true
			}
		case *ast.DerefAssignmentStmt:
			c.report(s.GetLocation(),
				"pure function writes through a pointer; pointer writes may mutate external state")
		}
		return true
	}
}

// checkInteriorMutation reports an interior-mutation target (`p.x = v`) whose
// root binding is not a value the pure function owns: a `mut`-borrowed parameter
// writes through to the caller's value, and a captured outer binding mutates
// state observable elsewhere. A root that is an owned local (`own` param,
// body-declared `var`/`let mut`) is fine; an unrooted target (e.g. a call result)
// can't be attributed to a binding, so it is left alone.
func (c *purityChecker) checkInteriorMutation(sc *funcScope, target ast.Expression, loc ast.Location) {
	root := rootIdentName(target)
	switch {
	case root == "":
		return
	case sc.mutBorrows[root]:
		c.report(loc,
			"pure function mutates through `mut`-borrowed parameter %q; the write escapes to the caller's value", root)
	case !sc.isLocal(root):
		c.report(loc,
			"pure function mutates captured binding %q; mutation must not escape the function", root)
	}
}

func (c *purityChecker) exprVisitor(sc *funcScope, capture []scopeBindings) func(ast.Expression) bool {
	return func(expr ast.Expression) bool {
		switch e := expr.(type) {
		case *ast.LambdaExpr:
			// `det`/`noalloc` are enforced against this lambda's full inferred
			// (transitive) effect set, independent of the enclosing context.
			c.checkBoundedEffects(e.IsDet, e.IsNoAlloc, c.impureLambdas[e], e.GetLocation())
			// Default values execute at the call site, in the *enclosing* context.
			for i := range e.Parameters {
				ast.WalkExpr(e.Parameters[i].DefaultValue, c.stmtVisitor(sc), c.exprVisitor(sc, capture))
			}
			// The body runs in this lambda's own context: pure → build its scope;
			// impure → nil (no checking). Either way it introduces a new lexical
			// scope, so push its bindings frame onto the capture stack regardless
			// of purity — a pure lambda nested deeper still needs to resolve names
			// captured through this (possibly impure) intermediate scope.
			var child *funcScope
			scope := c.frames.forLambda(e)
			if e.IsPure {
				locals := make(map[string]bool, len(scope.mutable))
				for name := range scope.mutable {
					locals[name] = true
				}
				child = &funcScope{locals: locals, mutBorrows: mutBorrowParams(e)}
			}
			childCapture := pushScope(capture, scope)
			walkLambdaBodies(e, c.stmtVisitor(child), c.exprVisitor(child, childCapture))
			return false // recursed manually

		case *ast.IdentifierExpr:
			// A read of a captured binding whose value can change between calls is
			// non-deterministic — it breaks referential transparency even though no
			// effect escapes. Writes through this node (an assignment target) are
			// reported elsewhere, so skip those.
			if sc != nil && !c.assignTargets[e] && !sc.isLocal(e.Name) && capturedMutable(capture, e.Name) {
				c.report(e.GetLocation(),
					"pure function reads captured mutable binding %q; its value can change between calls, breaking referential transparency", e.Name)
			}

		case *ast.MathAssignOpExpr:
			// The LHS is a write target (reported below); don't also flag it as a read.
			c.assignTargets[&e.Left] = true
			if sc != nil && !sc.isLocal(e.Left.Name) {
				c.report(e.GetLocation(),
					"pure function mutates captured binding %q; mutation must not escape the function", e.Left.Name)
			}

		case *ast.FunctionCallExpr:
			if sc != nil {
				if method, ok := c.methodTable.Get(e); ok {
					// Mask with PurityEffects: only correctness effects (mut/io)
					// make a callee non-pure — EffectAlloc is orthogonal, so a
					// pure function may call a method that merely allocates.
					if c.impureMethods[method]&PurityEffects != 0 {
						c.report(e.GetLocation(),
							"pure function calls non-pure trait method %q", method.Name.GetName())
					}
				} else if ref, ok := c.methodTable.GetBound(e); ok {
					// Abstract dispatch through a `where` bound: pure only if every
					// impl of the bound trait method is pure.
					if boundCallEffect(ref, c.boundGroups, c.impureMethods)&PurityEffects != 0 {
						c.report(e.GetLocation(),
							"pure function calls non-pure trait method %q via a bound", ref.Method)
					}
				} else if name := calleeName(e.Function); name != "" && c.isImpureCallee(capture, name) {
					c.report(e.GetLocation(),
						"pure function calls impure function %q", name)
				}
			}

		case *ast.AwaitExpr:
			// `await` suspends until an external asynchronous operation completes —
			// an observable I/O effect that breaks determinism and referential
			// transparency, so it may never appear in a pure function.
			if sc != nil {
				c.report(e.GetLocation(),
					"pure function performs `await`; awaiting suspends on external I/O and must not cross the function boundary")
			}
		}
		return true
	}
}

// isImpureCallee reports whether a call target with the given dotted name is
// known or assumed to be impure. Returns true for:
//   - hard-coded effectful builtins (print, read, …) — those with PurityEffects
//   - user-defined functions whose lambda inferImpurity has flagged
//   - names that can't be resolved to any local lambda AND aren't in
//     builtinEffects (known builtins with non-purity effects are fine) AND
//     aren't pure type-conversion calls — these are treated conservatively as
//     impure (imported/external functions whose purity we can't verify)
func (c *purityChecker) isImpureCallee(capture []scopeBindings, name string) bool {
	if e, ok := builtinEffects[name]; ok {
		// Known builtin: only flag if it has a purity-violating effect.
		// Builtins with EffectAlloc only (e.g. Arena.new) are fine to call from
		// a pure function — allocation is orthogonal to purity.
		return e&PurityEffects != 0
	}
	lam, ok := resolveFunction(capture, name)
	if ok {
		// Mask with PurityEffects: an alloc-only callee is still pure to call
		// from a pure function (EffectAlloc is orthogonal to purity).
		return c.impureLambdas[lam]&PurityEffects != 0
	}
	// Unresolvable — could be an imported/external function. Conservatively
	// treat as impure unless it's a known pure type-conversion call.
	return !isTypeConversionCall(name)
}

func (c *purityChecker) report(loc ast.Location, format string, args ...any) {
	c.reportCode(diag.CodePurityViolation, loc, format, args...)
}

// reportCode appends a diagnostic with an explicit code. Used by the det/noalloc
// bound checks (CodeEffectBoundViolation), which share this pass with `pure`
// (CodePurityViolation) but are a distinct diagnostic.
func (c *purityChecker) reportCode(code string, loc ast.Location, format string, args ...any) {
	c.errors = append(c.errors, PurityError{
		Code:     code,
		Message:  fmt.Sprintf(format, args...),
		Location: loc,
	})
}

// checkBoundedEffects enforces the `det` and `noalloc` bounds for a callable
// whose fully-inferred (transitive) effect set is `effects`. `det` forbids the
// non-determinism sources (DetEffects = input/rand/time) while still allowing
// mutation, allocation, and output; `noalloc` forbids EffectAlloc. Unlike the
// fine-grained `pure` walk, this reports once at the callable's own location,
// naming the offending effect — per-operation locating is a later refinement.
// `pure` is handled by the walk (precise per-op diagnostics), not here; a
// `pure`+`det` pair is already an error (CodeConflictingEffectBounds).
func (c *purityChecker) checkBoundedEffects(isDet, isNoAlloc bool, effects Effect, loc ast.Location) {
	if isDet {
		if bad := effects & DetEffects; bad != 0 {
			c.reportCode(diag.CodeEffectBoundViolation, loc,
				"`det` function %s; a `det` function must be reproducible from its inputs — thread external state (a seed, the tick) through parameters instead", nondeterminismDescription(bad))
		}
	}
	if isNoAlloc && effects.Has(EffectAlloc) {
		c.reportCode(diag.CodeEffectBoundViolation, loc,
			"`noalloc` function heap-allocates by constructing a `shared`-typed value; a `noalloc` function must not allocate")
	}
}

// nondeterminismDescription names the first non-determinism source set in e
// (input, then randomness, then time — all three detected today).
func nondeterminismDescription(e Effect) string {
	switch {
	case e.Has(EffectInput):
		return "reads external input (I/O whose result depends on the outside world)"
	case e.Has(EffectRand):
		return "draws from a random source"
	case e.Has(EffectTime):
		return "reads the system clock"
	default:
		return "performs a non-deterministic effect"
	}
}

// scopeFrames builds a lambda's flat capture-stack frame (scopeBindings) from
// the collector's Scope tree instead of re-walking the AST (FP/Imperative todo
// #3, Phase 2). The collector already recorded, per lambda, the parameter
// `ScopeFunction` it pushed (ScopeTable keyed on the *ast.LambdaExpr) and, as
// descendant scopes, every block / loop / `with` / if-let scope in its body —
// each holding the names declared directly there as ast.Named symbols. forLambda
// flattens that subtree (down to but not through a nested lambda's own
// ScopeFunction) into the single per-lambda frame the purity analysis expects,
// reproducing directScopeBindings' result exactly. Two collector quirks are
// reconciled so behavior is unchanged: a `with`-arena handle is registered as a
// plain `let` in the scope but must read as interior-mutable (a captured arena
// is a stateful allocator), and a `for … in` loop variable lives in the loop
// scope but was never a body binding the old AST walk collected — precomputed
// arenaScopes / forInScopes carry both facts. Method clauses are not covered
// (the collector records no scope for them; see directScopeBindingsForClause).
type scopeFrames struct {
	scopeTable *symbols.ScopeTable
	// arenaScopes are the `with`-block scopes whose sole binding (the arena
	// handle) must read as interior-mutable despite being a plain `let`.
	arenaScopes map[*symbols.Scope]bool
	// forInScopes are `for … in` loop scopes whose own symbols (the key/value
	// loop variables) are skipped — the AST walk never collected them.
	forInScopes map[*symbols.Scope]bool
}

// newScopeFrames precomputes the arena / for-in scope classifications by walking
// the program once and mapping each WithStmt / ForInLoopExpr node to the scope
// the collector recorded for it. A nil scopeTable yields empty maps and a
// forLambda that contributes only parameter/clause names (defensive — the real
// pipeline always passes the collected table).
func newScopeFrames(program *ast.Program, scopeTable *symbols.ScopeTable) *scopeFrames {
	f := &scopeFrames{
		scopeTable:  scopeTable,
		arenaScopes: map[*symbols.Scope]bool{},
		forInScopes: map[*symbols.Scope]bool{},
	}
	if scopeTable == nil {
		return f
	}
	onStmt := func(s ast.Statement) bool {
		if w, ok := s.(*ast.WithStmt); ok {
			if sc, ok := scopeTable.Get(w); ok {
				f.arenaScopes[sc] = true
			}
		}
		return true
	}
	onExpr := func(e ast.Expression) bool {
		if fin, ok := e.(*ast.ForInLoopExpr); ok {
			if sc, ok := scopeTable.Get(fin); ok {
				f.forInScopes[sc] = true
			}
		}
		return true
	}
	for _, node := range program.Statements {
		if stmt, ok := node.(ast.Statement); ok {
			ast.WalkStmt(stmt, onStmt, onExpr)
		}
	}
	return f
}

// forLambda produces the single flat scopeBindings frame for lambda: which names
// declared in its own scope are interior-mutable (`var`/`let mut`, or an arena
// handle) and which are bound to a function value. Parameters and multi-clause
// patterns are read directly off the node (they are immutable, and clause
// patterns are not registered in the scope tree); body declarations come from
// the recorded Scope subtree.
func (f *scopeFrames) forLambda(lambda *ast.LambdaExpr) scopeBindings {
	frame := scopeBindings{mutable: map[string]bool{}, functions: map[string]*ast.LambdaExpr{}}
	for i := range lambda.Parameters {
		for _, n := range patternBoundNames(lambda.Parameters[i].Pattern) {
			frame.mutable[n] = false
		}
	}
	for _, clause := range lambda.LambdaClauses {
		for _, pat := range clause.Patterns {
			for _, n := range patternBoundNames(pat) {
				frame.mutable[n] = false
			}
		}
	}
	if f.scopeTable != nil {
		if scope, ok := f.scopeTable.Get(lambda); ok {
			f.collectSubtree(scope, &frame)
		}
	}
	return frame
}

// collectSubtree walks scope's descendants (its children are the lambda body's
// block/loop/with scopes), folding each scope's declarations into frame. It
// stops at a nested lambda's own ScopeFunction — those bindings belong to that
// lambda's frame, mirroring the old walk's "return false at a nested LambdaExpr".
func (f *scopeFrames) collectSubtree(scope *symbols.Scope, frame *scopeBindings) {
	for _, child := range scope.Children {
		if child.Kind == symbols.ScopeFunction {
			continue
		}
		f.addScopeSymbols(child, frame)
		f.collectSubtree(child, frame)
	}
}

// addScopeSymbols folds one scope's directly-declared names into frame, deriving
// each name's interior-mutability from its declaring node. A `for … in` loop
// scope's own key/value variables are skipped (never body bindings); an arena
// scope's handle reads as mutable.
func (f *scopeFrames) addScopeSymbols(scope *symbols.Scope, frame *scopeBindings) {
	if f.forInScopes[scope] {
		return
	}
	arena := f.arenaScopes[scope]
	for name, sym := range scope.Symbols {
		switch d := sym.(type) {
		case *ast.VarDeclStmt:
			if lam, ok := d.Value.(*ast.LambdaExpr); ok {
				frame.functions[name] = lam
				frame.mutable[name] = false
			} else if arena {
				frame.mutable[name] = true
			} else {
				frame.mutable[name] = d.CanMutateInterior()
			}
		case *ast.DestructuringDeclStmt:
			frame.mutable[name] = d.Keyword == "var" || d.IsMut
		default:
			// A parameter (edge cases where one is reached via the subtree) or any
			// other named symbol is an immutable, non-function binding.
			frame.mutable[name] = false
		}
	}
}

// directScopeBindingsForClause is directScopeBindings's counterpart for a
// bare trait-method clause — a method is always exactly one ordinary clause
// (the grammar never gives it a multi-clause or bare-body form the way a
// regular lambda can take), so there is no LambdaExpr wrapper to dispatch on.
// The inner walk logic (collecting let/var/destructuring bindings and
// stopping at nested lambdas) mirrors directScopeBindings's collectBody.
func directScopeBindingsForClause(clause *ast.LambdaClause) scopeBindings {
	scope := scopeBindings{mutable: map[string]bool{}, functions: map[string]*ast.LambdaExpr{}}
	for _, pat := range clause.Patterns {
		for _, n := range patternBoundNames(pat) {
			scope.mutable[n] = false
		}
	}
	mergeStmt := func(s ast.Statement) {
		for name, mutable := range declaredMutability(s) {
			scope.mutable[name] = mutable
		}
		if vd, ok := s.(*ast.VarDeclStmt); ok {
			if lam, ok := vd.Value.(*ast.LambdaExpr); ok {
				scope.functions[vd.Name] = lam
			}
		}
	}
	ast.WalkExpr(clause.Body,
		func(s ast.Statement) bool {
			mergeStmt(s)
			return true
		},
		func(e ast.Expression) bool {
			switch n := e.(type) {
			case *ast.LambdaExpr:
				return false
			case *ast.ForLoopExpr:
				mergeStmt(n.Init)
			}
			return true
		},
	)
	return scope
}

// declaredMutability returns the name(s) bound directly by stmt — a
// `let`/`var`/const declaration, a destructuring decl, an `if let`/`else`
// destructuring, or a `with`-arena statement — mapped to whether each is an
// interior-mutable binding (`var`, or `let mut`/destructuring with the `mut`
// modifier). A name bound to a function value is always reported as immutable
// here: call purity for it is handled separately (via scopeBindings.functions /
// inferImpureLambdas), and treating a function name as mutable data would be
// confusing. Returns nil for any other statement kind.
func declaredMutability(stmt ast.Statement) map[string]bool {
	switch s := stmt.(type) {
	case *ast.VarDeclStmt:
		if _, isLambda := s.Value.(*ast.LambdaExpr); isLambda {
			return map[string]bool{s.Name: false}
		}
		return map[string]bool{s.Name: s.CanMutateInterior()}
	case *ast.DestructuringDeclStmt:
		return destructuredMutability(s)
	case *ast.IfDestructuringStmt:
		// `if let <pat> = v { ... }` binds the pattern names locally to the pure
		// function; mutating or reassigning them must not be mistaken for an
		// escaping effect on captured state.
		return destructuredMutability(&s.DestructuringStatement)
	case *ast.ElseDestructuringStmt:
		// `let <pat> = v else { ... }` binds the pattern names for the code after
		// the diverging else, same as a plain destructuring decl.
		return destructuredMutability(&s.DestructuringStatement)
	case *ast.WithStmt:
		// The arena handle is a local owned binding; it's a stateful allocator, so
		// treat its interior as mutable (a nested pure lambda reading the captured
		// arena is still non-deterministic).
		return map[string]bool{s.Name: true}
	}
	return nil
}

// destructuredMutability returns each name a destructuring decl binds, mapped to
// whether its interior is mutable (`var` or the `mut` modifier).
func destructuredMutability(s *ast.DestructuringDeclStmt) map[string]bool {
	mutable := s.Keyword == "var" || s.IsMut
	out := map[string]bool{}
	for _, n := range patternBoundNames(s.Pattern) {
		out[n] = mutable
	}
	return out
}

// mutBorrowParams collects the names of a lambda's `mut`-modified parameters.
// Writing through such a parameter mutates the caller's value (a borrow), so it
// is an effect that escapes a pure function; `own`/`ref`/bare parameters are not
// in this set.
func mutBorrowParams(lambda *ast.LambdaExpr) map[string]bool {
	mut := map[string]bool{}
	for i := range lambda.Parameters {
		p := &lambda.Parameters[i]
		if p.TypeModifier != types.Mut {
			continue
		}
		if ip, ok := p.Pattern.(*ast.IdentifierPattern); ok {
			mut[ip.Name] = true
		}
	}
	return mut
}

// rootIdentExpr walks an interior-mutation target (a member/index chain like
// `grid[i].y`) back to the identifier it is rooted at, returning that node. It
// returns nil when the target is not rooted at a plain identifier (e.g. a
// function-call result). Note it follows only the *object* spine: an index
// expression's index (`i` in `grid[i]`) is a separate sub-read, not the root.
func rootIdentExpr(expr ast.Expression) *ast.IdentifierExpr {
	for {
		switch e := expr.(type) {
		case *ast.IdentifierExpr:
			return e
		case *ast.MemberExpr:
			expr = e.Object
		case *ast.IndexExpr:
			expr = e.Object
		default:
			return nil
		}
	}
}

// rootIdentName is rootIdentExpr's name, or "" when the target is not rooted at a
// plain identifier, in which case the write cannot be attributed to a binding.
func rootIdentName(expr ast.Expression) string {
	if id := rootIdentExpr(expr); id != nil {
		return id.Name
	}
	return ""
}

// mutableGlobals returns the names of top-level bindings whose *value* can change
// over time — a `var` (name and interior mutable) or a `let mut` (interior
// mutable). Reading one from a pure function is non-deterministic. Function
// bindings (lambda values) are excluded: call purity is handled by
// inferImpureLambdas, and treating a function name as mutable data would be
// confusing. Plain `let` (deeply immutable) and `const` are safe to read.
func mutableGlobals(program *ast.Program) map[string]bool {
	globals := map[string]bool{}
	for _, node := range program.Statements {
		vd, ok := node.(*ast.VarDeclStmt)
		if !ok || !vd.CanMutateInterior() {
			continue
		}
		if _, isLambda := vd.Value.(*ast.LambdaExpr); isLambda {
			continue
		}
		globals[vd.Name] = true
	}
	return globals
}

// allocContext holds the program-level information the effect inference needs
// to detect EffectAlloc: the TypeTable (so a construction's resolved flavor can
// be read) and which construction expressions are lexically inside a
// `with`-arena block (so their allocation is discharged into the arena rather
// than counting as an escaping heap allocation). Computed once per program by
// buildAllocContext and threaded into the effect-inference functions.
//
// Allocation is a *use-site* property: there is no declaration-level flavor, so
// constructing a value heap-allocates exactly when the value is used as
// `shared` — recorded on the construction expression's TypeTable entry by the
// typechecker (an annotated binding `let n: shared Node = Node{…}` records the
// annotation's flavor over the raw inferred type, so AllocationOf sees Shared).
//
// First-slice scope (FP/Imperative todo #5): a construction is detected as
// allocating only where the typechecker recorded a `shared` type on it — today
// that's an annotated `let`/`var` binding. A `shared` construction in a
// return/argument position (where the flavor isn't yet recorded on the
// construction node) and implicit allocation (dynamic arrays/strings, escaping
// closures) are deferred to a future layout/escape-analysis pass. The arena
// discharge is likewise approximate: a `shared` value built inside a `with`
// block but *returned out* still escapes, but detecting that needs the same
// escape analysis, so anything lexically inside the block is treated as
// discharged.
type allocContext struct {
	// typeTable resolves a construction expression to its recorded type, whose
	// flavor (AllocationOf) decides whether it heap-allocates. nil for entry
	// points that run without a typechecker pass (InferredEffects) — alloc
	// detection is then disabled, matching that entry point's limited contract.
	typeTable *typetable.TypeTable
	// discharged holds construction exprs lexically inside a `with`-arena
	// block, whose allocation goes into the arena rather than escaping.
	discharged map[ast.Expression]bool
}

// allocates reports whether the construction expr heap-allocates: its recorded
// type is `shared` and it is not discharged into an enclosing arena.
func (a *allocContext) allocates(expr ast.Expression) bool {
	if a == nil || a.typeTable == nil || a.discharged[expr] {
		return false
	}
	t, ok := a.typeTable.Get(expr)
	return ok && types.AllocationOf(t) == types.Shared
}

// buildAllocContext records the TypeTable (for reading each construction's
// resolved flavor) and marks construction expressions enclosed in a `with`-arena
// block as discharged. typeTable may be nil (see allocContext.typeTable).
func buildAllocContext(program *ast.Program, typeTable *typetable.TypeTable) *allocContext {
	a := &allocContext{
		typeTable:  typeTable,
		discharged: map[ast.Expression]bool{},
	}
	// Mark every expression lexically inside a `with`-arena body as discharged.
	// WithStmt is a *statement*, so it is caught in the onStmt callback; the
	// outer walk descends through the whole program (including into lambda
	// bodies, via the walker's stmt/expr mutual recursion), so a construction's
	// pointer marked here is the same object the effect walk later visits,
	// making set membership exact.
	markInsideArenas := func(stmt ast.Statement) bool {
		if w, ok := stmt.(*ast.WithStmt); ok {
			ast.WalkExpr(&w.Body, nil, func(inner ast.Expression) bool {
				a.discharged[inner] = true
				return true
			})
		}
		return true
	}
	for _, node := range program.Statements {
		if stmt, ok := node.(ast.Statement); ok {
			ast.WalkStmt(stmt, markInsideArenas, nil)
		}
	}
	return a
}

// topLevelFunctions maps each top-level `let`/`var name = <lambda>` binding to
// its lambda. Used as the base (outermost) frame of every capture stack.
func topLevelFunctions(program *ast.Program) map[string]*ast.LambdaExpr {
	fns := map[string]*ast.LambdaExpr{}
	for _, node := range program.Statements {
		if vd, ok := node.(*ast.VarDeclStmt); ok {
			if lam, ok := vd.Value.(*ast.LambdaExpr); ok {
				fns[vd.Name] = lam
			}
		}
	}
	return fns
}

// collectFuncBindings walks the whole program once, recording every lambda
// literal together with the capture stack (scopeBindings frames) visible at
// its *definition* site — not just top-level ones. This lexical context is
// what lets inferImpureLambdas resolve a captured mutable read or a call to a
// sibling function correctly for a function declared at any nesting depth.
// Keying the result by lambda pointer (rather than name) also means two
// unrelated functions in different scopes that happen to share a name are
// never confused with each other.
func collectFuncBindings(program *ast.Program, base []scopeBindings, frames *scopeFrames) map[*ast.LambdaExpr][]scopeBindings {
	defs := map[*ast.LambdaExpr][]scopeBindings{}
	var visit func(capture []scopeBindings, lam *ast.LambdaExpr)
	visit = func(capture []scopeBindings, lam *ast.LambdaExpr) {
		defs[lam] = capture
		findIn := func(at []scopeBindings) func(ast.Expression) bool {
			return func(e ast.Expression) bool {
				if inner, ok := e.(*ast.LambdaExpr); ok {
					visit(at, inner)
					return false
				}
				return true
			}
		}
		// Default values run at the call site, in the enclosing (this lambda's
		// own definition-site) scope, not the body's.
		for i := range lam.Parameters {
			ast.WalkExpr(lam.Parameters[i].DefaultValue, nil, findIn(capture))
		}
		childCapture := pushScope(capture, frames.forLambda(lam))
		walkLambdaBodies(lam, nil, findIn(childCapture))
	}
	findAtTop := func(e ast.Expression) bool {
		if lam, ok := e.(*ast.LambdaExpr); ok {
			visit(base, lam)
			return false
		}
		return true
	}
	for _, node := range program.Statements {
		if stmt, ok := node.(ast.Statement); ok {
			ast.WalkStmt(stmt, nil, findAtTop)
		}
	}
	return defs
}

// inferImpurity returns the sets of function literals and trait-impl methods
// whose bodies have an observable effect — directly (captured mutation,
// pointer write, `await`, call to an impure builtin) or transitively (a call
// to another impure function or method). Functions and methods are inferred
// in one joint fixpoint, rather than two separate ones, because each kind of
// callable can call the other (a function via a `.`-call/`Trait::method`
// dispatch resolved in methodTable, a method via an ordinary call to a
// sibling function or method) — iterating only one side first could settle
// on a stale answer for a callee analyzed on the other side after it. This is
// purity *inference*: unlike CheckPurity's enforcement pass, a method's
// explicit `pure` annotation is not consulted here — an unannotated method
// with no detected effect is inferred pure exactly like an unannotated
// function (FP/Imperative todo #3, "bottom-up purity inference for
// unannotated methods"). methodTable is nil-safe — see purityChecker.methodTable.
func inferImpurity(lambdaDefs map[*ast.LambdaExpr][]scopeBindings, methods []*ast.TraitMethodImpl, base []scopeBindings, methodTable *typetable.MethodTable, boundGroups map[typetable.BoundMethodRef][]*ast.TraitMethodImpl, alloc *allocContext, frames *scopeFrames) (map[*ast.LambdaExpr]Effect, map[*ast.TraitMethodImpl]Effect) {
	impureLambdas := map[*ast.LambdaExpr]Effect{}
	impureMethods := map[*ast.TraitMethodImpl]Effect{}
	// Fixpoint over an effect *set*: a callable's effect can only grow as more
	// of its callees are analyzed, so each pass ORs in any newly found bits and
	// we iterate until nothing changes. (This must recompute every callable
	// each pass — not skip the ones already non-pure — because a function found
	// to allocate early might still gain an io/mut bit from a callee resolved
	// later; a boolean "already impure, skip" early-out would miss that.)
	for {
		changed := false
		for lam, capture := range lambdaDefs {
			e := impureLambdas[lam] | lambdaEffects(lam, capture, impureLambdas, impureMethods, methodTable, boundGroups, alloc, frames)
			if e != impureLambdas[lam] {
				impureLambdas[lam] = e
				changed = true
			}
		}
		for _, m := range methods {
			e := impureMethods[m] | methodEffects(m, base, impureLambdas, impureMethods, methodTable, boundGroups, alloc)
			if e != impureMethods[m] {
				impureMethods[m] = e
				changed = true
			}
		}
		if !changed {
			break
		}
	}
	return impureLambdas, impureMethods
}

// collectMethodImpls gathers every trait-impl method declared in program,
// keyed by pointer (impl.Methods[i]'s address is stable since Methods is
// never reallocated after collection) so it matches the identity methodTable
// resolutions use.
func collectMethodImpls(program *ast.Program) []*ast.TraitMethodImpl {
	var methods []*ast.TraitMethodImpl
	for _, node := range program.Statements {
		impl, ok := node.(*ast.TraitImplStmt)
		if !ok {
			continue
		}
		for i := range impl.Methods {
			methods = append(methods, &impl.Methods[i])
		}
	}
	return methods
}

// collectTraitDecls indexes the program's trait declarations by name, so an
// impl method can be checked against the effect bounds (`pure`/`det`/`noalloc`)
// its trait declares on the corresponding method.
func collectTraitDecls(program *ast.Program) map[string]*ast.TraitDeclStmt {
	decls := map[string]*ast.TraitDeclStmt{}
	for _, node := range program.Statements {
		if td, ok := node.(*ast.TraitDeclStmt); ok {
			decls[td.Name] = td
		}
	}
	return decls
}

// traitMethodDecl returns the declaration of the named method in trait
// traitName, or nil if the trait or method isn't found.
func traitMethodDecl(traitDecls map[string]*ast.TraitDeclStmt, traitName string, name ast.MethodName) *ast.TraitMethod {
	td, ok := traitDecls[traitName]
	if !ok {
		return nil
	}
	for i := range td.Methods {
		if td.Methods[i].Name.Kind == name.Kind && td.Methods[i].Name.Value == name.Value {
			return &td.Methods[i]
		}
	}
	return nil
}

// collectTraitMethodGroups groups every identifier-named trait-impl method by the
// (trait, method-name) it implements. A call resolved abstractly through a bound
// (`t: Show` → `Show::show`) dispatches, at instantiation, to one of the impls in
// the matching group; its effect is the join over the group (below).
func collectTraitMethodGroups(program *ast.Program) map[typetable.BoundMethodRef][]*ast.TraitMethodImpl {
	groups := map[typetable.BoundMethodRef][]*ast.TraitMethodImpl{}
	for _, node := range program.Statements {
		impl, ok := node.(*ast.TraitImplStmt)
		if !ok {
			continue
		}
		for i := range impl.Methods {
			m := &impl.Methods[i]
			if m.Name.Kind == ast.MethodNameKindIdentifier {
				key := typetable.BoundMethodRef{Trait: impl.TraitName, Method: m.Name.Value}
				groups[key] = append(groups[key], m)
			}
		}
	}
	return groups
}

// boundCallEffect is the effect of a call resolved through a bound: the join over
// every concrete impl of that trait method. A `pure`/`det`/`noalloc` caller is
// only safe if *all* impls of the bound method are — the bound admits any of
// them. With no impls in scope the join is empty (EffectNone).
func boundCallEffect(ref typetable.BoundMethodRef, groups map[typetable.BoundMethodRef][]*ast.TraitMethodImpl, impureMethods map[*ast.TraitMethodImpl]Effect) Effect {
	var found Effect
	for _, m := range groups[ref] {
		found |= impureMethods[m]
	}
	return found
}

// lambdaEffects returns the set of effects lam's body performs that are
// observable outside the call, given defCapture (the capture stack at lam's
// definition site) and the effect sets already inferred for lambdas/methods
// it might call. It mirrors the checks in the main enforcement pass but
// accumulates an Effect bitmask instead of emitting diagnostics, and is used
// only for inference. It does not descend into nested lambdas (they have
// their own boundary). EffectNone means no effect was found (inferred pure).
func lambdaEffects(lam *ast.LambdaExpr, defCapture []scopeBindings, impureLambdas map[*ast.LambdaExpr]Effect, impureMethods map[*ast.TraitMethodImpl]Effect, methodTable *typetable.MethodTable, boundGroups map[typetable.BoundMethodRef][]*ast.TraitMethodImpl, alloc *allocContext, frames *scopeFrames) Effect {
	scope := frames.forLambda(lam)
	locals := make(map[string]bool, len(scope.mutable))
	for name := range scope.mutable {
		locals[name] = true
	}
	// Calls/reads inside lam's own body must resolve against a stack that
	// includes lam's own frame too (e.g. a call to a sibling helper declared
	// alongside lam in the same body), not just the scope it was defined in.
	bodyCapture := pushScope(defCapture, scope)
	mutBorrows := mutBorrowParams(lam)
	var found Effect
	onStmt := func(s ast.Statement) bool {
		switch st := s.(type) {
		case *ast.VarReassignmentStmt:
			if !locals[st.Name] {
				found |= EffectMut
			}
		case *ast.LValueAssignmentStmt:
			if root := rootIdentName(st.Target); root != "" && (mutBorrows[root] || !locals[root]) {
				found |= EffectMut
			}
		case *ast.DerefAssignmentStmt:
			found |= EffectMut
		}
		return true
	}
	onExpr := func(e ast.Expression) bool {
		switch ex := e.(type) {
		case *ast.LambdaExpr:
			return false // nested lambda: separate boundary
		case *ast.IdentifierExpr:
			// Reading captured mutable state is non-deterministic. (An assignment
			// target also visits its root as an IdentifierExpr, but those nodes are
			// already counted by the mutation cases above, so double-counting the
			// bit here is harmless.)
			if !locals[ex.Name] && capturedMutable(bodyCapture, ex.Name) {
				found |= EffectMut
			}
		case *ast.MathAssignOpExpr:
			if !locals[ex.Left.Name] {
				found |= EffectMut
			}
		case *ast.FunctionCallExpr:
			if method, ok := methodTable.Get(ex); ok {
				found |= impureMethods[method]
			} else if ref, ok := methodTable.GetBound(ex); ok {
				// Abstract dispatch through a `where` bound: join over the impls of
				// the bound trait method (pure only if all of them are).
				found |= boundCallEffect(ref, boundGroups, impureMethods)
			} else if name := calleeName(ex.Function); name != "" {
				if e, ok := builtinEffects[name]; ok {
					found |= e
				} else if target, ok := resolveFunction(bodyCapture, name); ok {
					found |= impureLambdas[target]
				} else if !isTypeConversionCall(name) {
					// Cannot resolve to a local lambda or known builtin, and not a
					// pure type-conversion call. Conservatively assume the worst —
					// the callee is imported/external and we can't verify anything
					// about it, including whether it allocates (AllEffects, not just
					// PurityEffects, so `noalloc` catches it too).
					found |= AllEffects
				}
			}
		case *ast.StructInstanceExpr:
			// Constructing a value used as `shared` heap-allocates (allocates
			// reads the flavor the typechecker recorded on this construction).
			if alloc.allocates(ex) {
				found |= EffectAlloc
			}
		case *ast.TupleLiteralExpr:
			// A named tuple literal (`Foo(1, 2)`) or a data construction
			// (`Branch(5)`) — allocates when its recorded flavor is `shared`.
			if alloc.allocates(ex) {
				found |= EffectAlloc
			}
		case *ast.DataConstructorExpr:
			// A nullary constructor (`Leaf`) — allocates when used as `shared`.
			if alloc.allocates(ex) {
				found |= EffectAlloc
			}
		case *ast.AwaitExpr:
			// Awaiting resumes with the result of an external async operation, so
			// its value is non-deterministic — an input effect (forbidden in
			// `pure` and `det`).
			found |= EffectInput
		}
		return true
	}
	walkLambdaBodies(lam, onStmt, onExpr)
	return found
}

// methodEffects is lambdaEffects's counterpart for a trait-impl method: same
// effect checks as lambdaEffects, but over a bare LambdaClause (a method is
// always exactly one clause, never a multi-clause or bare-body lambda) and
// with no mut-borrow parameter set to consult (methods have no mut/own/ref
// parameter-modifier syntax yet, unlike a lambda's — see
// directScopeBindingsForClause). base is the program's top-level capture
// frame; a method's own scope never nests inside another lambda's, so that
// is the entire capture stack besides the method's own.
func methodEffects(m *ast.TraitMethodImpl, base []scopeBindings, impureLambdas map[*ast.LambdaExpr]Effect, impureMethods map[*ast.TraitMethodImpl]Effect, methodTable *typetable.MethodTable, boundGroups map[typetable.BoundMethodRef][]*ast.TraitMethodImpl, alloc *allocContext) Effect {
	scope := directScopeBindingsForClause(&m.Clause)
	locals := make(map[string]bool, len(scope.mutable))
	for name := range scope.mutable {
		locals[name] = true
	}
	bodyCapture := pushScope(base, scope)
	var found Effect
	onStmt := func(s ast.Statement) bool {
		switch st := s.(type) {
		case *ast.VarReassignmentStmt:
			if !locals[st.Name] {
				found |= EffectMut
			}
		case *ast.LValueAssignmentStmt:
			if root := rootIdentName(st.Target); root != "" && !locals[root] {
				found |= EffectMut
			}
		case *ast.DerefAssignmentStmt:
			found |= EffectMut
		}
		return true
	}
	onExpr := func(e ast.Expression) bool {
		switch ex := e.(type) {
		case *ast.LambdaExpr:
			return false // nested lambda: separate boundary
		case *ast.IdentifierExpr:
			if !locals[ex.Name] && capturedMutable(bodyCapture, ex.Name) {
				found |= EffectMut
			}
		case *ast.MathAssignOpExpr:
			if !locals[ex.Left.Name] {
				found |= EffectMut
			}
		case *ast.FunctionCallExpr:
			if method, ok := methodTable.Get(ex); ok {
				found |= impureMethods[method]
			} else if ref, ok := methodTable.GetBound(ex); ok {
				// Abstract dispatch through a `where` bound: join over the impls of
				// the bound trait method (pure only if all of them are).
				found |= boundCallEffect(ref, boundGroups, impureMethods)
			} else if name := calleeName(ex.Function); name != "" {
				if e, ok := builtinEffects[name]; ok {
					found |= e
				} else if target, ok := resolveFunction(bodyCapture, name); ok {
					found |= impureLambdas[target]
				} else if !isTypeConversionCall(name) {
					// Cannot resolve to a local lambda or known builtin, and not a
					// pure type-conversion call. Conservatively assume the worst —
					// the callee is imported/external and we can't verify anything
					// about it, including whether it allocates (AllEffects, not just
					// PurityEffects, so `noalloc` catches it too).
					found |= AllEffects
				}
			}
		case *ast.StructInstanceExpr:
			if alloc.allocates(ex) {
				found |= EffectAlloc
			}
		case *ast.TupleLiteralExpr:
			if alloc.allocates(ex) {
				found |= EffectAlloc
			}
		case *ast.DataConstructorExpr:
			if alloc.allocates(ex) {
				found |= EffectAlloc
			}
		case *ast.AwaitExpr:
			found |= EffectInput
		}
		return true
	}
	ast.WalkExpr(m.Clause.Body, onStmt, onExpr)
	return found
}

// isTypeConversionCall reports whether name is a numeric primitive type name
// used as a type-conversion call (e.g. `i32(x)`, `f64(val)`). These are always
// pure — they don't allocate, observe external state, or mutate anything — so
// they must not be treated as impure even though they have no lambda binding.
func isTypeConversionCall(name string) bool {
	switch name {
	case "i8", "i16", "i32", "i64",
		"u8", "u16", "u32", "u64",
		"f16", "f32", "f64":
		return true
	}
	return false
}

// calleeName renders a call target as a dotted name ("foo", "fmt.println",
// "Arena.new") for lookup against builtinEffects. Returns "" for callees that
// aren't a plain identifier/constructor or member chain (e.g. an
// immediately-invoked lambda). Uppercase names like `Arena` are collected as
// DataConstructorExpr (user_defined_type_name), so that case is handled here
// too — otherwise `Arena.new(...)` would produce just "new".
func calleeName(fn ast.Expression) string {
	switch f := fn.(type) {
	case *ast.IdentifierExpr:
		return f.Name
	case *ast.DataConstructorExpr:
		return f.Constructor
	case *ast.MemberExpr:
		if obj := calleeName(f.Object); obj != "" {
			return obj + "." + f.Property.Name
		}
		return f.Property.Name
	}
	return ""
}
