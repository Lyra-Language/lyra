package checker

import (
	"fmt"
	"strings"

	"github.com/Lyra-Language/lyra/pkg/analyzer/captures"
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
// inside any function marked `pure`, plus — as warnings, the second result — the
// inverse: a function that could have been marked `pure` and was not
// (missingPureBounds, lyra-W018). Two results because the two severities travel
// differently: an effect inside a `pure` function is an error and a missing bound
// is advice, and the pass owns both because both read the one effect fixpoint it
// computes.
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
func CheckPurity(program *ast.Program, scopeTable *symbols.ScopeTable, typeTable *typetable.TypeTable, methodTable *typetable.MethodTable, caps *captures.Table) ([]PurityError, []diag.Diagnostic) {
	base := []scopeBindings{{mutable: mutableGlobals(program), functions: topLevelFunctions(program)}}
	frames := newScopeFrames(program, scopeTable)
	boundGroups := collectTraitMethodGroups(program)
	traitDecls := collectTraitDecls(program)
	signatures := collectMethodSignatures(program, traitDecls)
	// Bound rather than built inline: the inference fills in the allocation *sites* as it
	// goes, and `lyra-E016` reads them back to point at the offending expression.
	// The captures table is here so a *closure construction* can be charged exactly:
	// a nested lambda that captures allocates its environment box, one that does not
	// is the shared pinned static (closures.go's emptyEnv) and stays free.
	alloc := buildAllocContext(program, typeTable, caps)
	impureLambdas, impureMethods, callbacks, methodCallbacks := inferImpurity(collectFuncBindings(program, base, frames), collectMethodImpls(program), base, methodTable, boundGroups, alloc, frames, signatures)
	c := &purityChecker{
		methodCallbacks: methodCallbacks,
		signatures:      signatures,
		impureLambdas:   impureLambdas,
		impureMethods:   impureMethods,
		callbacks:       callbacks,
		assignTargets:   map[*ast.IdentifierExpr]bool{},
		methodTable:     methodTable,
		boundGroups:     boundGroups,
		traitDecls:      traitDecls,
		frames:          frames,
		typeTable:       typeTable,
		allocSites:      alloc,
	}
	for _, node := range program.Statements {
		if stmt, ok := node.(ast.Statement); ok {
			// Top level is an impure context: a nil scope means "not inside a pure
			// function, don't check".
			ast.WalkStmt(stmt, c.stmtVisitor(nil), c.exprVisitor(nil, base, nil))
		}
		if impl, ok := node.(*ast.TraitImplStmt); ok {
			c.checkTraitMethodBounds(impl, base)
		}
		if trait, ok := node.(*ast.TraitDeclStmt); ok {
			c.checkTraitDefaultBounds(trait, base)
		}
	}
	return c.errors, c.missingPureBounds(program)
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
		isPure, isDet, isNoAlloc := c.effectiveMethodBounds(impl, m)
		c.checkBoundedEffects(isDet, isNoAlloc, c.impureMethods[m], m.Clause.GetLocation(), c.allocSites.methodSites[m])
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
		ast.WalkExpr(m.Clause.Body, c.stmtVisitor(sc), c.exprVisitor(sc, childCapture, nil))
	}
}

// checkTraitDefaultBounds holds a trait method's **default body** to the bound the trait
// declares for that method, exactly as checkTraitMethodBounds holds an impl's clause to it.
//
// A default is the body an impl inherits by writing nothing, so a `pure shout` whose
// default prints has broken the contract at the declaration — and the diagnostic belongs
// there, on the body that is wrong, rather than at each impl that inherited it. Without
// this the bound is enforced on every override and not on the thing being overridden,
// which is the wrong way round: the default is the one body the trait's author controls.
//
// The bound is the trait method's own annotation, with no impl to consult: there is no
// override in play, which is exactly what makes this the default's case.
func (c *purityChecker) checkTraitDefaultBounds(trait *ast.TraitDeclStmt, base []scopeBindings) {
	for i := range trait.Methods {
		tm := &trait.Methods[i]
		m := tm.DefaultImpl()
		if m == nil {
			continue
		}
		c.checkBoundedEffects(tm.IsDet, tm.IsNoAlloc, c.impureMethods[m], m.Clause.GetLocation(), c.allocSites.methodSites[m])
		if !tm.IsPure {
			continue
		}
		scope := directScopeBindingsForClause(&m.Clause)
		locals := make(map[string]bool, len(scope.mutable))
		for name := range scope.mutable {
			locals[name] = true
		}
		sc := &funcScope{locals: locals, mutBorrows: map[string]bool{}}
		childCapture := pushScope(base, scope)
		ast.WalkExpr(m.Clause.Body, c.stmtVisitor(sc), c.exprVisitor(sc, childCapture, nil))
	}
}

// effectiveMethodBounds is the bound set an impl method is actually held to: its
// own annotation OR the one the trait declares for that method
// (`trait Show { pure show: … }`), since a bound on the trait is a contract every
// impl must satisfy. Shared by the enforcement half (checkTraitMethodBounds) and
// the missing-bound warning, which must agree about what counts as annotated —
// warning "mark this `pure`" at a method whose trait already says `pure` would be
// advice to write down something the compiler is already enforcing.
func (c *purityChecker) effectiveMethodBounds(impl *ast.TraitImplStmt, m *ast.TraitMethodImpl) (isPure, isDet, isNoAlloc bool) {
	isPure, isDet, isNoAlloc = m.IsPure, m.IsDet, m.IsNoAlloc
	if td := traitMethodDecl(c.traitDecls, impl.TraitName, m.Name); td != nil {
		isPure = isPure || td.IsPure
		isDet = isDet || td.IsDet
		isNoAlloc = isNoAlloc || td.IsNoAlloc
	}
	return isPure, isDet, isNoAlloc
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
	// The third result is the per-lambda callback set; this helper reports each
	// function's *base* effect, which is what a caller asking "what does this function
	// itself do" wants — the callback contribution is per call site by construction.
	impure, _, _, _ := inferImpurity(collectFuncBindings(program, base, frames), collectMethodImpls(program), base, nil, collectTraitMethodGroups(program), buildAllocContext(program, nil, nil), frames, collectMethodSignatures(program, collectTraitDecls(program)))
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

// resolveCallee resolves a *callee* name to its function literal, including the
// namespace-qualified form `maybe.map` that resolveFunction cannot see.
//
// A dotted callee had no resolution at all before, so it fell through to the
// conservative "external, assume every effect" branch — which meant **any** cross-module
// call from a `pure` function was reported impure, and a standard library reached through
// its namespace (`maybe.map(…)`, which is how the combinators were reached while they
// lived in `std.maybe`) was unusable from pure code no matter how pure it was.
//
// The standard library no longer splits that way — receiver-keyed overloading let the
// combinators for both types move into the prelude (08/04) — but the namespace form is
// ordinary syntax any program may use, so the resolution here is not vestigial.
//
// The last segment is what resolves, against the merged program's top-level functions:
// module paths are collapsed into one program before this pass runs, and a `pub` name is
// program-wide unique, so `maybe.map` and the top-level `map` are the same lambda. The
// namespace fallback is taken **only when the object segment names no binding** — the
// backend's namespaceCallee makes the same distinction, because `math.double` with a local
// `math` in scope is an ordinary field read, not a module reference, and resolving it to
// some other module's `double` would attribute the wrong body's effects to it.
func resolveCallee(capture []scopeBindings, name string) (*ast.LambdaExpr, bool) {
	if lam, ok := resolveFunction(capture, name); ok {
		return lam, true
	}
	obj, member, isDotted := strings.Cut(name, ".")
	if !isDotted || strings.Contains(member, ".") {
		return nil, false
	}
	for i := len(capture) - 1; i >= 0; i-- {
		if _, shadowed := capture[i].mutable[obj]; shadowed {
			return nil, false
		}
		if _, shadowed := capture[i].functions[obj]; shadowed {
			return nil, false
		}
	}
	return resolveFunction(capture, member)
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
	// callbacks are this function's *effect-polymorphic* parameters: the
	// function-typed ones it calls. A call through one contributes no effect here —
	// it belongs to whoever supplies the callback, and is charged at that call site.
	// See callableParams and the effect-polymorphism note above lambdaEffects.
	callbacks map[string]int
}

func (s *funcScope) isLocal(name string) bool { return s != nil && s.locals[name] }

// isCallback reports whether name is one of this function's effect-polymorphic
// parameters — a callback whose effects are the caller's, not this function's.
func (s *funcScope) isCallback(name string) bool {
	if s == nil {
		return false
	}
	_, ok := s.callbacks[name]
	return ok
}

// callableParams maps a lambda's parameter names to their positions, so a call to
// one can be recognized inside the body and matched to an argument at a call site.
//
// **A body that immediately matches on its own parameters contributes the names its arms
// bind them to**, not just the declared names. `match f { g => g() }` binds `g` to the whole
// of parameter `f`, so a call through `g` is a call through `f` and costs whatever `f`
// costs; the same holds per-position for a match over a tuple of parameters.
//
// That shape is not a curiosity — it is what every **multi-clause function** becomes.
// `desugarClauses` (typechecker/multi_clause.go) rewrites clauses into exactly this match
// before any of this runs, so by the time purity sees it, `LambdaClauses` is empty and a
// clause's renamed parameter is an arm binding. Until 08/06 this map held only the declared
// names, so a rename made the call an *unresolvable* callee, charged AllEffects — impure and
// allocating — with nothing at the declaration to explain either. That broke the prelude's
// two `filter` combinators (declared `predicate`, destructured as `pred`) and ~25 tests with
// them. The hand-written match had the same hole; the desugaring only made it reachable from
// code that reads as a plain function head. See COMPLETED.md, 08/06.
//
// Only a *whole-value* binding counts (wholeArgumentAlias): the `v` of `Some v` names the
// payload, not the argument, and charging a call through it against the argument's position
// would read the wrong parameter's declared bound.
func callableParams(lam *ast.LambdaExpr) map[string]int {
	if lam == nil {
		return nil
	}
	out := make(map[string]int, len(lam.Parameters))
	for i := range lam.Parameters {
		if n := lam.Parameters[i].Pattern.GetName(); n != "" {
			out[n] = i
		}
	}
	addMatchAliases(lam, out)
	return out
}

// addMatchAliases folds the parameter aliases bound by a body-level `match` on the
// parameters into params.
//
// Only the body's *own* match is read, not every match in the body. This is the shape a
// desugared clause list has and the shape a hand-written equivalent has, and at that
// position the scrutinee's names still mean the parameters — a match nested deeper sits
// under bindings this pass would have to track to know whether `f` is still the parameter.
// Being wrong there is not a missed diagnostic but a misattributed one, so the narrow rule
// is the sound one.
func addMatchAliases(lam *ast.LambdaExpr, params map[string]int) {
	match, ok := lam.Body.(*ast.MatchExpr)
	if !ok {
		return
	}
	// The scrutinee is the parameter itself for a one-parameter function and a tuple of
	// them otherwise — clauseScrutinee's two cases, which arm patterns mirror.
	scrutinees := []ast.Expression{match.Scrutinee}
	if tuple, isTuple := match.Scrutinee.(*ast.TupleLiteralExpr); isTuple {
		scrutinees = tuple.Elements
	}
	// Which parameter each scrutinee position names; -1 for a position that is any other
	// expression, whose arm bindings alias no parameter.
	paramAt := make([]int, len(scrutinees))
	for i, s := range scrutinees {
		paramAt[i] = -1
		if id, isIdent := s.(*ast.IdentifierExpr); isIdent {
			if idx, isParam := params[id.Name]; isParam {
				paramAt[i] = idx
			}
		}
	}

	// A name is dropped rather than guessed at when two arms disagree about which position
	// it names (`(a, b) => …` beside `(b, a) => …`): there is no single argument to charge
	// a call through it against, so it stays an unresolvable callee — what every one of
	// these was before.
	var ambiguous map[string]bool
	for _, arm := range match.MatchArms {
		for i, pat := range armPatterns(arm.Pattern, len(scrutinees)) {
			if paramAt[i] < 0 {
				continue
			}
			n := wholeArgumentAlias(pat)
			if n == "" {
				continue
			}
			if prev, seen := params[n]; seen && prev != paramAt[i] {
				if ambiguous == nil {
					ambiguous = map[string]bool{}
				}
				ambiguous[n] = true
				continue
			}
			params[n] = paramAt[i]
		}
	}
	for n := range ambiguous {
		delete(params, n)
	}
}

// armPatterns splits an arm's pattern into one per scrutinee position, or nil when it does
// not line up with the scrutinee's shape (a wildcard arm over a tuple, say, which binds the
// whole tuple and so aliases no single parameter).
func armPatterns(pat ast.Pattern, n int) []ast.Pattern {
	if n == 1 {
		return []ast.Pattern{pat}
	}
	tuple, ok := pat.(*ast.TuplePattern)
	if !ok || len(tuple.Elements) != n {
		return nil
	}
	return tuple.Elements
}

// wholeArgumentAlias returns the name a pattern binds to the *entire* matched value, or ""
// when it binds no such name. A bare identifier is one; so is the `p` of a `p @ inner`
// binding pattern. Everything else either binds nothing (`_`, a literal) or binds only
// pieces of the value (`Some v`, a struct or tuple pattern), and a piece is not the value.
func wholeArgumentAlias(pat ast.Pattern) string {
	switch p := pat.(type) {
	case *ast.IdentifierPattern:
		if p.Name == "_" {
			return ""
		}
		return p.Name
	case *ast.BindingPattern:
		if p.Name == "_" {
			return ""
		}
		return p.Name
	}
	return ""
}

// declaredBound returns the effect bounds written on a parameter's *type*
// (`f: pure () -> t`), or nil when it is not a function type or carries none.
//
// A parameter with a declared bound is **not** effect-polymorphic: its effects are known
// from the signature, so calling it costs exactly what the bound permits and the enclosing
// function's own purity does not depend on the caller. That is the difference the declared
// half buys — `unwrap_or_else` with `f: pure () -> t` is pure *for every caller*, rather
// than pure at the call sites that happen to pass a pure callback.
func declaredBound(lam *ast.LambdaExpr, idx int) *types.LambdaType {
	if lam == nil || idx < 0 || idx >= len(lam.Parameters) {
		return nil
	}
	lt, ok := lam.Parameters[idx].Type.(*types.LambdaType)
	if !ok || (!lt.IsPure && !lt.IsDet && !lt.IsNoAlloc) {
		return nil
	}
	return lt
}

// signatureBound is declaredBound for a *trait* method: the bounds written on parameter idx
// of the trait's declared signature. A method's parameter types live only there — an impl
// binds patterns, not typed parameters — so this is the trait-side counterpart.
func signatureBound(sig *types.LambdaType, idx int) *types.LambdaType {
	if sig == nil || idx < 0 || idx >= len(sig.Parameters) {
		return nil
	}
	lt, ok := sig.Parameters[idx].Type.(*types.LambdaType)
	if !ok || (!lt.IsPure && !lt.IsDet && !lt.IsNoAlloc) {
		return nil
	}
	return lt
}

// boundEffect is the effect set a declared bound still permits: what a caller may not
// assume is absent. `pure` permits nothing, `det` permits everything outside DetEffects,
// `noalloc` constrains only allocation.
func boundEffect(lt *types.LambdaType) Effect {
	permitted := AllEffects
	if lt.IsPure {
		permitted &^= PurityEffects
	}
	if lt.IsDet {
		permitted &^= DetEffects
	}
	if lt.IsNoAlloc {
		permitted &^= EffectAlloc
	}
	return permitted
}

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
	// methodCallbacks is callbacks' counterpart for trait-impl methods. Positions index the
	// *signature*, where the receiver is parameter 0 — see methodArgumentAt.
	methodCallbacks map[*ast.TraitMethodImpl]map[string]int
	// signatures maps each impl method to the signature its trait declares, the only place
	// a method's parameter types (and so its declared callback bounds) exist.
	signatures map[*ast.TraitMethodImpl]*types.LambdaType
	// callbacks holds each lambda's effect-polymorphic parameters (name → position):
	// the function-typed ones it calls, whose effects are charged at the call site
	// rather than to the definition. See the note above lambdaEffects.
	callbacks map[*ast.LambdaExpr]map[string]int
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
	// typeTable carries the resolved callee of an overloaded call (see calleeFor).
	// Nil-safe, like methodTable, so a caller that has not run the typechecker
	// simply resolves callees by name as this pass always did.
	typeTable *typetable.TypeTable
	// allocSites is the alloc context the effect inference filled in, read here for the
	// one expression `lyra-E016` points at.
	allocSites *allocContext
}

// describeAllocation names what an expression allocates, in the terms the author wrote it
// in. `lyra-E016` used to list every allocating form because the effect is a single bit and
// the site was not recorded; naming the construct is only useful if the name matches what
// is on the line, so these track the syntax rather than the representation.
func describeAllocation(ex ast.Expression) string {
	switch ex.(type) {
	case *ast.ArrayCompExpr:
		return "an array comprehension builds a `[]T`"
	case *ast.ArrayLiteralExpr:
		return "an array literal builds a `[]T`"
	case *ast.ArrayRepeatExpr:
		return "a repeat literal builds a `[]T`"
	case *ast.StringConcatExpr:
		return "`++` builds a new string"
	case *ast.InterpolatedStringExpr:
		return "a `${…}` interpolation builds a new string"
	case *ast.StructInstanceExpr, *ast.TupleLiteralExpr, *ast.DataConstructorExpr:
		return "a `shared`-typed value is constructed"
	case *ast.LambdaExpr:
		return "a closure captures its environment into a heap box"
	}
	return "a value is heap-allocated"
}

// calleeFor resolves the function a *specific call* invokes.
//
// A name is no longer enough: with receiver-keyed overloading several declarations share
// one, and which is meant depends on the receiver's type. The typechecker recorded the
// answer for exactly those calls, so this reads it first and falls back to the by-name
// walk for every other call — which is every call that was resolvable before.
//
// Getting this wrong is silent rather than loud, which is why it is worth a named
// function instead of an inline check at each site: an overloaded call resolved by name
// alone would be scored against whichever member the capture frame happened to hold, so
// a `pure` function could call an impure overload with nothing reported, and a declared
// callback bound would be checked against the wrong member's parameter list.
func calleeFor(tt *typetable.TypeTable, call *ast.FunctionCallExpr, capture []scopeBindings, name string) (*ast.LambdaExpr, bool) {
	if fn, ok := tt.Callee(call); ok {
		return fn, true
	}
	return resolveCallee(capture, name)
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

// enclosing is the lambda whose body is being walked (nil at the top level, and for a
// trait-method body). It is needed to answer one question the capture stack cannot: when a
// callback is handed straight on to a slot with a *declared* bound, whether the enclosing
// function's own parameter declares one strong enough to satisfy it.
func (c *purityChecker) exprVisitor(sc *funcScope, capture []scopeBindings, enclosing *ast.LambdaExpr) func(ast.Expression) bool {
	return func(expr ast.Expression) bool {
		// An overloaded operator is a call to a trait impl method, so a `pure` function
		// using one is as impure as the impl. Reported by naming the *operator*, since
		// that is what the author wrote — the method name `(_+_)` would send them
		// looking for a call that is not there.
		if sc != nil {
			if eff, method := operatorImplEffect(expr, c.methodTable, c.impureMethods, c.boundGroups); eff&PurityEffects != 0 {
				c.report(expr.GetLocation(),
					"pure function uses an operator that dispatches to non-pure trait method %q",
					method)
			}
		}
		switch e := expr.(type) {
		case *ast.LambdaExpr:
			// `det`/`noalloc` are enforced against this lambda's full inferred
			// (transitive) effect set, independent of the enclosing context.
			c.checkBoundedEffects(e.IsDet, e.IsNoAlloc, c.impureLambdas[e], e.GetLocation(), c.allocSites.lambdaSites[e])
			// Default values execute at the call site, in the *enclosing* context.
			for i := range e.Parameters {
				ast.WalkExpr(e.Parameters[i].DefaultValue, c.stmtVisitor(sc), c.exprVisitor(sc, capture, enclosing))
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
				// The callback parameters inference found for this lambda travel with the
				// scope, so the call check below can tell "calls its own callback"
				// (polymorphic, not this function's effect) from "calls something impure".
				child = &funcScope{locals: locals, mutBorrows: mutBorrowParams(e), callbacks: c.callbacks[e]}
			}
			childCapture := pushScope(capture, scope)
			walkLambdaBodies(e, c.stmtVisitor(child), c.exprVisitor(child, childCapture, e))
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
			// A declared bound is a contract on the *callee's signature*, so it is checked
			// at every call site regardless of whether the caller is itself pure — unlike
			// the polymorphic case below, which only matters when the caller has something
			// to protect.
			if method, ok := c.methodTable.Get(e); ok {
				c.checkDeclaredMethodBounds(capture, enclosing, e, method)
			} else if name := calleeName(e.Function); name != "" {
				c.checkDeclaredCallbackBounds(capture, enclosing, e, name)
			}
			if sc != nil {
				if c.methodTable.IsBuiltinMethod(e) {
					// A compiler builtin method (`x.wrapping_mul(y)`, `x.floor()`) is
					// pure arithmetic. This is the *reporting* copy of the ladder that
					// lambdaEffects and methodEffects also carry — all three ask "what
					// does this call call?" and all three must answer alike, or a call
					// is charged no effect while still being reported as impure.
					//
					// Nothing to report either way: the one builtin method that is not
					// free is `s.slice(…)`, and what it costs is EffectAlloc, which
					// this walk does not report on (it reports purity violations, and
					// allocation is orthogonal). `noalloc` sees it through the two
					// inference copies below.
				} else if method, ok := c.methodTable.Get(e); ok {
					// Mask with PurityEffects: only correctness effects (mut/io)
					// make a callee non-pure — EffectAlloc is orthogonal, so a
					// pure function may call a method that merely allocates.
					//
					// The effect is of *this call site*: a method taking a callback is
					// polymorphic in it exactly as a free function is, so what the
					// argument supplied here does is part of the cost.
					eff := methodCallEffect(method, e, capture, c.impureLambdas, c.impureMethods,
						c.callbacks, c.methodCallbacks, callbackParamsOf(sc), nil)
					if eff&PurityEffects != 0 {
						if c.impureMethods[method]&PurityEffects != 0 {
							c.report(e.GetLocation(),
								"pure function calls non-pure trait method %q", method.Name.GetName())
						} else {
							c.report(e.GetLocation(),
								"pure function calls trait method %q with an impure callback argument; "+
									"the callback's effects are this call's", method.Name.GetName())
						}
					}
				} else if ref, ok := c.methodTable.GetBound(e); ok {
					// Abstract dispatch through a `where` bound: pure only if every
					// impl of the bound trait method is pure.
					if boundCallEffect(ref, c.boundGroups, c.impureMethods)&PurityEffects != 0 {
						c.report(e.GetLocation(),
							"pure function calls non-pure trait method %q via a bound", ref.Method)
					}
				} else if name := calleeName(e.Function); name != "" {
					c.checkCallPurity(sc, capture, enclosing, e, name)
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
//
// callbackParamsOf is the enclosing function's polymorphic parameters, or nil outside a
// pure context. Passing them into an argument-effect walk is what lets a callback handed
// straight on stay polymorphic rather than tainting the call it is handed to.
func callbackParamsOf(sc *funcScope) map[string]int {
	if sc == nil {
		return nil
	}
	return sc.callbacks
}

// checkDeclaredCallbackBounds enforces the *declared* half of effect polymorphism: a
// parameter typed `f: pure () -> t` constrains every function handed to it.
//
// Unlike the inferred half, this is a property of the callee's signature rather than of the
// call, so it holds for every caller — an impure program may not quietly supply an impure
// callback to a `pure`-declared slot just because it had nothing to protect itself.
//
// The argument's *inferred* effect is what is compared, not its annotation. Requiring the
// word `pure` on every lambda literal a program writes would make the bound cost more than
// it is worth, and inference is exactly what this pass has that the typechecker does not —
// which is also why assignability deliberately lets the two types through (see
// isAssignable) instead of reporting a shape mismatch that explains nothing.
func (c *purityChecker) checkDeclaredCallbackBounds(capture []scopeBindings, enclosing *ast.LambdaExpr, call *ast.FunctionCallExpr, name string) {
	callee, ok := calleeFor(c.typeTable, call, capture, name)
	if !ok {
		return
	}
	for i := range callee.Parameters {
		bound := declaredBound(callee, i)
		if bound == nil || i >= len(call.Arguments) {
			continue
		}
		c.reportBoundViolation(capture, enclosing, call.Arguments[i], bound, name,
			fmt.Sprintf("%q", callee.Parameters[i].Pattern.GetName()))
	}
}

// checkDeclaredMethodBounds is checkDeclaredCallbackBounds for a trait-method call. The
// bounds come from the *trait's* signature (a method's parameter types live nowhere else),
// and the arguments are offset by the receiver — see methodArgumentAt.
func (c *purityChecker) checkDeclaredMethodBounds(capture []scopeBindings, enclosing *ast.LambdaExpr, call *ast.FunctionCallExpr, method *ast.TraitMethodImpl) {
	sig := c.signatures[method]
	if sig == nil {
		return
	}
	for i := range sig.Parameters {
		bound := signatureBound(sig, i)
		if bound == nil {
			continue
		}
		arg, ok := methodArgumentAt(call, i)
		if !ok {
			continue // arity mismatch, or the receiver — reported elsewhere
		}
		c.reportBoundViolation(capture, enclosing, arg, bound, method.Name.GetName(), parameterLabel(sig, i))
	}
}

// parameterLabel names a signature parameter for a diagnostic. A trait signature carries
// types, not names, so the position is the only handle there is.
func parameterLabel(sig *types.LambdaType, idx int) string {
	return fmt.Sprintf("parameter %d", idx)
}

// reportBoundViolation is the shared verdict for one argument against one declared bound,
// so the free-function and trait-method paths cannot drift on what counts as a violation or
// on how it is worded.
func (c *purityChecker) reportBoundViolation(capture []scopeBindings, enclosing *ast.LambdaExpr, arg ast.Expression, bound *types.LambdaType, calleeName, paramLabel string) {
	actual, known := c.suppliedEffect(arg, capture, enclosing)
	if !known {
		c.report(arg.GetLocation(),
			"%s: cannot verify that this argument satisfies the declared `%s` bound on %s; "+
				"pass a function literal or a named function, or declare the parameter it comes from",
			calleeName, strings.TrimSpace(bound.EffectPrefix()), paramLabel)
		return
	}
	if bad := actual &^ boundEffect(bound); bad != 0 {
		c.report(arg.GetLocation(),
			"%s: argument for %s must be `%s`, but it %s",
			calleeName, paramLabel, strings.TrimSpace(bound.EffectPrefix()), effectDescription(bad))
	}
}

// suppliedEffect is the effect of a value supplied for a *bounded* parameter. known is
// false when this pass cannot see through to an answer, which is a rejection rather than a
// pass: a bound the compiler cannot check is not a bound.
//
// The `enclosing` case is what lets a bound compose. A callback handed straight on carries
// no body to inspect, so its own *declared* bound is the answer — which is why an
// unconstrained parameter cannot be forwarded into a constrained slot, and the diagnostic
// tells the caller to declare it.
func (c *purityChecker) suppliedEffect(arg ast.Expression, capture []scopeBindings, enclosing *ast.LambdaExpr) (Effect, bool) {
	switch a := arg.(type) {
	case *ast.LambdaExpr:
		if len(c.callbacks[a]) > 0 {
			return 0, false // polymorphic itself; nothing here supplies its callbacks
		}
		return c.impureLambdas[a], true
	case *ast.IdentifierExpr:
		if lam, ok := resolveCallee(capture, a.Name); ok {
			if len(c.callbacks[lam]) > 0 {
				return 0, false
			}
			return c.impureLambdas[lam], true
		}
		if params := callableParams(enclosing); params != nil {
			if idx, isParam := params[a.Name]; isParam {
				if b := declaredBound(enclosing, idx); b != nil {
					return boundEffect(b), true
				}
				return 0, false // an unconstrained parameter promises nothing
			}
		}
	}
	return 0, false
}

// effectDescription names the offending bits for a bound violation, in the order a reader
// is most likely to care about them.
func effectDescription(e Effect) string {
	switch {
	case e.Has(EffectMut):
		return "mutates state outside itself"
	case e.Has(EffectInput):
		return "reads external input"
	case e.Has(EffectOutput):
		return "writes output"
	case e.Has(EffectRand):
		return "draws from a random source"
	case e.Has(EffectTime):
		return "reads the system clock"
	case e.Has(EffectAlloc):
		return "heap-allocates"
	}
	return "has an effect the bound forbids"
}

// checkCallPurity reports a call inside a `pure` function that is not pure *at this call
// site* — which, for a higher-order callee, depends on the arguments and not on the callee
// alone. See the effect-polymorphism note above lambdaEffects.
//
// Three outcomes, and the diagnostic distinguishes them because the fix differs:
//   - the callee is this function's own callback: nothing to report. A `pure` annotation
//     constrains the function's own body, and effects arriving through its parameters
//     belong to whoever supplied them.
//   - the callee's own base effect is impure: the old message, naming the callee.
//   - the callee is pure but an argument is not: name the *argument*, since the callee is
//     innocent and pointing at it would send the reader to the wrong file.
func (c *purityChecker) checkCallPurity(sc *funcScope, capture []scopeBindings, enclosing *ast.LambdaExpr, call *ast.FunctionCallExpr, name string) {
	if sc.isCallback(name) {
		return
	}
	// A call through a parameter carrying a *declared* bound costs exactly what the bound
	// still permits — known from the signature, so there is nothing to charge the caller
	// and nothing to report unless the bound itself allows an impurity. Without this the
	// pure walk would flag `f()` inside `pure (…, f: pure () -> t) => … f() …`, which is
	// precisely the function the declared half exists to make writable.
	if params := callableParams(enclosing); params != nil {
		if idx, isParam := params[name]; isParam {
			if bound := declaredBound(enclosing, idx); bound != nil {
				if boundEffect(bound)&PurityEffects != 0 {
					c.report(call.GetLocation(),
						"pure function calls %q, whose declared `%s` bound still permits an effect",
						name, strings.TrimSpace(bound.EffectPrefix()))
				}
				return
			}
		}
	}
	callee, resolved := calleeFor(c.typeTable, call, capture, name)
	if !resolved {
		if c.isImpureCallee(capture, name) {
			c.report(call.GetLocation(), "pure function calls impure function %q", name)
		}
		return
	}
	if c.impureLambdas[callee]&PurityEffects != 0 {
		c.report(call.GetLocation(), "pure function calls impure function %q", name)
		return
	}
	// The callee contributes nothing of its own; anything impure came in through a
	// callback this site supplied.
	for cbName, idx := range c.callbacks[callee] {
		if idx >= len(call.Arguments) {
			continue // arity mismatch — the typechecker reports it
		}
		arg := call.Arguments[idx]
		eff := argumentEffect(arg, capture, c.impureLambdas, c.callbacks, sc.callbacks, nil)
		if eff&PurityEffects == 0 {
			continue
		}
		// Name the parameter as the *callee's signature* spells it. cbName is whatever
		// binding the callee's body called it through, which for a multi-clause function
		// is an arm binding (`(None, g) => g()` for a parameter declared `f`) — a name
		// the caller cannot see anywhere and cannot act on.
		c.report(arg.GetLocation(),
			"pure function calls %q with an impure %s argument; the callback's effects are this call's",
			name, declaredParamName(callee, idx, cbName))
	}
}

// declaredParamName is how the callee's signature spells parameter idx, falling back to
// whatever name the caller had when the head does not bind it plainly.
func declaredParamName(callee *ast.LambdaExpr, idx int, fallback string) string {
	if callee == nil || idx < 0 || idx >= len(callee.Parameters) {
		return fallback
	}
	if n := callee.Parameters[idx].Pattern.GetName(); n != "" {
		return n
	}
	return fallback
}

func (c *purityChecker) isImpureCallee(capture []scopeBindings, name string) bool {
	// **A user binding is resolved first**, and only then the builtin table — the
	// order the typechecker resolves a call in (scope, then print/println/panic on a
	// miss), so the two cannot disagree about which declaration a name means.
	//
	// Consulting the table first classified a *user's own* function by the builtin's
	// entry, which is wrong in both directions and unsound in one: a user `print` that
	// is pure was reported impure (noise), and — once `panic` joined the table as
	// EffectNone — a user `panic` that mutates or writes would have been waved through
	// as pure. The name is not the callee.
	if lam, ok := resolveCallee(capture, name); ok {
		// Mask with PurityEffects: an alloc-only callee is still pure to call
		// from a pure function (EffectAlloc is orthogonal to purity).
		return c.impureLambdas[lam]&PurityEffects != 0
	}
	if e, ok := builtinEffects[name]; ok {
		// Known builtin: only flag if it has a purity-violating effect.
		// Builtins with EffectAlloc only (e.g. Arena.new) are fine to call from
		// a pure function — allocation is orthogonal to purity.
		return e&PurityEffects != 0
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
func (c *purityChecker) checkBoundedEffects(isDet, isNoAlloc bool, effects Effect, loc ast.Location, allocSite ast.Expression) {
	if isDet {
		if bad := effects & DetEffects; bad != 0 {
			c.reportCode(diag.CodeEffectBoundViolation, loc,
				"`det` function %s; a `det` function must be reproducible from its inputs — thread external state (a seed, the tick) through parameters instead", nondeterminismDescription(bad))
		}
	}
	if isNoAlloc && effects.Has(EffectAlloc) {
		// Point at the allocation when it is in this body, and fall back to naming the
		// forms when it is not. The two cases are genuinely different: a direct
		// allocation has a line to fix, while one arriving through a callee has only a
		// call here — pointing at that call would name a line that does not allocate.
		if allocSite != nil {
			c.reportCode(diag.CodeEffectBoundViolation, loc,
				"`noalloc` function heap-allocates: %s at %s. A `noalloc` function must not allocate",
				describeAllocation(allocSite), allocSite.GetLocation().Pretty())
		} else {
			c.reportCode(diag.CodeEffectBoundViolation, loc,
				"`noalloc` function heap-allocates by calling something that allocates; a `noalloc` function must not allocate. Allocating forms are a `shared`-typed construction, a dynamic array (`[]T`, including a comprehension), a newly built string (`++`, interpolation, or `slice`), and a closure that captures")
		}
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
	// handle) must read as interior-mutable despite being a plain `let`. Reachable
	// only from a program that already fails to check (lyra-E050 refuses `with`);
	// kept for the reason declaredMutability's WithStmt case is.
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
		//
		// `with` is refused outright since 08/13 (lyra-E050, arenas unimplemented),
		// so this is unreachable from a *buildable* program — kept because the purity
		// pass still runs on a program that has errors, and this is the conservative
		// reading (it makes purity stricter). The arena *discharge*, which was the
		// unsound direction, is gone; see buildAllocContext.
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
	// captures answers whether a nested lambda captures anything, which is exactly
	// whether constructing it allocates: a capturing closure heap-boxes its
	// environment per construction (closures.go's buildEnv), a capture-free one is
	// the shared pinned static (emptyEnv) and costs nothing — under the dev lowering
	// *and* under Lambda Set Specialization, so the exemption is not a bet on the
	// release tier. nil for entry points without a captures pass (InferredEffects) —
	// closure charging is then disabled, like the rest of alloc detection there.
	captures *captures.Table
	// lambdaSites / methodSites record the **first** expression found to allocate in
	// each callable, so `lyra-E016` can point at it instead of listing every form the
	// language can allocate with.
	//
	// First rather than all: one precise location is what a reader acts on, and the
	// second allocation in a `noalloc` function is not a separate mistake — removing the
	// bound or the allocation fixes both. Parallel maps keyed by lambda and by method,
	// matching `impureLambdas`/`impureMethods`, because the two callables have no common
	// type and a `map[any]` would lose that at every read.
	//
	// **Only a *direct* allocation is recorded.** An allocation reaching a function
	// through a *callee* has no expression in this body to name — the call is here, the
	// allocation is not — so those keep the form-listing message. Recording the call site
	// instead would point at a line that does not allocate.
	lambdaSites map[*ast.LambdaExpr]ast.Expression
	methodSites map[*ast.TraitMethodImpl]ast.Expression
}

// noteLambda / noteMethod record a callable's first directly-allocating expression.
//
// The effect inference runs to a fixpoint, so each walk happens several times; keeping the
// first write makes the reported site independent of how many passes convergence took,
// which is the difference between a stable diagnostic and one that moves when an unrelated
// function is edited.
func (a *allocContext) noteLambda(lam *ast.LambdaExpr, ex ast.Expression) {
	if a == nil || a.lambdaSites == nil || a.lambdaSites[lam] != nil {
		return
	}
	a.lambdaSites[lam] = ex
}

func (a *allocContext) noteMethod(m *ast.TraitMethodImpl, ex ast.Expression) {
	if a == nil || a.methodSites == nil || a.methodSites[m] != nil {
		return
	}
	a.methodSites[m] = ex
}

// table exposes the type table for the callee lookup an overloaded call needs
// (calleeFor). It rides along here rather than becoming a ninth parameter on
// inferImpurity/lambdaEffects: the context is already threaded to every point in the
// effect walk that inspects a call, which is exactly where the resolved callee is
// wanted. Nil-safe, so the typechecker-less entry points keep working.
func (a *allocContext) table() *typetable.TypeTable {
	if a == nil {
		return nil
	}
	return a.typeTable
}

// allocates reports whether a value-*producing* expr heap-allocates: its recorded type is
// heap-represented.
//
// The question is about **representation, not flavor**. Until 08/04 this asked only whether
// the flavor was `shared`, which is the right question for a construction — allocation is a
// use-site property there, with no declaration-level flavor — and the wrong one for a
// `[]T`, which is a ref-counted box *whatever* flavor it carries. So
// `pure noalloc (…) -> []i64 => [1, 2, 3]` was accepted, and once `map`/`filter` for arrays
// went into the prelude as comprehensions the annotation was being claimed on functions
// that allocate per element.
//
// It stays a question about the *expression*, not merely its type: a `[]T` **identifier**
// is heap-represented and allocates nothing, so only the producing forms are asked (the
// `case`s in lambdaEffects/methodEffects). Asking `heapRepresented` of every expression
// would charge every mention of an array to its function.
func (a *allocContext) allocates(expr ast.Expression) bool {
	if a == nil || a.typeTable == nil {
		return false
	}
	t, ok := a.typeTable.Get(expr)
	return ok && heapRepresented(t)
}

// allocatesByForm reports whether an expression heap-allocates because of **what it is**,
// independently of its type — string concatenation and `${…}` interpolation.
//
// Strings need this second rule because the type cannot carry the answer: a literal, a
// `++` and an interpolation are all `string`, and only the last two allocate. A literal
// interns as a *pinned static box* whose retain/release are no-ops, while `++` and
// interpolation each build a fresh ref-counted box (`lowerStringConcat`,
// `lowerInterpolatedString`). That is the opposite of the array case, where the type is
// exactly what distinguishes the allocating `[]T` from the stack-resident `[N]T` — so the
// two rules are kept apart rather than folded into one predicate that would have to mean
// something different for each.
//
// Gated on the TypeTable like `allocates`, even though it never reads one. The AST-only
// `InferredEffects` entry point deliberately reports *no* allocation at all; letting it
// find strings but not `shared` values or arrays would make its answer partial in a way a
// caller cannot detect, which is worse than the documented nothing.
func (a *allocContext) allocatesByForm(expr ast.Expression) bool {
	if a == nil || a.typeTable == nil {
		return false
	}
	switch expr.(type) {
	case *ast.StringConcatExpr, *ast.InterpolatedStringExpr:
		return true
	}
	return false
}

// closureAllocates reports whether constructing the nested lambda e heap-allocates:
// true exactly when it captures, since a capturing closure's environment is a fresh
// ref-counted box per construction while a capture-free one shares a pinned static.
//
// This closed the audit finding that `noalloc` silently did not bind for closures
// (08/12): a `noalloc` function containing a capturing lambda checked clean while
// its emitted body called `lyra_rc_alloc` on every invocation — the `slice` hole's
// shape again, a bound that silently stops binding. The old position ("`noalloc` is
// defined against the *release* lowering") deferred the charge until Lambda Set
// Specialization; but LSS is not built, and the capture split makes the deferral
// unnecessary anyway — a capture-free closure is free under both tiers, and a
// capturing one that *escapes* allocates under both too. If LSS later makes a
// non-escaping capturing closure free, relaxing this is a compatible loosening;
// today's charge is what the shipped compiler actually does.
func (a *allocContext) closureAllocates(e *ast.LambdaExpr) bool {
	if a == nil || a.captures == nil {
		return false
	}
	return len(a.captures.Of(e)) > 0
}

// heapRepresented reports whether a value of t lives in a heap box.
//
// Two ways to be one, and they are genuinely different questions. A `shared` **flavor** is
// a use-site decision: the same `Node{…}` is stack or heap depending on how the value is
// used, so the flavor recorded on the construction is what decides. A **dynamic array** is
// heap-boxed by its own nature — `[]T` lowers to a pointer to `{ rc, len, [0 x T] }` before
// the flavor is even consulted (`lowerType`) — so there is no flavor that makes `[1, 2, 3]`
// not allocate.
//
// **Strings are not here**, and not because they do not allocate — they do, but which ones
// is not a question about the type (see allocatesByForm). Still deferred: **escaping
// closures**, boxed in the dev lowering and free under Lambda Set Specialization, so what
// `noalloc` should say about one depends on the tier it is defined against. See todo.md.
func heapRepresented(t types.Type) bool {
	if types.AllocationOf(t) == types.Shared {
		return true
	}
	_, isDynamicArray := types.StripNewtype(t).(types.DynamicArrayType)
	return isDynamicArray
}

// buildAllocContext records the TypeTable (for reading each construction's
// resolved flavor) and the captures table. typeTable may be nil (see
// allocContext.typeTable).
//
// **It used to discharge arenas, and that was the phantom's teeth** (removed
// 08/13, lyra-E050). Every expression lexically inside a `with` body was marked
// discharged and every allocation predicate consulted the mark, so wrapping a
// `shared` construction in `with a = 42 { … }` silently turned lyra-E016 off and
// `noalloc` stopped binding — for a statement that has no lowering and whose
// arena expression nothing type-checked. A bound that quietly stops binding is
// worse than no bound, and this one was discharged into an allocator that does
// not exist.
//
// If arenas are built, the discharge comes back **with an escape analysis, not
// without one**: the old note conceded that a `shared` value built inside a
// `with` block and returned out still escapes, and treating everything lexically
// inside as discharged was already the approximation standing in for that
// analysis.
func buildAllocContext(program *ast.Program, typeTable *typetable.TypeTable, caps *captures.Table) *allocContext {
	return &allocContext{
		typeTable:   typeTable,
		captures:    caps,
		lambdaSites: map[*ast.LambdaExpr]ast.Expression{},
		methodSites: map[*ast.TraitMethodImpl]ast.Expression{},
	}
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
		// An extern is a top-level function like any other here, which is what makes a
		// call to one resolvable by the fixpoint and reportable by lyra-E011 — every
		// extern is `unsafe` to call, so `unsafeFunctions` finds it through this.
		if ext, ok := node.(*ast.ExternDeclStmt); ok {
			fns[ext.Name] = ext.Func()
		}
	}
	return fns
}

// externEffects is what an extern's *declared bound* permits, and it is where a foreign
// function's effects come from — there is no body to infer them from.
//
// **The default is every effect**, matching the unresolved-callee rule this pass already
// uses: a function whose body the compiler cannot see may do anything. A bound narrows it,
// and the narrowing is what `unsafe` on the declaration asserts, since nothing here can
// check it. So for Lyra code a bound is a promise the compiler *checks*; for an extern it
// is a promise the compiler *records*.
//
// `noalloc` is the one that needs saying out loud: it clears EffectAlloc, which tracks the
// ref-counted boxes the ownership pass reasons about. A foreign `malloc` is not in that
// ledger and this bound does not claim it is.
func externEffects(lam *ast.LambdaExpr) Effect {
	switch {
	case lam.IsPure:
		return EffectNone
	case lam.IsDet:
		// `det` permits exactly what determinism allows: output, mutation, allocation.
		e := EffectOutput | EffectMut | EffectAlloc
		if lam.IsNoAlloc {
			e &^= EffectAlloc
		}
		return e
	case lam.IsNoAlloc:
		return AllEffects &^ EffectAlloc
	}
	return AllEffects
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
	// An extern's lambda is not reachable by walking the program — it hangs off the
	// declaration rather than sitting in an expression — so it is added here. Without an
	// entry its effect stays the zero value, which reads as *pure*: the one answer a
	// function nobody can see the body of must never get.
	for _, node := range program.Statements {
		if ext, ok := node.(*ast.ExternDeclStmt); ok {
			defs[ext.Func()] = base
		}
	}
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
// It also returns each lambda's **callback parameters** — the function-typed ones it
// calls, which is what makes its effect polymorphic; see the note above lambdaEffects.
// That set is part of the same fixpoint, because discovering a callback can change a
// caller's effect (it stops paying AllEffects for the call) and discovering an effect can
// reveal a callback one round later.
func inferImpurity(lambdaDefs map[*ast.LambdaExpr][]scopeBindings, methods []*ast.TraitMethodImpl, base []scopeBindings, methodTable *typetable.MethodTable, boundGroups map[typetable.BoundMethodRef][]*ast.TraitMethodImpl, alloc *allocContext, frames *scopeFrames, signatures map[*ast.TraitMethodImpl]*types.LambdaType) (map[*ast.LambdaExpr]Effect, map[*ast.TraitMethodImpl]Effect, map[*ast.LambdaExpr]map[string]int, map[*ast.TraitMethodImpl]map[string]int) {
	impureLambdas := map[*ast.LambdaExpr]Effect{}
	impureMethods := map[*ast.TraitMethodImpl]Effect{}
	callbacks := map[*ast.LambdaExpr]map[string]int{}
	methodCallbacks := map[*ast.TraitMethodImpl]map[string]int{}
	// Fixpoint over an effect *set*: a callable's effect can only grow as more
	// of its callees are analyzed, so each pass ORs in any newly found bits and
	// we iterate until nothing changes. (This must recompute every callable
	// each pass — not skip the ones already non-pure — because a function found
	// to allocate early might still gain an io/mut bit from a callee resolved
	// later; a boolean "already impure, skip" early-out would miss that.)
	for {
		changed := false
		for lam, capture := range lambdaDefs {
			effects, cbs := lambdaEffects(lam, capture, impureLambdas, impureMethods, methodTable, boundGroups, alloc, frames, callbacks)
			e := impureLambdas[lam] | effects
			if e != impureLambdas[lam] {
				impureLambdas[lam] = e
				changed = true
			}
			// The callback set grows monotonically too, and never shrinks: a parameter
			// found to be called stays called. Merging (rather than replacing) keeps the
			// fixpoint monotone even though an earlier round may have seen a call the
			// current one resolves differently.
			for name, idx := range cbs {
				if _, seen := callbacks[lam][name]; !seen {
					if callbacks[lam] == nil {
						callbacks[lam] = map[string]int{}
					}
					callbacks[lam][name] = idx
					changed = true
				}
			}
		}
		for _, m := range methods {
			effects, cbs := methodEffects(m, base, impureLambdas, impureMethods, methodTable,
				boundGroups, alloc, signatures[m], callbacks, methodCallbacks)
			e := impureMethods[m] | effects
			if e != impureMethods[m] {
				impureMethods[m] = e
				changed = true
			}
			// Monotone, exactly as the lambda side: a parameter found to be called stays
			// called, so the sets only grow and the fixpoint still terminates.
			for name, idx := range cbs {
				if _, seen := methodCallbacks[m][name]; !seen {
					if methodCallbacks[m] == nil {
						methodCallbacks[m] = map[string]int{}
					}
					methodCallbacks[m][name] = idx
					changed = true
				}
			}
		}
		if !changed {
			break
		}
	}
	return impureLambdas, impureMethods, callbacks, methodCallbacks
}

// collectMethodImpls gathers every trait-impl method declared in program,
// keyed by pointer (impl.Methods[i]'s address is stable since Methods is
// never reallocated after collection) so it matches the identity methodTable
// resolutions use.
// collectMethodImpls gathers every method body the fixpoint must account for: an impl's
// own clauses, and a **trait method's default**, which is a body like any other and is
// dispatched to exactly as an impl's is.
//
// A default is gathered through ast.TraitMethod.DefaultImpl(), not by building a
// TraitMethodImpl here — the effect map is keyed by pointer and the typechecker's
// resolutions name that same instance, so a second one would leave every call to a
// default charged the unresolved-callee default (AllEffects) while the body it actually
// runs sat in the map unread.
func collectMethodImpls(program *ast.Program) []*ast.TraitMethodImpl {
	var methods []*ast.TraitMethodImpl
	for _, node := range program.Statements {
		switch decl := node.(type) {
		case *ast.TraitImplStmt:
			for i := range decl.Methods {
				methods = append(methods, &decl.Methods[i])
			}
		case *ast.TraitDeclStmt:
			for i := range decl.Methods {
				if m := decl.Methods[i].DefaultImpl(); m != nil {
					methods = append(methods, m)
				}
			}
		}
	}
	return methods
}

// collectMethodSignatures maps each trait-impl method to the signature its trait declares
// for it. A method's *parameters* live in the trait declaration, not in the impl (an impl
// binds patterns: `show = (self) => …`), so this is the only way the effect passes can see
// what a method's parameters are typed as — which is what declared callback bounds
// (`f: pure () -> t`) on a trait method need.
//
// A method with no matching declaration (an impl of an undeclared method, already an error)
// simply gets no entry, and the passes fall back to their conservative behaviour.
func collectMethodSignatures(program *ast.Program, traitDecls map[string]*ast.TraitDeclStmt) map[*ast.TraitMethodImpl]*types.LambdaType {
	sigs := map[*ast.TraitMethodImpl]*types.LambdaType{}
	for _, node := range program.Statements {
		impl, ok := node.(*ast.TraitImplStmt)
		if !ok {
			continue
		}
		for i := range impl.Methods {
			if decl := traitMethodDecl(traitDecls, impl.TraitName, impl.Methods[i].Name); decl != nil {
				sigs[&impl.Methods[i]] = decl.Signature
			}
		}
	}
	return sigs
}

// clauseParams maps a trait-impl method's bound parameter names to their positions in the
// *signature*, so a call through one can be recognized in the body. Position 0 is the
// receiver: a trait signature includes `Self`, while a call site writes it as the receiver
// rather than an argument — see methodArgumentAt for the offset that follows from this.
//
// Non-identifier patterns (a destructuring receiver) have no name to call, so they are
// skipped rather than mismatching the signature.
func clauseParams(m *ast.TraitMethodImpl) map[string]int {
	if m == nil {
		return nil
	}
	out := make(map[string]int, len(m.Clause.Patterns))
	for i, pat := range m.Clause.Patterns {
		if ip, ok := pat.(*ast.IdentifierPattern); ok && ip.Name != "" {
			out[ip.Name] = i
		}
	}
	return out
}

// methodArgumentAt returns the expression supplied for signature parameter idx at a method
// call site, and whether there is one.
//
// **The offset is the whole point.** A trait signature counts `Self` as parameter 0, but
// `x.foo(a, b)` puts the receiver outside `call.Arguments` — so signature index i is
// `Arguments[i-1]`, and index 0 is the receiver, which is never a callback (it is the value
// being dispatched on). Reading `Arguments[i]` instead would check every callback against
// the argument one place to its right, silently, since a wrong function-typed argument
// type-checks against the wrong parameter just as well.
func methodArgumentAt(call *ast.FunctionCallExpr, idx int) (ast.Expression, bool) {
	if idx <= 0 || idx-1 >= len(call.Arguments) {
		return nil, false
	}
	return call.Arguments[idx-1], true
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

// collectTraitMethodGroups groups every trait-impl method by the (trait, method-name) it
// implements. A call resolved abstractly through a bound (`t: Show` → `Show::show`)
// dispatches, at instantiation, to one of the impls in the matching group; its effect is
// the join over the group (below).
//
// **Operator-named methods are included**, keyed by `MethodName.Key` so prefix `-` and
// binary `-` land in different groups. They were filtered out until 08/08, back when
// nothing dispatched to them; once an *operator* could resolve through a bound
// (`a + b` under `where t: Add`) the filter meant the join ran over an empty group and
// answered EffectNone — so a `pure` function using a bound operator whose impl printed
// type-checked clean. The same shape as the identifier filter removed from
// resolveTraitMethod a day earlier, and the same lesson: a filter written when a kind
// could not occur becomes a silent hole the day it can.
func collectTraitMethodGroups(program *ast.Program) map[typetable.BoundMethodRef][]*ast.TraitMethodImpl {
	groups := map[typetable.BoundMethodRef][]*ast.TraitMethodImpl{}
	for _, node := range program.Statements {
		impl, ok := node.(*ast.TraitImplStmt)
		if !ok {
			continue
		}
		for i := range impl.Methods {
			m := &impl.Methods[i]
			key := typetable.BoundMethodRef{Trait: impl.TraitName, Method: m.Name.Key()}
			groups[key] = append(groups[key], m)
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
// Effect polymorphism over function-typed parameters.
//
// A higher-order function's effects are not a property of the function alone: what
// `unwrap_or_else(m, f)` does depends entirely on `f`. Charging the *definition* for a
// call it cannot see — the old behavior, where an unresolvable callee taints AllEffects —
// made every combinator maximally impure, and the taint spread to callers, so no
// callback-taking function was callable from `pure` code at all. That is the whole
// prelude combinator layer.
//
// The scheme here is the *inferred* half of todo.md's entry: a function's stored effect is
// its **base** — everything its own body does — plus a set of **callback parameters**, the
// function-typed ones it calls. A call site then pays base ∪ the effects of the arguments
// actually supplied for those parameters (callEffect). So `unwrap_or_else(m, () -> i64 => 0)`
// is pure and `unwrap_or_else(m, () -> i64 => read())` is not, from one definition.
//
// Two consequences worth stating, because they are what make the annotations usable:
//   - An annotation constrains a function's **own body**. `pure` on a higher-order function
//     says "contributes no effects of its own", not "no effect can ever occur through me" —
//     that second claim is not the function's to make while its callback is unconstrained,
//     and is what the *declared* half is for (`f: pure () -> t`; see declaredBound). So the
//     prelude can annotate `unwrap_or_else` `pure noalloc` without restricting its callers,
//     and one passing an impure callback is still caught, at the call site where the
//     impurity actually is.
//   - A callback *passed onward* stays polymorphic: `(f) => unwrap_or_else(m, f)` is
//     polymorphic in `f` too, rather than being charged AllEffects for handing it over.
//     Without that, combinators built out of combinators would be exactly as poisoned as
//     before.
//
// What stays conservative (sound, imprecise): a callback reached through anything other than
// a parameter or a resolvable binding — a struct field, a call result, an array element —
// and multi-clause lambdas, whose parameters have no single index to match arguments
// against. Trait-impl methods are not polymorphic yet either; methodEffects is unchanged.

// callEffect is the effect of *this call site*: the callee's own base effect, plus the
// effect of each argument supplied for a callback parameter.
//
// enclosing/enclosingCallbacks let a callback that is passed straight through — the
// argument is one of the *caller's* own callback parameters — propagate rather than taint:
// it is recorded as a callback of the enclosing function, to be charged one level up.
func callEffect(
	callee *ast.LambdaExpr,
	call *ast.FunctionCallExpr,
	capture []scopeBindings,
	effects map[*ast.LambdaExpr]Effect,
	callbacks map[*ast.LambdaExpr]map[string]int,
	enclosing map[string]int,
	enclosingCallbacks map[string]int,
) Effect {
	found := effects[callee]
	for _, idx := range callbacks[callee] {
		if idx >= len(call.Arguments) {
			// Arity mismatch — the typechecker reports it; assume the worst here rather
			// than reading past the end.
			found |= AllEffects
			continue
		}
		found |= argumentEffect(call.Arguments[idx], capture, effects, callbacks, enclosing, enclosingCallbacks)
	}
	return found
}

// methodCallEffect is callEffect for a *trait-method* call site: the method's own base
// effect plus the effects of the arguments supplied for its callback parameters.
//
// Separate from callEffect only because of the receiver offset (methodArgumentAt) and
// because a method's callbacks are keyed by *ast.TraitMethodImpl rather than by lambda.
func methodCallEffect(
	method *ast.TraitMethodImpl,
	call *ast.FunctionCallExpr,
	capture []scopeBindings,
	effects map[*ast.LambdaExpr]Effect,
	methodEffectsByImpl map[*ast.TraitMethodImpl]Effect,
	callbacks map[*ast.LambdaExpr]map[string]int,
	methodCallbacks map[*ast.TraitMethodImpl]map[string]int,
	enclosing map[string]int,
	enclosingCallbacks map[string]int,
) Effect {
	found := methodEffectsByImpl[method]
	for _, idx := range methodCallbacks[method] {
		arg, ok := methodArgumentAt(call, idx)
		if !ok {
			found |= AllEffects
			continue
		}
		found |= argumentEffect(arg, capture, effects, callbacks, enclosing, enclosingCallbacks)
	}
	return found
}

// argumentEffect is the effect of a value supplied for a callback parameter.
func argumentEffect(
	arg ast.Expression,
	capture []scopeBindings,
	effects map[*ast.LambdaExpr]Effect,
	callbacks map[*ast.LambdaExpr]map[string]int,
	enclosing map[string]int,
	enclosingCallbacks map[string]int,
) Effect {
	switch a := arg.(type) {
	case *ast.LambdaExpr:
		// An inline lambda literal: its own inferred base. If it is *itself* polymorphic,
		// nothing here supplies its callbacks, so assume the worst for those.
		return effects[a] | unknownCallbackEffect(callbacks[a])
	case *ast.IdentifierExpr:
		// The caller's own callback, handed straight on: stay polymorphic and charge it
		// one level up instead of tainting here.
		if idx, ok := enclosing[a.Name]; ok {
			if enclosingCallbacks != nil {
				enclosingCallbacks[a.Name] = idx
			}
			return EffectNone
		}
		if lam, ok := resolveFunction(capture, a.Name); ok {
			return effects[lam] | unknownCallbackEffect(callbacks[lam])
		}
	}
	// A field, a call result, an element — nothing this pass can see through.
	return AllEffects
}

// unknownCallbackEffect is AllEffects when a function being *passed as a value* has
// callback parameters of its own, since this site supplies none of them.
func unknownCallbackEffect(callbacks map[string]int) Effect {
	if len(callbacks) > 0 {
		return AllEffects
	}
	return EffectNone
}

func lambdaEffects(lam *ast.LambdaExpr, defCapture []scopeBindings, impureLambdas map[*ast.LambdaExpr]Effect, impureMethods map[*ast.TraitMethodImpl]Effect, methodTable *typetable.MethodTable, boundGroups map[typetable.BoundMethodRef][]*ast.TraitMethodImpl, alloc *allocContext, frames *scopeFrames, callbacks map[*ast.LambdaExpr]map[string]int) (Effect, map[string]int) {
	if lam.IsExtern {
		// No body to walk, and no reason to walk one: what a foreign function does is
		// what its declaration claims. See externEffects.
		return externEffects(lam), nil
	}
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
	params := callableParams(lam)
	// foundCallbacks are the effect-polymorphic parameters discovered on this pass: the
	// ones this body calls, or hands on to another function's callback slot.
	foundCallbacks := make(map[string]int)
	var found Effect
	// noteAlloc sets the bit *and* remembers where it came from, so lyra-E016 can point at
	// the allocation instead of listing every form the language can allocate with.
	noteAlloc := func(ex ast.Expression) {
		found |= EffectAlloc
		alloc.noteLambda(lam, ex)
	}
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
		// An overloaded operator is a call to a trait impl method; charge it as one.
		// Before the switch, since the operator nodes appear in it for other reasons.
		if eff, _ := operatorImplEffect(e, methodTable, impureMethods, boundGroups); eff != EffectNone {
			found |= eff
		}
		switch ex := e.(type) {
		case *ast.LambdaExpr:
			// A nested lambda is a separate boundary — its body's effects are its
			// own, charged where it is called — but its *construction* happens here,
			// and a capturing one heap-boxes its environment (closureAllocates has
			// the reasoning; a capture-free one is a shared pinned static and stays
			// free). This was the audit's `noalloc` hole: with no charge here, a
			// `noalloc` function building a capturing closure checked clean while
			// calling `lyra_rc_alloc` on every invocation.
			if alloc.closureAllocates(ex) {
				noteAlloc(ex)
			}
			return false
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
			if methodTable.IsBuiltinMethod(ex) {
				// A compiler builtin method (`x.wrapping_mul(y)`, `x.floor()`,
				// `xs.len()`): pure arithmetic, no effect. Checked before the
				// name-based ladder below, which would otherwise see the dotted name
				// `x.wrapping_mul`, resolve it to nothing, and charge AllEffects —
				// making explicit wrapping arithmetic unusable from exactly the
				// `pure`/`det`/`noalloc` code that wants it.
				//
				// "No effect" is not quite "nothing": `s.slice(…)` builds a fresh
				// string, so it carries EffectAlloc — pure, and refused by `noalloc`.
				// The typechecker records which, since only it saw the receiver's type.
				if methodTable.BuiltinMethodAllocates(ex) {
					found |= EffectAlloc
				}
			} else if method, ok := methodTable.Get(ex); ok {
				found |= impureMethods[method]
			} else if ref, ok := methodTable.GetBound(ex); ok {
				// Abstract dispatch through a `where` bound: join over the impls of
				// the bound trait method (pure only if all of them are).
				found |= boundCallEffect(ref, boundGroups, impureMethods)
			} else if name := calleeName(ex.Function); name != "" {
				// Resolution order: a real binding, then our own parameters, then
				// builtins — the typechecker's order, and it is also what makes a
				// body-declared `let f = …` shadowing a parameter named `f` resolve to
				// the declaration (both live in this lambda's frame, and resolveFunction
				// consults .functions before .mutable).
				if target, ok := calleeFor(alloc.table(), ex, bodyCapture, name); ok {
					// The callee's *base* effect plus whatever this site supplies for
					// its callback parameters.
					found |= callEffect(target, ex, bodyCapture, impureLambdas, callbacks, params, foundCallbacks)
				} else if idx, isParam := params[name]; isParam {
					if bound := declaredBound(lam, idx); bound != nil {
						// The parameter's *type* constrains it (`f: pure () -> t`), so
						// what calling it can do is known from the signature alone: charge
						// exactly what the bound still permits. This function is therefore
						// **not** polymorphic in it — it is pure (or det, or noalloc) for
						// every caller, which is the whole point of writing the bound.
						found |= boundEffect(bound)
					} else {
						// Unconstrained: this function is effect-polymorphic in the
						// parameter. The effect belongs to whoever supplies the callback
						// and is charged at that call site.
						foundCallbacks[name] = idx
					}
				} else if e, ok := builtinEffects[name]; ok {
					found |= e
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
				noteAlloc(ex)
			}
		case *ast.TupleLiteralExpr:
			// A named tuple literal (`Foo(1, 2)`) or a data construction
			// (`Branch(5)`) — allocates when its recorded flavor is `shared`.
			if alloc.allocates(ex) {
				noteAlloc(ex)
			}
		case *ast.DataConstructorExpr:
			// A nullary constructor (`Leaf`) — allocates when used as `shared`.
			if alloc.allocates(ex) {
				noteAlloc(ex)
			}
		case *ast.ArrayLiteralExpr, *ast.ArrayRepeatExpr:
			// `[1, 2, 3]` as a `[]T` allocates its box; the same literal as a fixed
			// `[3]T` is stack storage and does not. `allocates` reads the type the
			// typechecker recorded, so the two are told apart by what the literal was
			// used as rather than by how it was written.
			//
			// `[0; 3]` is the same expression with a count instead of a list, and
			// belongs in the same arm for the same reason. It was left out when the
			// repeat form landed (08/08) and the gap was live for exactly one build:
			// `noalloc … => { let d: []i64 = [0; 3]; … }` type-checked clean while the
			// identical `[1, 2, 3]` was refused — hazard 8, in the arm that names the
			// forms rather than in a switch over types.
			if alloc.allocates(ex) {
				noteAlloc(ex)
			}
		case *ast.ArrayCompExpr:
			// A comprehension always builds a `[]T` box — its length is a runtime
			// question, so there is no fixed-size form of it to be the cheap case.
			if alloc.allocates(ex) {
				noteAlloc(ex)
			}
		case *ast.StringConcatExpr, *ast.InterpolatedStringExpr:
			// `a ++ b` and `"${x}"` each build a fresh ref-counted box. A string
			// *literal* does not — it interns as a pinned static box — which is why
			// these are charged by form rather than by type: all three are `string`.
			if alloc.allocatesByForm(ex) {
				noteAlloc(ex)
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
	return found, foundCallbacks
}

// methodEffects is lambdaEffects's counterpart for a trait-impl method: same
// effect checks as lambdaEffects, but over a bare LambdaClause (a method is
// always exactly one clause, never a multi-clause or bare-body lambda) and
// with no mut-borrow parameter set to consult (methods have no mut/own/ref
// parameter-modifier syntax yet, unlike a lambda's — see
// directScopeBindingsForClause). base is the program's top-level capture
// frame; a method's own scope never nests inside another lambda's, so that
// is the entire capture stack besides the method's own.
// It returns the method's *base* effect plus the callback parameters it calls, exactly as
// lambdaEffects does for a free function — a trait method taking a callback used to be as
// poisoned as every function was before effect polymorphism landed.
//
// signature is what the method's trait declares for it (collectMethodSignatures); it is the
// only place a method's parameter *types* live, so it is what declared bounds
// (`f: pure () -> t` in a trait signature) are read from. A nil signature falls back to
// treating every called parameter as unconstrained, which is the conservative answer.
func methodEffects(m *ast.TraitMethodImpl, base []scopeBindings, impureLambdas map[*ast.LambdaExpr]Effect, impureMethods map[*ast.TraitMethodImpl]Effect, methodTable *typetable.MethodTable, boundGroups map[typetable.BoundMethodRef][]*ast.TraitMethodImpl, alloc *allocContext, signature *types.LambdaType, callbacks map[*ast.LambdaExpr]map[string]int, methodCallbacks map[*ast.TraitMethodImpl]map[string]int) (Effect, map[string]int) {
	scope := directScopeBindingsForClause(&m.Clause)
	locals := make(map[string]bool, len(scope.mutable))
	for name := range scope.mutable {
		locals[name] = true
	}
	bodyCapture := pushScope(base, scope)
	params := clauseParams(m)
	foundCallbacks := make(map[string]int)
	var found Effect
	noteAlloc := func(ex ast.Expression) {
		found |= EffectAlloc
		alloc.noteMethod(m, ex)
	}
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
		// An overloaded operator is a call to a trait impl method; charge it as one.
		// Before the switch, since the operator nodes appear in it for other reasons.
		if eff, _ := operatorImplEffect(e, methodTable, impureMethods, boundGroups); eff != EffectNone {
			found |= eff
		}
		switch ex := e.(type) {
		case *ast.LambdaExpr:
			// A separate boundary, but a capturing construction heap-boxes its
			// environment here — the same charge as lambdaEffects's copy of this
			// arm, for the reason the builtin-method case below spells out about
			// the two ladders staying in step.
			if alloc.closureAllocates(ex) {
				noteAlloc(ex)
			}
			return false
		case *ast.IdentifierExpr:
			if !locals[ex.Name] && capturedMutable(bodyCapture, ex.Name) {
				found |= EffectMut
			}
		case *ast.MathAssignOpExpr:
			if !locals[ex.Left.Name] {
				found |= EffectMut
			}
		case *ast.FunctionCallExpr:
			if methodTable.IsBuiltinMethod(ex) {
				// A compiler builtin method — pure, and checked first, for the reason
				// spelled out in lambdaEffects's copy of this ladder. The two must stay
				// in step: this one is the same question asked from inside a trait-impl
				// method body, so a divergence would make `x.wrapping_mul(y)` pure in a
				// free function and all-effects in a method — or, with the allocating
				// case below, `noalloc` refusing `s.slice(…)` in a function and
				// permitting it in a method.
				if methodTable.BuiltinMethodAllocates(ex) {
					found |= EffectAlloc
				}
			} else if method, ok := methodTable.Get(ex); ok {
				found |= methodCallEffect(method, ex, bodyCapture, impureLambdas, impureMethods,
					callbacks, methodCallbacks, params, foundCallbacks)
			} else if ref, ok := methodTable.GetBound(ex); ok {
				// Abstract dispatch through a `where` bound: join over the impls of
				// the bound trait method (pure only if all of them are).
				found |= boundCallEffect(ref, boundGroups, impureMethods)
			} else if name := calleeName(ex.Function); name != "" {
				// Scope before builtins, matching isImpureCallee and the typechecker:
				// a user binding shadows a builtin of the same name, so its own body
				// decides its effects.
				if target, ok := calleeFor(alloc.table(), ex, bodyCapture, name); ok {
					found |= callEffect(target, ex, bodyCapture, impureLambdas, callbacks, params, foundCallbacks)
				} else if idx, isParam := params[name]; isParam {
					// A call through one of this method's own parameters. Its trait
					// signature may constrain it (`f: pure () -> t`), in which case the
					// cost is known and the method is not polymorphic in it.
					if bound := signatureBound(signature, idx); bound != nil {
						found |= boundEffect(bound)
					} else {
						foundCallbacks[name] = idx
					}
				} else if e, ok := builtinEffects[name]; ok {
					found |= e
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
				noteAlloc(ex)
			}
		case *ast.TupleLiteralExpr:
			if alloc.allocates(ex) {
				noteAlloc(ex)
			}
		case *ast.DataConstructorExpr:
			if alloc.allocates(ex) {
				noteAlloc(ex)
			}
		case *ast.ArrayLiteralExpr, *ast.ArrayRepeatExpr:
			if alloc.allocates(ex) {
				noteAlloc(ex)
			}
		case *ast.ArrayCompExpr:
			if alloc.allocates(ex) {
				noteAlloc(ex)
			}
		case *ast.StringConcatExpr, *ast.InterpolatedStringExpr:
			if alloc.allocatesByForm(ex) {
				noteAlloc(ex)
			}
		case *ast.AwaitExpr:
			found |= EffectInput
		}
		return true
	}
	ast.WalkExpr(m.Clause.Body, onStmt, onExpr)
	return found, foundCallbacks
}

// isTypeConversionCall reports whether name is a type name used as a
// type-conversion call (e.g. `i32(x)`, `string(e)`). These are always pure — they
// don't allocate (the identity forms return their operand's own box), observe
// external state, or mutate anything — so they must not be treated as impure even
// though they have no lambda binding.
//
// Delegated to the shared answer (08/12) rather than keeping a list here, because
// the list had already drifted: it was missing `rune`, so `rune(n)` — the explicit
// spelling for building a code point, in exactly the classification arithmetic
// `pure` code writes — was charged the unresolved-callee default and reported as
// impure. The hazard-8 shape, in the fourth copy of one question.
func isTypeConversionCall(name string) bool {
	_, ok := types.ConversionTargetName(name)
	return ok
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

// operatorImplEffect is the effect of an **overloaded operator** — `a + b` on a type
// with a `(_+_)` impl is a call to that impl's method, and costs whatever the method
// costs.
//
// It exists as one function called from four places rather than four arms, because the
// four places are the same question asked four times (hazard 8's third instance, and
// its fifth): the two effect-inference walks, the reporting walk, and the trait-method
// walk all ask "what does this expression call?", and an operator answers where a
// FunctionCallExpr would.
//
// **It closes the hole `Eq`/`Ord` opened first.** A comparison operator has dispatched
// to an impl since 08/07, and none of the ladders looked, so `let f = pure (a: T, b: T)
// -> bool => a < b` type-checked with an `Ord::compare` that printed. Arithmetic would
// have added a second instance of that; this fixes both, which is why it keys on the
// resolution rather than on the operator.
func operatorImplEffect(e ast.Expression, methodTable *typetable.MethodTable, impureMethods map[*ast.TraitMethodImpl]Effect, boundGroups map[typetable.BoundMethodRef][]*ast.TraitMethodImpl) (Effect, string) {
	switch e.(type) {
	case *ast.MathBinaryOpExpr, *ast.NegationExpr, *ast.BitwiseNotExpr,
		*ast.BooleanBinaryOpExpr, *ast.MathAssignOpExpr:
		// MathAssignOpExpr belongs here for the same reason it reuses the binary
		// operator's type rules: `x += y` is `x = x + y`, so it calls the same impl.
	default:
		return EffectNone, ""
	}
	if res, ok := methodTable.OperatorResolution(e); ok && res.Method != nil {
		return impureMethods[res.Method], res.Method.Name.GetName()
	}
	// An operator resolved through a `where` bound names no single impl, so the effect
	// is the join over every impl of that trait method — the same rule a bound *call*
	// follows, and the same reason: the bound admits any of them, so a `pure` caller is
	// only safe if all of them are.
	if ref, ok := methodTable.OperatorBound(e); ok {
		return boundCallEffect(ref, boundGroups, impureMethods), ref.Method
	}
	return EffectNone, ""
}
