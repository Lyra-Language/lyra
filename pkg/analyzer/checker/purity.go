package checker

import (
	"fmt"

	"github.com/Lyra-Language/lyra/pkg/ast"
	diag "github.com/Lyra-Language/lyra/pkg/diagnostic"
	"github.com/Lyra-Language/lyra/pkg/types"
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

// knownImpureBuiltins lists builtins that perform observable effects (I/O, etc.)
// and therefore may never be called from a `pure` function.
//
// This hard-coded set is a stop-gap for *builtins* specifically — user-defined
// functions (at any nesting depth) are covered by inferImpureLambdas instead;
// see todo: purity inference, item #3.
var knownImpureBuiltins = map[string]bool{
	"print":       true,
	"println":     true,
	"fmt.print":   true,
	"fmt.println": true,
	"read":        true,
	"write":       true,
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
// just the top level. Reading captured *mutable* state (`var` / `let mut`,
// whose value can change between calls) is also reported — it breaks
// referential transparency even though no effect escapes — for state captured
// from any enclosing scope, again not just the top level. Both resolve names
// via the capture stack of scopeBindings frames built as the walk descends
// through lambda boundaries. Not yet handled (needs symbol-table backing; see
// todo items #3/#4):
//   - impurity of methods and imported functions
func CheckPurity(program *ast.Program) []PurityError {
	base := []scopeBindings{{mutable: mutableGlobals(program), functions: topLevelFunctions(program)}}
	c := &purityChecker{
		impureLambdas: inferImpureLambdas(collectFuncBindings(program, base)),
		assignTargets: map[*ast.IdentifierExpr]bool{},
	}
	for _, node := range program.Statements {
		if stmt, ok := node.(ast.Statement); ok {
			// Top level is an impure context: a nil scope means "not inside a pure
			// function, don't check".
			ast.WalkStmt(stmt, c.stmtVisitor(nil, base), c.exprVisitor(nil, base))
		}
	}
	return c.errors
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
	// impureLambdas holds the function literals inferred to be impure, keyed by
	// pointer (not name) so that two unrelated functions in different scopes
	// that happen to share a name are never confused with each other. Populated
	// by inferImpureLambdas (purity inference, todo #3).
	impureLambdas map[*ast.LambdaExpr]bool
	// assignTargets records IdentifierExpr nodes that are the root of an assignment
	// target (the LHS of `x += …`, or the base of `x.f = …`). The write is reported
	// by the mutation checks, so the same node must not be re-reported as a read.
	assignTargets map[*ast.IdentifierExpr]bool
}

// stmtVisitor returns a statement callback. sc is non-nil exactly when we are
// inside a `pure` function body. capture is unused here but threaded through
// so the signature matches exprVisitor's manual-recursion call sites. A
// mutation of a name not owned locally is a mutation of captured outer state;
// interior mutation through a `mut`-borrowed parameter escapes to the caller's
// value.
func (c *purityChecker) stmtVisitor(sc *funcScope, capture []scopeBindings) func(ast.Statement) bool {
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
			// Default values execute at the call site, in the *enclosing* context.
			for i := range e.Parameters {
				ast.WalkExpr(e.Parameters[i].DefaultValue, c.stmtVisitor(sc, capture), c.exprVisitor(sc, capture))
			}
			// The body runs in this lambda's own context: pure → build its scope;
			// impure → nil (no checking). Either way it introduces a new lexical
			// scope, so push its bindings frame onto the capture stack regardless
			// of purity — a pure lambda nested deeper still needs to resolve names
			// captured through this (possibly impure) intermediate scope.
			var child *funcScope
			scope := directScopeBindings(e)
			if e.IsPure {
				locals := make(map[string]bool, len(scope.mutable))
				for name := range scope.mutable {
					locals[name] = true
				}
				child = &funcScope{locals: locals, mutBorrows: mutBorrowParams(e)}
			}
			childCapture := pushScope(capture, scope)
			walkLambdaBodies(e, c.stmtVisitor(child, childCapture), c.exprVisitor(child, childCapture))
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
				if name := calleeName(e.Function); name != "" && c.isImpureCallee(capture, name) {
					c.report(e.GetLocation(),
						"pure function calls impure function %q", name)
				}
			}
		}
		return true
	}
}

// isImpureCallee reports whether a call target with the given dotted name is
// known to be impure — either a hard-coded effectful builtin, or a
// user-defined function that resolves (via the capture stack) to a lambda
// inferImpureLambdas has flagged.
func (c *purityChecker) isImpureCallee(capture []scopeBindings, name string) bool {
	if knownImpureBuiltins[name] {
		return true
	}
	lam, ok := resolveFunction(capture, name)
	return ok && c.impureLambdas[lam]
}

func (c *purityChecker) report(loc ast.Location, format string, args ...any) {
	c.errors = append(c.errors, PurityError{
		Code:     diag.CodePurityViolation,
		Message:  fmt.Sprintf(format, args...),
		Location: loc,
	})
}

// localBindings collects every name bound locally within a pure lambda: its
// parameter patterns, its clause patterns, and any `let`/`var`/destructuring
// declaration in its body. It deliberately does NOT descend into nested lambdas —
// those own their own scope, and their bindings are not local to this function.
func localBindings(lambda *ast.LambdaExpr) map[string]bool {
	scope := directScopeBindings(lambda)
	locals := make(map[string]bool, len(scope.mutable))
	for name := range scope.mutable {
		locals[name] = true
	}
	return locals
}

// directScopeBindings scans every name declared directly within lambda's own
// scope — parameter patterns, clause patterns, and any `let`/`var`/destructuring
// declaration in its body (not descending into nested lambdas) — producing one
// full capture-stack frame: which of those names are interior-mutable bindings
// (`var`/`let mut`), and which are bound to a function value declared directly
// here. This is the per-lambda analogue of mutableGlobals/topLevelFunctions's
// top-level scan, used to resolve a name a *nested* function reads or calls but
// does not own, against the scope it is actually captured from — recording
// immutable/non-function names too (rather than just omitting them) so a
// closer declaration correctly shadows a farther one of either kind.
func directScopeBindings(lambda *ast.LambdaExpr) scopeBindings {
	scope := scopeBindings{mutable: map[string]bool{}, functions: map[string]*ast.LambdaExpr{}}
	addImmutable := func(names []string) {
		for _, n := range names {
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
	for i := range lambda.Parameters {
		addImmutable(patternBoundNames(lambda.Parameters[i].Pattern))
	}
	collectBody := func(body ast.Expression) {
		ast.WalkExpr(body,
			func(s ast.Statement) bool {
				mergeStmt(s)
				return true
			},
			func(e ast.Expression) bool {
				switch n := e.(type) {
				case *ast.LambdaExpr:
					// Stop at nested lambdas: their inner bindings are not ours.
					return false
				case *ast.ForLoopExpr:
					// The walker exposes the C-style loop's init as a special
					// field (ForLoopExpr.Init), not as a visited statement, so
					// collect the loop variable explicitly.
					mergeStmt(n.Init)
				}
				// NOTE: other binding-introducing special fields not surfaced as
				// statements by the walker (if-let / else destructuring patterns,
				// `with`-arena bindings) should be collected here too once those
				// forms can appear in pure code.
				return true
			},
		)
	}
	collectBody(lambda.Body)
	for _, clause := range lambda.LambdaClauses {
		for _, pat := range clause.Patterns {
			addImmutable(patternBoundNames(pat))
		}
		collectBody(clause.Body)
	}
	return scope
}

// declaredMutability returns the name(s) bound directly by stmt — a
// `let`/`var`/const declaration or a destructuring decl — mapped to whether
// each is an interior-mutable binding (`var`, or `let mut`/destructuring with
// the `mut` modifier). A name bound to a function value is always reported as
// immutable here: call purity for it is handled separately (via
// scopeBindings.functions / inferImpureLambdas), and treating a function name
// as mutable data would be confusing. Returns nil for any other statement kind.
func declaredMutability(stmt ast.Statement) map[string]bool {
	switch s := stmt.(type) {
	case *ast.VarDeclStmt:
		if _, isLambda := s.Value.(*ast.LambdaExpr); isLambda {
			return map[string]bool{s.Name: false}
		}
		return map[string]bool{s.Name: s.CanMutateInterior()}
	case *ast.DestructuringDeclStmt:
		mutable := s.Keyword == "var" || s.IsMut
		out := map[string]bool{}
		for _, n := range patternBoundNames(s.Pattern) {
			out[n] = mutable
		}
		return out
	}
	return nil
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
func collectFuncBindings(program *ast.Program, base []scopeBindings) map[*ast.LambdaExpr][]scopeBindings {
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
		childCapture := pushScope(capture, directScopeBindings(lam))
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

// inferImpureLambdas returns the set of function literals (from defs, which
// pairs each with the capture stack visible at its definition site) whose
// bodies have an observable effect — directly (captured mutation, pointer
// write, call to an impure builtin) or transitively (a call to another impure
// function). It iterates to a fixpoint so impurity propagates through call
// chains regardless of declaration order, for functions at any nesting depth.
func inferImpureLambdas(defs map[*ast.LambdaExpr][]scopeBindings) map[*ast.LambdaExpr]bool {
	impure := map[*ast.LambdaExpr]bool{}
	for {
		changed := false
		for lam, capture := range defs {
			if impure[lam] {
				continue
			}
			if lambdaHasObservableEffect(lam, capture, impure) {
				impure[lam] = true
				changed = true
			}
		}
		if !changed {
			break
		}
	}
	return impure
}

// lambdaHasObservableEffect reports whether lam's body performs any effect
// observable outside the call, given defCapture (the capture stack at lam's
// definition site) and the set of lambdas already known to be impure. It
// mirrors the checks in the main pass but accumulates a bool instead of
// emitting diagnostics, and is used only for inference. It does not descend
// into nested lambdas (they have their own boundary).
func lambdaHasObservableEffect(lam *ast.LambdaExpr, defCapture []scopeBindings, impureLambdas map[*ast.LambdaExpr]bool) bool {
	scope := directScopeBindings(lam)
	locals := make(map[string]bool, len(scope.mutable))
	for name := range scope.mutable {
		locals[name] = true
	}
	// Calls/reads inside lam's own body must resolve against a stack that
	// includes lam's own frame too (e.g. a call to a sibling helper declared
	// alongside lam in the same body), not just the scope it was defined in.
	bodyCapture := pushScope(defCapture, scope)
	mutBorrows := mutBorrowParams(lam)
	found := false
	onStmt := func(s ast.Statement) bool {
		switch st := s.(type) {
		case *ast.VarReassignmentStmt:
			if !locals[st.Name] {
				found = true
			}
		case *ast.LValueAssignmentStmt:
			if root := rootIdentName(st.Target); root != "" && (mutBorrows[root] || !locals[root]) {
				found = true
			}
		case *ast.DerefAssignmentStmt:
			found = true
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
			// already counted by the mutation cases above, so double-counting a
			// bool here is harmless.)
			if !locals[ex.Name] && capturedMutable(bodyCapture, ex.Name) {
				found = true
			}
		case *ast.MathAssignOpExpr:
			if !locals[ex.Left.Name] {
				found = true
			}
		case *ast.FunctionCallExpr:
			if name := calleeName(ex.Function); name != "" {
				if knownImpureBuiltins[name] {
					found = true
				} else if target, ok := resolveFunction(bodyCapture, name); ok && impureLambdas[target] {
					found = true
				}
			}
		}
		return true
	}
	walkLambdaBodies(lam, onStmt, onExpr)
	return found
}

// calleeName renders a call target as a dotted name ("foo", "fmt.println") for
// lookup against knownImpureBuiltins. Returns "" for callees that aren't a plain
// identifier or member chain (e.g. an immediately-invoked lambda).
func calleeName(fn ast.Expression) string {
	switch f := fn.(type) {
	case *ast.IdentifierExpr:
		return f.Name
	case *ast.MemberExpr:
		if obj := calleeName(f.Object); obj != "" {
			return obj + "." + f.Property.Name
		}
		return f.Property.Name
	}
	return ""
}
