package checker

import (
	"github.com/Lyra-Language/lyra/pkg/analyzer/captures"
	"github.com/Lyra-Language/lyra/pkg/analyzer/ownership"
	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/types"
)

// globalWrites is, per function body, the mutable globals that body or anything it may run
// can write: reassign, assign into, grow through a mutating builtin, lend `mut`, or take the
// `&mut` of. It answers lyra-E098 (globalBorrows.writtenGlobal): `var zs = g; via(zs[0])`,
// where `via` pushes onto `g` — the two names are one array, and the push frees the buffer
// `zs[0]` points into. No walk of the caller sees that, so the callee says what it writes.
//
// **What a body may run** is every function it names (called or not: a function handed on
// as a value may be called), every lambda written inside it, and every method its calls
// dispatch to (a `where` bound's every candidate). A lambda written at the call, or a named
// function passed there, is added at the call (calleeBodies).
//
// **A call through a function value may run any function that escapes as one** — every
// lambda not a top-level declaration's value, and every function named other than as a
// callee — taking as many parameters as the call passes arguments (indirectCall). Which
// value a field, a local, a global or a collection holds no walk can say, and a function
// can get there only by escaping. The arity is what keeps that from being every function:
// `self.on_click()` can't run a `(s: ref string) -> i64`.
type globalWrites struct {
	c *globalBorrows
	// direct is what each body writes itself; runs is what it may run.
	direct map[ast.Expression]map[*ast.VarDeclStmt]bool
	runs   map[ast.Expression][]ast.Expression
	closed map[ast.Expression]map[*ast.VarDeclStmt]bool
	// declared is every lambda a top-level declaration binds: a function, not a value.
	declared map[*ast.LambdaExpr]bool
	// escaped is every function that escapes as a value, by body, with its arity.
	escaped map[ast.Expression]int
	// indirect is, per body, the arity of each call it makes through a function value.
	indirect map[ast.Expression][]int
	// callees is every call's function position, which names a function without its escaping.
	callees map[ast.Expression]bool
	// bound is, per enclosing function, every name it binds, as globalBorrows keeps it.
	bound []map[string]bool
	// body is the body being walked; nil at the top level.
	body ast.Expression
}

func newGlobalWrites(c *globalBorrows, program *ast.Program) *globalWrites {
	w := &globalWrites{
		c:        c,
		direct:   map[ast.Expression]map[*ast.VarDeclStmt]bool{},
		runs:     map[ast.Expression][]ast.Expression{},
		declared: map[*ast.LambdaExpr]bool{},
		escaped:  map[ast.Expression]int{},
		indirect: map[ast.Expression][]int{},
		callees:  map[ast.Expression]bool{},
	}
	for _, node := range program.Statements {
		if d, ok := node.(*ast.VarDeclStmt); ok {
			if lam, isFunc := d.Value.(*ast.LambdaExpr); isFunc {
				w.declared[lam] = true
			}
		}
	}
	for _, node := range program.Statements {
		switch s := node.(type) {
		case *ast.TraitImplStmt:
			for i := range s.Methods {
				clause := &s.Methods[i].Clause
				w.function(clause.Body, nil, patternNames(clause.Patterns), []ast.Expression{clause.Body})
			}
		case *ast.TraitDeclStmt:
			for i := range s.Methods {
				if d := s.Methods[i].DefaultMethod; d != nil {
					w.function(d.Body, nil, patternNames(d.Patterns), []ast.Expression{d.Body})
				}
			}
		case ast.Statement:
			ast.WalkStmt(s, w.stmt, w.expr)
		}
	}
	w.close()
	return w
}

// lambdaKey is the body a lambda's summary is kept under: its body, or itself for one written
// as clauses.
func lambdaKey(lam *ast.LambdaExpr) ast.Expression {
	if lam.Body != nil {
		return lam.Body
	}
	return lam
}

// function walks one body, with the names it binds in scope. A lambda passes itself, for its
// binders and its parameters; a method's clause passes its patterns' names.
func (w *globalWrites) function(key ast.Expression, lam *ast.LambdaExpr, params map[string]bool, bodies []ast.Expression) {
	binds := map[string]bool{}
	if lam != nil {
		binds = captures.BodyBinders(lam)
		for _, p := range lam.Parameters {
			binds[p.GetName()] = true
		}
	} else {
		for _, b := range bodies {
			for name := range captures.BodyBinders(&ast.LambdaExpr{Body: b}) {
				binds[name] = true
			}
		}
	}
	for name := range params {
		binds[name] = true
	}
	if w.body != nil {
		w.runs[w.body] = append(w.runs[w.body], key)
	}
	outer := w.body
	w.body = key
	w.bound = append(w.bound, binds)
	defer func() {
		w.body = outer
		w.bound = w.bound[:len(w.bound)-1]
	}()
	for _, b := range bodies {
		if b != nil {
			ast.WalkExpr(b, w.stmt, w.expr)
		}
	}
}

func (w *globalWrites) write(place ast.Expression) {
	if w.body == nil {
		return
	}
	if decl, ok := ownership.MutableGlobalRoot(place, w.c.symTable, w.bound); ok {
		w.wrote(decl)
	}
}

func (w *globalWrites) wrote(decl *ast.VarDeclStmt) {
	if w.direct[w.body] == nil {
		w.direct[w.body] = map[*ast.VarDeclStmt]bool{}
	}
	w.direct[w.body][decl] = true
}

func (w *globalWrites) stmt(s ast.Statement) bool {
	switch s := s.(type) {
	case *ast.LValueAssignmentStmt:
		w.write(s.Target)
	case *ast.VarReassignmentStmt:
		if decl, ok := ownership.MutableGlobalNamed(w.c.symTable, w.bound, s.Name, s.GetLocation()); ok && w.body != nil {
			w.wrote(decl)
		}
	}
	return true
}

func (w *globalWrites) expr(e ast.Expression) bool {
	switch e := e.(type) {
	case *ast.LambdaExpr:
		var bodies []ast.Expression
		if e.Body != nil {
			bodies = append(bodies, e.Body)
		}
		for i := range e.LambdaClauses {
			bodies = append(bodies, e.LambdaClauses[i].Body)
		}
		if !w.declared[e] {
			w.escape(e)
		}
		w.function(lambdaKey(e), e, nil, bodies)
		return false
	case *ast.MathAssignOpExpr:
		w.write(e.Left)
	case *ast.AddressOfExpr:
		if e.IsMut {
			w.write(e.Operand)
		}
	case *ast.IdentifierExpr:
		if lam := w.namedFunction(e); lam != nil {
			w.mayRun(lambdaKey(lam))
			if !w.callees[e] {
				w.escape(lam)
			}
		}
	case *ast.MemberExpr:
		if lam := w.c.namespacedFunction(w.bound, e); lam != nil {
			w.mayRun(lambdaKey(lam))
			if !w.callees[e] {
				w.escape(lam)
			}
		}
	case *ast.TryExpr:
		if r, ok := w.c.mt.TryConversion(e); ok && r.Method != nil {
			w.mayRun(r.Method.Clause.Body)
		}
	case *ast.FunctionCallExpr:
		w.call(e)
	}
	// An operator a program's impl answers (`a + b` over its `Add`) runs that impl.
	if r, ok := w.c.mt.OperatorResolution(e); ok && r.Method != nil {
		w.mayRun(r.Method.Clause.Body)
	}
	return true
}

// mayRun records that the body being walked may run key.
func (w *globalWrites) mayRun(key ast.Expression) {
	if w.body != nil {
		w.runs[w.body] = append(w.runs[w.body], key)
	}
}

// call records what a call may run and what it writes by handing a global on: a `mut`
// argument or receiver, and a mutating builtin's receiver.
func (w *globalWrites) call(e *ast.FunctionCallExpr) {
	w.callees[e.Function] = true
	if w.body == nil {
		return
	}
	direct := w.c.calleeBodies(e, false)
	for _, key := range direct {
		w.mayRun(key)
	}
	if w.callsThroughValue(e, direct, w.bound) {
		w.indirect[w.body] = append(w.indirect[w.body], len(e.Arguments))
	}
	if w.c.mt.BuiltinMethodMutates(e) {
		if m, ok := e.Function.(*ast.MemberExpr); ok {
			w.write(m.Object)
		}
	}
	modes, receiver := w.c.parameterModes(e)
	offset := 0
	if receiver != nil {
		offset = 1
		if len(modes) > 0 && modes[0] == types.Mut {
			w.write(receiver)
		}
	}
	for i, arg := range e.Arguments {
		if i+offset < len(modes) && modes[i+offset] == types.Mut {
			w.write(arg)
		}
	}
}

// namedFunction is the top-level function a name means here, unless something binds it.
func (w *globalWrites) namedFunction(id *ast.IdentifierExpr) *ast.LambdaExpr {
	for _, frame := range w.bound {
		if frame[id.Name] {
			return nil
		}
	}
	if w.c.symTable == nil {
		return nil
	}
	fn, _ := w.c.symTable.LookupFunctionFrom(id.Name, id.GetLocation())
	return fn
}

// escape records that lam is a value something may call through.
func (w *globalWrites) escape(lam *ast.LambdaExpr) {
	w.escaped[lambdaKey(lam)] = arity(lam)
}

// arity is how many parameters a lambda takes, written as clauses or not.
func arity(lam *ast.LambdaExpr) int {
	if lam.Body == nil && len(lam.LambdaClauses) > 0 {
		return len(lam.LambdaClauses[0].Patterns)
	}
	return len(lam.Parameters)
}

// indirectCall reports whether e calls through a function value no walk resolves: a local or
// parameter, a global holding one, a field (`self.on_click()`), or what another expression
// evaluates to. A builtin, a constructor and a conversion are none of these; the functions,
// methods and bounds calleeBodies resolves are asked before this is.
func (w *globalWrites) indirectCall(e *ast.FunctionCallExpr, bound []map[string]bool) bool {
	if w.c.mt.IsBuiltinMethod(e) || w.c.symTable == nil {
		return false
	}
	switch f := e.Function.(type) {
	case *ast.IdentifierExpr:
		return boundIn(bound, f.Name) || w.heldGlobal(f, bound)
	case *ast.MemberExpr:
		if ns, isName := f.Object.(*ast.IdentifierExpr); isName && !boundIn(bound, ns.Name) {
			if _, isNS := w.c.symTable.NamespaceIn(w.c.symTable.ModuleOfFile[f.GetLocation().File], ns.Name); isNS {
				return w.heldGlobal(f, bound)
			}
		}
		_, isMethod := w.c.mt.Get(e)
		return !isMethod
	}
	return true
}

// callsThroughValue reports whether e calls a function value: one nothing resolves, or a
// global `var` holding one, which resolves to its first value but may hold any later.
func (w *globalWrites) callsThroughValue(e *ast.FunctionCallExpr, direct []ast.Expression, bound []map[string]bool) bool {
	if len(direct) > 0 && !w.heldGlobal(e.Function, bound) {
		return false
	}
	return w.indirectCall(e, bound)
}

// heldGlobal reports whether f names a top-level binding holding a function value (holdsValue),
// by its own name or through a namespace.
func (w *globalWrites) heldGlobal(f ast.Expression, bound []map[string]bool) bool {
	if w.c.symTable == nil {
		return false
	}
	switch f := f.(type) {
	case *ast.IdentifierExpr:
		if boundIn(bound, f.Name) {
			return false
		}
		decl, ok := w.c.symTable.BindingIn(w.c.symTable.ModuleOfFile[f.GetLocation().File], f.Name)
		return ok && holdsValue(decl)
	case *ast.MemberExpr:
		ns, isName := f.Object.(*ast.IdentifierExpr)
		if !isName || boundIn(bound, ns.Name) {
			return false
		}
		path, isNS := w.c.symTable.NamespaceIn(w.c.symTable.ModuleOfFile[f.GetLocation().File], ns.Name)
		if !isNS {
			return false
		}
		decl, ok := w.c.symTable.BindingIn(path, f.Property.Name)
		return ok && holdsValue(decl)
	}
	return false
}

// holdsValue is a top-level binding whose value is not a function's own: a `var` or `let`
// holding a function value, which may be replaced.
func holdsValue(decl *ast.VarDeclStmt) bool {
	if decl == nil || decl.Value == nil {
		return false
	}
	_, isFunc := decl.Value.(*ast.LambdaExpr)
	return !isFunc || decl.BindingKind == ast.BindingVar
}

func boundIn(bound []map[string]bool, name string) bool {
	for _, frame := range bound {
		if frame[name] {
			return true
		}
	}
	return false
}

// escapedOfArity is every escaped function taking n parameters.
func (w *globalWrites) escapedOfArity(n int) []ast.Expression {
	var out []ast.Expression
	for key, a := range w.escaped {
		if a == n {
			out = append(out, key)
		}
	}
	return out
}

// close takes each body's summary over everything it may run, to a fixed point.
func (w *globalWrites) close() {
	for body, arities := range w.indirect {
		for _, n := range arities {
			w.runs[body] = append(w.runs[body], w.escapedOfArity(n)...)
		}
	}
	w.closed = map[ast.Expression]map[*ast.VarDeclStmt]bool{}
	for key, decls := range w.direct {
		w.closed[key] = map[*ast.VarDeclStmt]bool{}
		for d := range decls {
			w.closed[key][d] = true
		}
	}
	for changed := true; changed; {
		changed = false
		for key, runs := range w.runs {
			for _, r := range runs {
				for d := range w.closed[r] {
					if w.closed[key] == nil {
						w.closed[key] = map[*ast.VarDeclStmt]bool{}
					}
					if !w.closed[key][d] {
						w.closed[key][d] = true
						changed = true
					}
				}
			}
		}
	}
}

// of is what the bodies may write between them.
func (w *globalWrites) of(bodies []ast.Expression) []*ast.VarDeclStmt {
	seen := map[*ast.VarDeclStmt]bool{}
	var out []*ast.VarDeclStmt
	for _, b := range bodies {
		for d := range w.closed[b] {
			if !seen[d] {
				seen[d] = true
				out = append(out, d)
			}
		}
	}
	return out
}

// calleeBodies is the bodies a call runs: its function's, or the method's it dispatches to
// (every candidate of a `where` bound). With handed, also every function value the call is
// handed that the call site shows — a lambda written there, or a function named there.
func (c *globalBorrows) calleeBodies(e *ast.FunctionCallExpr, handed bool) []ast.Expression {
	var out []ast.Expression
	if lam := c.callees.resolveCallee(e); lam != nil {
		out = append(out, lambdaKey(lam))
	} else if m, ok := e.Function.(*ast.MemberExpr); ok && c.namespacedFunction(c.bound, m) != nil {
		out = append(out, lambdaKey(c.namespacedFunction(c.bound, m)))
	} else if r, ok := c.mt.GetResolution(e); ok && r.Method != nil {
		out = append(out, r.Method.Clause.Body)
	}
	for _, r := range c.mt.BoundCandidatesOf(e) {
		if r.Method != nil {
			out = append(out, r.Method.Clause.Body)
		}
	}
	if !handed {
		return out
	}
	args := e.Arguments
	if m, ok := e.Function.(*ast.MemberExpr); ok {
		args = append([]ast.Expression{m.Object}, args...)
	}
	for _, arg := range args {
		ast.WalkExpr(arg, nil, func(x ast.Expression) bool {
			switch x := x.(type) {
			case *ast.LambdaExpr:
				out = append(out, lambdaKey(x))
				return false
			case *ast.IdentifierExpr:
				if c.writes != nil {
					if lam := c.writes.namedFunctionIn(c.bound, x); lam != nil {
						out = append(out, lambdaKey(lam))
					}
				}
			case *ast.MemberExpr:
				if lam := c.namespacedFunction(c.bound, x); lam != nil {
					out = append(out, lambdaKey(lam))
				}
			}
			return true
		})
	}
	return out
}

// namedFunctionIn is namedFunction for a caller's own record of what its functions bind.
func (w *globalWrites) namedFunctionIn(bound []map[string]bool, id *ast.IdentifierExpr) *ast.LambdaExpr {
	saved := w.bound
	w.bound = bound
	defer func() { w.bound = saved }()
	return w.namedFunction(id)
}

// namespacedFunction is the function `lib.f` names, when lib is a namespace this file imports
// and nothing binds the name.
func (c *globalBorrows) namespacedFunction(bound []map[string]bool, m *ast.MemberExpr) *ast.LambdaExpr {
	ns, ok := m.Object.(*ast.IdentifierExpr)
	if !ok || c.symTable == nil {
		return nil
	}
	for _, frame := range bound {
		if frame[ns.Name] {
			return nil
		}
	}
	path, ok := c.symTable.NamespaceIn(c.symTable.ModuleOfFile[m.GetLocation().File], ns.Name)
	if !ok {
		return nil
	}
	fn, _ := c.symTable.LookupFunctionIn(path, m.Property.Name)
	return fn
}
