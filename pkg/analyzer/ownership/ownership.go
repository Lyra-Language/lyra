// Package ownership computes where the backend must retain and release
// reference-counted ("managed") values so they are freed exactly once. Today the
// only managed type is `string` (every string value is a pointer to a ref-counted
// box — a heap box for a `++` result, a pinned static box for a literal, so
// retain/release are total and safe on any string; see the llvm backend's
// STRING_LAYOUT.md / ALLOCATION.md). The analysis generalizes to `shared` values
// later.
//
// # The model (ARC over managed values)
//
// Every managed value carries a reference count. A binding (`let`/`var`) and an
// `own` parameter each hold one owning reference (+1), released once at scope
// exit — the backend does that with a cleanup stack (it knows a binding's type),
// so this pass does NOT record scope drops. What this pass computes is the two
// context-dependent adjustments the backend can't see locally:
//
//   - Retain: a *borrowed* value (an identifier/field read — +0) flowing into an
//     *owning* position (a binding initializer, an owned `return`, an `own`
//     argument) must be retained to create the +1 that position will release.
//   - ReleaseTemp: an *owned temporary* (a fresh `++` result, or an owned call
//     result — +1) flowing into a *borrowing* position (a `==`/`!=` operand, a
//     match scrutinee, a `++` operand, a discarded expression statement, a
//     borrowed argument) is dead after that use and must be released.
//
// Ownership of a value is decided by the expression that produces it (Owned vs
// Borrowed) and the requirement of the position that consumes it (needOwned).
// The four combinations: Owned→owning = transfer (nothing); Owned→borrowing =
// ReleaseTemp; Borrowed→owning = Retain; Borrowed→borrowing = nothing.
//
// # Safety bias
//
// A missed release leaks (unreclaimed memory) but is memory-safe; a spurious
// release double-frees or dangles. So every uncertain case is biased toward
// *transfer* (leak), never toward release: an unresolvable callee's arguments are
// treated as owned (transferred, not released here), its result as borrowed
// (never released); and a managed value flowing into an aggregate (struct/tuple/
// data field) is transferred (the aggregate conceptually owns it) and leaks,
// since per-type aggregate drop isn't implemented yet.
package ownership

import (
	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/ast/symbols"
	"github.com/Lyra-Language/lyra/pkg/types"
	"github.com/Lyra-Language/lyra/pkg/typetable"
)

// Table records the retain / release-temporary decisions for managed values,
// keyed by the expression node they apply to. The backend consults it while
// lowering: after producing an expression's value it retains when Retain is set,
// and it schedules a release at the end of the enclosing statement when
// ReleaseTemp is set. A nil *Table answers false to both (nothing managed).
type Table struct {
	// Retain[e]: e is a borrowed managed value consumed in an owning position;
	// retain it to mint the +1 that position releases.
	Retain map[ast.Expression]bool
	// ReleaseTemp[e]: e is an owned managed temporary consumed in a borrowing
	// position; release it after the enclosing statement.
	ReleaseTemp map[ast.Expression]bool
	// LastUseTransfer[e]: e is the final use of an owned binding in an *owning*
	// position — its reference is moved to the consumer, so no dup is needed and
	// the binding's slot is retired (Perceus transfer). Only set for a use that is
	// unconditional (not inside a branch), so the transfer happens on every path.
	LastUseTransfer map[ast.Expression]bool
	// LastUseDrop[e]: e is the final use of an owned binding in a *borrowing*
	// position — the binding is dead after this statement, so it is released here
	// (last-use precision) rather than at scope exit.
	LastUseDrop map[ast.Expression]bool
}

// ShouldRetain reports whether e's value must be retained when produced.
func (t *Table) ShouldRetain(e ast.Expression) bool {
	return t != nil && t.Retain[e]
}

// ShouldReleaseTemp reports whether e is an owned temporary to release after its
// enclosing statement.
func (t *Table) ShouldReleaseTemp(e ast.Expression) bool {
	return t != nil && t.ReleaseTemp[e]
}

// LastUse reports whether e is the last use of an owned binding and, if so,
// whether that use transfers the reference (owning position — no drop) or drops
// it (borrowing position — release here). ok is false when e is not a last use.
func (t *Table) LastUse(e ast.Expression) (transfer, ok bool) {
	if t == nil {
		return false, false
	}
	if t.LastUseTransfer[e] {
		return true, true
	}
	if t.LastUseDrop[e] {
		return false, true
	}
	return false, false
}

// IsManaged reports whether values of type t are reference-counted (freed via
// retain/release). Today: strings. This is the single definition of "managed",
// shared by the pass and the backend.
func IsManaged(t types.Type) bool {
	return types.IsString(t)
}

// Analyze walks the typed program and returns the retain/release-temp Table.
func Analyze(program *ast.Program, symTable *symbols.SymbolTable, tt *typetable.TypeTable) *Table {
	a := &analyzer{
		symTable: symTable,
		tt:       tt,
		table: &Table{
			Retain:          map[ast.Expression]bool{},
			ReleaseTemp:     map[ast.Expression]bool{},
			LastUseTransfer: map[ast.Expression]bool{},
			LastUseDrop:     map[ast.Expression]bool{},
		},
	}
	for _, stmt := range program.Statements {
		if vds, ok := stmt.(*ast.VarDeclStmt); ok {
			if lam, ok := vds.Value.(*ast.LambdaExpr); ok {
				a.lambda(lam)
				continue
			}
		}
		a.stmt(stmt)
	}
	return a.table
}

type analyzer struct {
	symTable       *symbols.SymbolTable
	tt             *typetable.TypeTable
	table          *Table
	curReturnOwned bool // does the enclosing function return an owned value?
	conditional    bool // are we inside a branch body (if/match arm)? gates transfers
	// lastUse holds the final-textual-reference node of each managed binding that is
	// eligible for last-use precision, in the function currently being analyzed.
	lastUse map[ast.Expression]bool
}

// lambda analyzes one function/lambda body under its own return-ownership.
func (a *analyzer) lambda(lam *ast.LambdaExpr) {
	saved := a.curReturnOwned
	savedCond := a.conditional
	savedLast := a.lastUse
	a.curReturnOwned = isOwnedReturn(lam.ReturnType.TypeModifier)
	a.conditional = false
	a.lastUse = a.computeLastUse(lam)
	defer func() { a.conditional = savedCond; a.lastUse = savedLast }()
	// The body is the function's return value: pass the return's ownership need
	// down unconditionally. Whether it actually causes a retain is decided at the
	// managed leaves (a non-managed return simply has none) — we don't gate on the
	// body's own type, which for a block/if isn't reliably recorded.
	if lam.Body != nil {
		a.expr(lam.Body, a.curReturnOwned)
	}
	for i := range lam.LambdaClauses {
		if b := lam.LambdaClauses[i].Body; b != nil {
			a.expr(b, a.curReturnOwned)
		}
		if g := lam.LambdaClauses[i].Guard; g != nil {
			a.expr(g.Condition, false)
		}
	}
	a.curReturnOwned = saved
}

// computeLastUse finds, for each managed binding eligible for last-use precision,
// the node of its *final textual reference* (pre-order = program order). A binding
// is eligible only when it's simple enough that "the last textual reference" is
// soundly its last dynamic use on every path:
//
//   - a managed `let`/`var` binding declared exactly once (no shadowing),
//   - whose name isn't a parameter,
//   - that is never reassigned (a `var s = …; s = …` has several live values),
//   - and isn't referenced inside a loop body (a back-edge re-runs earlier uses).
//
// Anything ineligible simply isn't given a last-use annotation and falls back to
// the scope-exit frame release (still correct — a missed last-use only defers the
// free, never double-frees). Over-approximating "used later" this way is the
// sound direction.
func (a *analyzer) computeLastUse(lam *ast.LambdaExpr) map[ast.Expression]bool {
	declCount := map[string]int{}   // managed let/var declarations per name
	reassigned := map[string]bool{} // names that are reassignment targets
	params := map[string]bool{}
	for _, p := range lam.Parameters {
		if ip, ok := p.Pattern.(*ast.IdentifierPattern); ok {
			params[ip.Name] = true
		}
	}
	onStmt := func(s ast.Statement) bool {
		switch s := s.(type) {
		case *ast.VarDeclStmt:
			if a.bindingIsManaged(s) {
				declCount[s.Name]++
			}
		case *ast.VarReassignmentStmt:
			reassigned[s.Name] = true
		}
		return true
	}

	// Names referenced anywhere inside a loop body are ineligible.
	loopUsed := map[string]bool{}
	ast.WalkExpr(lam.Body, onStmt, func(e ast.Expression) bool {
		switch le := e.(type) {
		case *ast.ForLoopExpr:
			collectNames(&le.Body, loopUsed)
		case *ast.ForInLoopExpr:
			collectNames(&le.Body, loopUsed)
		}
		return true
	})

	eligible := func(name string) bool {
		return declCount[name] == 1 && !params[name] && !reassigned[name] && !loopUsed[name]
	}

	// Pre-order walk records references in program order; the final one per eligible
	// name is its last use.
	lastRef := map[string]ast.Expression{}
	ast.WalkExpr(lam.Body, nil, func(e ast.Expression) bool {
		if id, ok := e.(*ast.IdentifierExpr); ok && eligible(id.Name) {
			lastRef[id.Name] = id
		}
		return true
	})

	out := make(map[ast.Expression]bool, len(lastRef))
	for _, node := range lastRef {
		out[node] = true
	}
	return out
}

// collectNames records every identifier name referenced within a block.
func collectNames(block *ast.BlockExpr, into map[string]bool) {
	ast.WalkExpr(block, nil, func(e ast.Expression) bool {
		if id, ok := e.(*ast.IdentifierExpr); ok {
			into[id.Name] = true
		}
		return true
	})
}

// isManaged reports whether e's recorded type is a managed (ref-counted) type.
func (a *analyzer) isManaged(e ast.Expression) bool {
	t, ok := a.tt.Get(e)
	return ok && IsManaged(t)
}

// markMergeTemp marks an if/match expression as an owned temporary to release
// after its statement — the merged (phi) value the backend produces. It's set
// unconditionally; the backend applies it only to an actual managed value (a
// non-managed if/match phi is skipped by the string-type guard in lowerExpr).
func (a *analyzer) markMergeTemp(e ast.Expression) {
	a.table.ReleaseTemp[e] = true
}

// bindingIsManaged reports whether a `let`/`var` binding owns a managed value:
// its declared type when annotated (reliable), else the initializer's recorded
// type.
func (a *analyzer) bindingIsManaged(vds *ast.VarDeclStmt) bool {
	if vds.Type != nil {
		return IsManaged(vds.Type)
	}
	return a.isManaged(vds.Value)
}

func (a *analyzer) stmt(s ast.AstNode) {
	switch s := s.(type) {
	case *ast.ExpressionStmt:
		a.expr(s.Expression, false) // value discarded → borrowing position
	case *ast.VarDeclStmt:
		// A managed binding owns its initializer (+1); a non-managed one borrows.
		a.expr(s.Value, a.bindingIsManaged(s))
	case *ast.VarReassignmentStmt:
		a.expr(s.Value, a.isManaged(s.Value))
	case *ast.ReturnStmt:
		if s.Value != nil {
			// Pass the return's ownership need down; managed leaves decide the retain.
			a.expr(s.Value, a.curReturnOwned)
		}
	}
}

// expr walks e, marking retains/temp-releases on managed sub-values. needOwned is
// the requirement of e's consuming position: true = the consumer takes ownership
// (a +1), false = the consumer borrows (+0).
func (a *analyzer) expr(e ast.Expression, needOwned bool) {
	switch e := e.(type) {
	case nil:
		return

	case *ast.StringLiteralExpr:
		// A literal is a pinned static box: retain/release no-op, so nothing to
		// record (marking it would only emit dead no-op calls).

	case *ast.StringConcatExpr:
		// Owned producer (a fresh rc=1 box). Its operands are borrowed (++ copies
		// their bytes).
		a.expr(e.Left, false)
		a.expr(e.Right, false)
		if !needOwned {
			a.table.ReleaseTemp[e] = true
		}

	case *ast.InterpolatedStringExpr:
		// Owned producer once it lowers (deferred in the backend today). Segments
		// are borrowed.
		for _, seg := range e.Segments {
			a.expr(seg, false)
		}
		if !needOwned {
			a.table.ReleaseTemp[e] = true
		}

	case *ast.IdentifierExpr:
		// A read of an existing binding is a borrow. What happens depends on whether
		// this is the binding's last use (Perceus precision):
		//   - last use, owning position: transfer the reference (no dup), unless the
		//     use is conditional (inside a branch) — then a path that skips it would
		//     leak, so fall back to a dup.
		//   - last use, borrowing position: the binding is dead after this statement,
		//     so drop it here rather than at scope exit.
		//   - not the last use, owning position: dup (retain) to mint a fresh +1.
		if !a.isManaged(e) {
			return
		}
		last := a.lastUse[e]
		switch {
		case last && needOwned && !a.conditional:
			a.table.LastUseTransfer[e] = true
		case last && !needOwned:
			a.table.LastUseDrop[e] = true
		case needOwned:
			a.table.Retain[e] = true
		}

	case *ast.MemberExpr:
		// Field access borrows out of the aggregate; borrowing the object.
		a.expr(e.Object, false)
		if needOwned && a.isManaged(e) {
			a.table.Retain[e] = true
		}

	case *ast.FunctionCallExpr:
		a.call(e, needOwned)

	case *ast.BlockExpr:
		a.block(e, needOwned)

	case *ast.IfExpr:
		// An if/match merges its branches into one value (a phi). Each branch is
		// coerced to an owned +1 (a borrowed branch value is retained, an owned one
		// transferred), so the merged value is uniformly owned and can be released
		// once — here, not per branch: a per-branch release would free the value the
		// phi still refers to. So the branches are owning positions (true), and the
		// merged value itself is the temporary released when the consumer borrows it.
		a.expr(e.Condition, false)
		// Branch bodies are conditional: a last-use transfer inside one wouldn't run
		// on the other path, so transfers are suppressed there (see IdentifierExpr).
		savedCond := a.conditional
		a.conditional = true
		a.expr(e.Then, true)
		a.expr(e.Else, true)
		a.conditional = savedCond
		if !needOwned {
			a.markMergeTemp(e)
		}

	case *ast.MatchExpr:
		a.expr(e.Scrutinee, false) // scrutinee is borrowed
		savedCond := a.conditional
		a.conditional = true
		for i := range e.MatchArms {
			a.expr(e.MatchArms[i].Body, true) // arms coerced to owned; merged value owns
			if g := e.MatchArms[i].Guard; g != nil {
				a.expr(g.Condition, false)
			}
		}
		a.conditional = savedCond
		if !needOwned {
			a.markMergeTemp(e)
		}

	case *ast.BooleanBinaryOpExpr:
		// Comparisons (incl. string ==/!=) borrow both operands.
		a.expr(e.Left, false)
		a.expr(e.Right, false)

	case *ast.TupleLiteralExpr:
		// The aggregate takes ownership of managed elements; transfer (they then
		// leak, since aggregate drop isn't implemented — see the package doc).
		for _, el := range e.Elements {
			a.expr(el, true)
		}

	case *ast.StructInstanceExpr:
		for i := range e.Fields {
			a.expr(e.Fields[i].Value, true)
		}

	// Numeric / control forms with no managed sub-values in a string program are
	// intentionally not recursed into: not recording anything is always safe
	// (a missed release only leaks). String-reachable positions are all handled
	// above.
	default:
	}
}

// block analyzes a block: every non-tail statement in its own context, and the
// tail expression (the block's value) under the block's own requirement.
func (a *analyzer) block(e *ast.BlockExpr, needOwned bool) {
	n := len(e.Statements)
	for i, s := range e.Statements {
		if i == n-1 {
			if es, ok := s.(*ast.ExpressionStmt); ok {
				a.expr(es.Expression, needOwned) // tail = block value
				return
			}
		}
		a.stmt(s)
	}
}

// call analyzes a function call: each argument under its parameter's ownership
// mode, and the call result under needOwned.
func (a *analyzer) call(e *ast.FunctionCallExpr, needOwned bool) {
	lam := a.resolveCallee(e)

	for i, arg := range e.Arguments {
		argOwns := true // conservative default: transfer (leak-safe) for an unknown callee
		if lam != nil {
			argOwns = i < len(lam.Parameters) && paramOwnsArgument(lam.Parameters[i].TypeModifier)
		}
		a.expr(arg, argOwns)
	}

	if !a.isManaged(e) {
		return
	}
	// The result's ownership: an owned return is a fresh +1; anything else (a
	// borrowed return, or an unresolved callee) is treated as borrowed.
	resultOwned := lam != nil && isOwnedReturn(lam.ReturnType.TypeModifier)
	if resultOwned {
		if !needOwned {
			a.table.ReleaseTemp[e] = true
		}
	} else if needOwned {
		a.table.Retain[e] = true
	}
}

// resolveCallee returns the LambdaExpr for a direct call to a top-level named
// function, or nil when the callee can't be resolved (a method call, a call
// through a local, a type-conversion call) — in which case call() falls back to
// the leak-safe conservative modes.
func (a *analyzer) resolveCallee(e *ast.FunctionCallExpr) *ast.LambdaExpr {
	id, ok := e.Function.(*ast.IdentifierExpr)
	if !ok || a.symTable == nil {
		return nil
	}
	return a.symTable.Functions[id.Name]
}

// paramOwnsArgument / isOwnedReturn mirror the typechecker's ownership predicates
// (assignable.go): only an `own` parameter adopts its argument, and a bare or
// `own` return transfers ownership to the caller (a `ref`/`mut` return borrows).
func paramOwnsArgument(mod types.TypeModifier) bool {
	return mod == types.Own
}

func isOwnedReturn(mod types.TypeModifier) bool {
	return mod != types.Ref && mod != types.Mut
}
