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
// This hard-coded set is a stop-gap. The real solution (todo: purity inference,
// item #3) is to track a purity/effect bit per function in the symbol table and
// consult it here, so user-defined impure functions are caught too — not just
// these builtins.
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
// Calls to *user-defined* impure functions are caught for top-level function
// bindings via inferImpureFunctions (transitive, fixpoint). Not yet handled
// (needs symbol-table / scope info; see todo items #3/#4):
//   - reading captured *mutable* (`var`) state — also breaks referential
//     transparency, but distinguishing captured `var` from `let` needs scope info
//   - impurity of non-top-level functions, methods, and imported functions
func CheckPurity(program *ast.Program) []PurityError {
	c := &purityChecker{impureFns: inferImpureFunctions(program)}
	for _, node := range program.Statements {
		if stmt, ok := node.(ast.Statement); ok {
			// Top level is an impure context: a nil scope means "not inside a pure
			// function, don't check".
			ast.WalkStmt(stmt, c.stmtVisitor(nil), c.exprVisitor(nil))
		}
	}
	return c.errors
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
	// impureFns holds the names of top-level functions inferred to be impure, so
	// a `pure` function calling one can be reported even though the callee isn't a
	// builtin. Populated by inferImpureFunctions (a first slice of purity
	// inference, todo #3).
	impureFns map[string]bool
}

// isImpureCallee reports whether a call target with the given dotted name is
// known to be impure — either a hard-coded effectful builtin or a top-level
// function inferred impure.
func (c *purityChecker) isImpureCallee(name string) bool {
	return knownImpureBuiltins[name] || c.impureFns[name]
}

// stmtVisitor returns a statement callback. sc is non-nil exactly when we are
// inside a `pure` function body. A mutation of a name not owned locally is a
// mutation of captured outer state; interior mutation through a `mut`-borrowed
// parameter escapes to the caller's value.
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

func (c *purityChecker) exprVisitor(sc *funcScope) func(ast.Expression) bool {
	return func(expr ast.Expression) bool {
		switch e := expr.(type) {
		case *ast.LambdaExpr:
			// Default values execute at the call site, in the *enclosing* context.
			for i := range e.Parameters {
				ast.WalkExpr(e.Parameters[i].DefaultValue, c.stmtVisitor(sc), c.exprVisitor(sc))
			}
			// The body runs in this lambda's own context: pure → build its scope;
			// impure → nil (no checking).
			var child *funcScope
			if e.IsPure {
				child = &funcScope{locals: localBindings(e), mutBorrows: mutBorrowParams(e)}
			}
			ast.WalkExpr(e.Body, c.stmtVisitor(child), c.exprVisitor(child))
			for _, clause := range e.LambdaClauses {
				ast.WalkExpr(clause.Body, c.stmtVisitor(child), c.exprVisitor(child))
			}
			return false // recursed manually

		case *ast.MathAssignOpExpr:
			if sc != nil && !sc.isLocal(e.Left.Name) {
				c.report(e.GetLocation(),
					"pure function mutates captured binding %q; mutation must not escape the function", e.Left.Name)
			}

		case *ast.FunctionCallExpr:
			if sc != nil {
				if name := calleeName(e.Function); name != "" && c.isImpureCallee(name) {
					c.report(e.GetLocation(),
						"pure function calls impure function %q", name)
				}
			}
		}
		return true
	}
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
	locals := map[string]bool{}
	add := func(names []string) {
		for _, n := range names {
			locals[n] = true
		}
	}
	for i := range lambda.Parameters {
		add(patternBoundNames(lambda.Parameters[i].Pattern))
	}
	collectBody := func(body ast.Expression) {
		ast.WalkExpr(body,
			func(s ast.Statement) bool {
				add(directDeclaredNames(s))
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
					add(directDeclaredNames(n.Init))
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
			add(patternBoundNames(pat))
		}
		collectBody(clause.Body)
	}
	return locals
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

// rootIdentName walks an interior-mutation target (a member/index chain like
// `grid[i].y`) back to the identifier it is rooted at, returning its name. It
// returns "" when the target is not rooted at a plain identifier (e.g. a
// function-call result), in which case the write cannot be attributed to a
// binding.
func rootIdentName(expr ast.Expression) string {
	for {
		switch e := expr.(type) {
		case *ast.IdentifierExpr:
			return e.Name
		case *ast.MemberExpr:
			expr = e.Object
		case *ast.IndexExpr:
			expr = e.Object
		default:
			return ""
		}
	}
}

// inferImpureFunctions returns the set of top-level function bindings whose
// bodies have an observable effect — directly (captured mutation, pointer write,
// call to an impure builtin) or transitively (a call to another impure
// function). It iterates to a fixpoint so impurity propagates through call
// chains regardless of declaration order.
//
// This is intentionally limited to top-level `let`/`var name = <lambda>`
// bindings; it is the minimal inference needed to flag a `pure` function that
// calls a user-defined impure function (todo #3 builds this out with proper
// scope/symbol-table backing).
func inferImpureFunctions(program *ast.Program) map[string]bool {
	fns := topLevelFunctions(program)
	impure := map[string]bool{}
	for {
		changed := false
		for name, lam := range fns {
			if impure[name] {
				continue
			}
			if lambdaHasObservableEffect(lam, impure) {
				impure[name] = true
				changed = true
			}
		}
		if !changed {
			break
		}
	}
	return impure
}

// topLevelFunctions maps each top-level `let`/`var name = <lambda>` binding to
// its lambda.
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

// lambdaHasObservableEffect reports whether lam's body performs any effect
// observable outside the call, given the set of names already known to be impure
// functions. It mirrors the checks in the main pass but accumulates a bool
// instead of emitting diagnostics, and is used only for inference. It does not
// descend into nested lambdas (they have their own boundary).
func lambdaHasObservableEffect(lam *ast.LambdaExpr, impureFns map[string]bool) bool {
	locals := localBindings(lam)
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
		case *ast.MathAssignOpExpr:
			if !locals[ex.Left.Name] {
				found = true
			}
		case *ast.FunctionCallExpr:
			if name := calleeName(ex.Function); name != "" && (knownImpureBuiltins[name] || impureFns[name]) {
				found = true
			}
		}
		return true
	}
	ast.WalkExpr(lam.Body, onStmt, onExpr)
	for _, clause := range lam.LambdaClauses {
		ast.WalkExpr(clause.Body, onStmt, onExpr)
	}
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
