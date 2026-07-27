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
// treated as owned (transferred, not released here), and its result as borrowed
// (never released).
//
// # Aggregates
//
// An aggregate field is an *owning* position: a managed value flowing into a
// struct/tuple/data field transfers its +1 to the aggregate, which owns it from
// then on. That reference is released by the backend's per-type **drop glue**
// (pkg/backend/llvm/drop.go), run as the box's drop_fn when a `shared` value's
// refcount reaches zero — so a string in a struct, or the `shared` tail of a
// `Cons` cell, is freed with the value that owns it.
//
// Symmetrically, a field a `match` arm binds out of a scrutinee is *duplicated*,
// never moved: the scrutinee's box drops its own fields when it dies, so a moved
// field would be freed twice. Eliding that dup/drop pair when the box is known
// unique is Perceus stage 4 (reuse specialization) — it costs refcount traffic
// today, but not allocations: reuse/FBIP reclaims the box shell either way.
//
// Still leaked conservatively: a managed value inside a plain **stack** aggregate.
// A stack aggregate is a value, so `let q = p` copies it and duplicates its field
// references with no retain; dropping both copies would double-free. Making that
// sound needs deep-retain-on-copy here first.
package ownership

import (
	"slices"

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
	// ReuseMatch[m]: m's scrutinee is an owned `shared data` binding at its last
	// use, so its ref-counted box may be *reclaimed* rather than freed (Perceus
	// reuse / FBIP). The value is the scrutinee binding's name — the box the backend
	// hands to a reuse-target construction in an arm (via a runtime `drop-reuse`
	// token: the box when unique, else null). The scrutinee's ordinary last-use drop
	// is suppressed (not marked here) because the token subsumes it: an arm that
	// reuses writes into the box, an arm that doesn't frees it.
	ReuseMatch map[*ast.MatchExpr]string
	// ReuseTarget[c]: c is a construction (a `data` value of the reuse-match's own
	// type) that consumes its enclosing reuse-match's token — it writes the new value
	// into the reclaimed box instead of allocating, when the token is non-null.
	ReuseTarget map[ast.Expression]bool
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

// ReuseScrutinee reports whether m is a reuse-match and, if so, the name of the
// scrutinee binding whose box the arms may reclaim.
func (t *Table) ReuseScrutinee(m *ast.MatchExpr) (string, bool) {
	if t == nil {
		return "", false
	}
	name, ok := t.ReuseMatch[m]
	return name, ok
}

// IsReuseTarget reports whether e is a construction that consumes its enclosing
// reuse-match's token (writes into the reclaimed box instead of allocating).
func (t *Table) IsReuseTarget(e ast.Expression) bool {
	return t != nil && t.ReuseTarget[e]
}

// IsManaged reports whether values of type t are reference-counted (freed via
// retain/release): a string, or a `shared`-flavored value (heap-allocated in a
// ref-counted box). This is the single definition of "managed", shared by the
// pass and the backend.
func IsManaged(t types.Type) bool {
	// A dynamic array `[]T` is always a heap-boxed, ref-counted value (dynarray.go),
	// so it is managed regardless of flavor — like a string.
	return types.IsString(t) || types.IsDynamicArray(t) || types.AllocationOf(t) == types.Shared
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
			ReuseMatch:      map[*ast.MatchExpr]string{},
			ReuseTarget:     map[ast.Expression]bool{},
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
	// reuseLastRef maps each owned managed binding (an eligible `let`/`var`, or an
	// `own` param) to the node of its final textual reference — used to decide
	// whether a `match` scrutinee is that binding's last use (a reuse source). Unlike
	// lastUse it includes `own` params (Perceus reuse reclaims a consumed argument's
	// cells), which is why it's computed separately.
	reuseLastRef map[string]ast.Expression
}

// lambda analyzes one function/lambda body under its own return-ownership.
func (a *analyzer) lambda(lam *ast.LambdaExpr) {
	saved := a.curReturnOwned
	savedCond := a.conditional
	savedLast := a.lastUse
	savedReuse := a.reuseLastRef
	a.curReturnOwned = isOwnedReturn(lam.ReturnType.TypeModifier)
	a.conditional = false
	a.lastUse = a.computeLastUse(lam)
	a.reuseLastRef = a.computeOwnedLastRef(lam)
	defer func() {
		a.conditional = savedCond
		a.lastUse = savedLast
		a.reuseLastRef = savedReuse
	}()
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

// computeOwnedLastRef maps each *owned* managed binding to the node of its final
// textual reference. An owned binding is an `own` managed parameter, or an eligible
// managed `let`/`var` (declared once, not a plain parameter, not reassigned) — the
// same eligibility as computeLastUse but *including* `own` params, since Perceus
// reuse reclaims the cells of a consumed argument. A name referenced inside a loop
// is excluded (a back-edge re-runs the reference, so "the last textual reference"
// isn't soundly its dynamic last use). The result decides whether a `match`
// scrutinee is a binding's last use — a precondition for reclaiming its box.
func (a *analyzer) computeOwnedLastRef(lam *ast.LambdaExpr) map[string]ast.Expression {
	declCount := map[string]int{}
	reassigned := map[string]bool{}
	params := map[string]bool{}
	ownParams := map[string]bool{}
	for _, p := range lam.Parameters {
		if ip, ok := p.Pattern.(*ast.IdentifierPattern); ok {
			params[ip.Name] = true
			if p.TypeModifier == types.Own {
				ownParams[ip.Name] = true
			}
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

	owned := func(name string) bool {
		if loopUsed[name] {
			return false
		}
		if ownParams[name] {
			return true
		}
		return declCount[name] == 1 && !params[name] && !reassigned[name]
	}

	lastRef := map[string]ast.Expression{}
	ast.WalkExpr(lam.Body, nil, func(e ast.Expression) bool {
		if id, ok := e.(*ast.IdentifierExpr); ok && owned(id.Name) {
			lastRef[id.Name] = id
		}
		return true
	})
	return lastRef
}

// reuseSource reports whether match m may reclaim its scrutinee's box (Perceus
// reuse) and, if so, the scrutinee binding's name. Requirements (all conservative,
// biased toward *not* reusing — a miss only forgoes an optimization):
//   - the scrutinee is a bare identifier naming an owned managed binding whose
//     *last use* is this scrutinee (computeOwnedLastRef), so the box is dead after;
//   - its type is a `shared data` value (reuse reclaims a heap box; strings and
//     stack values don't qualify);
//   - the arms are a plain tag switch — no guards, no value-testing payload
//     sub-patterns — matching exactly the backend path that wires reuse; and
//   - at least one arm constructs a value of the *same* `data` type (so the
//     reclaimed box, sized for that type, fits) — otherwise there's nothing to
//     reuse it for.
func (a *analyzer) reuseSource(m *ast.MatchExpr) (string, bool) {
	id, ok := m.Scrutinee.(*ast.IdentifierExpr)
	if !ok || a.reuseLastRef[id.Name] != ast.Expression(id) {
		return "", false
	}
	dtName, ok := a.sharedDataName(m.Scrutinee)
	if !ok || !plainTagSwitch(m) || !a.anyArmConstructs(m, dtName) {
		return "", false
	}
	return id.Name, true
}

// sharedDataName returns the name of e's `data` type when e is a `shared`-flavored
// data value, resolving an UnresolvedType through the symbol table.
func (a *analyzer) sharedDataName(e ast.Expression) (string, bool) {
	t, ok := a.tt.Get(e)
	if !ok || types.AllocationOf(t) != types.Shared {
		return "", false
	}
	switch v := t.(type) {
	case types.DataType:
		return v.Name, true
	case types.UnresolvedType:
		if a.symTable != nil {
			if decl, ok := a.symTable.Types[v.Name]; ok {
				if dt, ok := decl.Type.(types.DataType); ok {
					return dt.Name, true
				}
			}
		}
	}
	return "", false
}

// plainTagSwitch reports whether every arm of m is a plain tag-switch arm: a
// wildcard/identifier catch-all, or a data pattern whose payload sub-patterns only
// bind (no literal/range/nested-data value test), and no arm carries a guard. This
// mirrors the backend's switch-vs-ladder condition, so reuse is only claimed for a
// match the reuse-wired switch path will actually lower.
func plainTagSwitch(m *ast.MatchExpr) bool {
	for _, arm := range m.MatchArms {
		if arm.Guard != nil {
			return false
		}
		switch p := arm.Pattern.(type) {
		case *ast.WildcardPattern, *ast.IdentifierPattern:
		case *ast.DataPattern:
			if !dataPatternBindsOnly(p) {
				return false
			}
		default:
			return false
		}
	}
	return true
}

// dataPatternBindsOnly reports whether a data pattern's payload only binds/ignores
// — no literal, range, or nested value-testing sub-pattern that would force the
// backend's if-else ladder instead of the tag switch.
func dataPatternBindsOnly(p *ast.DataPattern) bool {
	return !patternHasValueTest(p.Pattern)
}

// patternHasValueTest reports whether a pattern imposes any runtime value test
// beyond binding (a literal/range, a nested data tag, or an aggregate containing
// one). Mirrors the backend's patternHasTest so the analysis and lowering agree.
func patternHasValueTest(pat ast.Pattern) bool {
	switch p := pat.(type) {
	case *ast.LiteralPattern, *ast.RangePattern, *ast.DataPattern:
		return true
	case *ast.StructPattern:
		return slices.ContainsFunc(p.Fields, func(f ast.StructPatternField) bool {
			return patternHasValueTest(f.Pattern)
		})
	case *ast.TuplePattern:
		return slices.ContainsFunc(p.Elements, patternHasValueTest)
	}
	return false
}

// anyArmConstructs reports whether any arm's tail expression constructs a value of
// the `shared data` type named dtName (a candidate to consume the reuse token).
func (a *analyzer) anyArmConstructs(m *ast.MatchExpr, dtName string) bool {
	for i := range m.MatchArms {
		if tc := tailConstruction(m.MatchArms[i].Body); tc != nil && a.constructsSharedData(tc, dtName) {
			return true
		}
	}
	return false
}

// constructsSharedData reports whether tc constructs a `shared`-flavored value of
// the `data` type named dtName.
func (a *analyzer) constructsSharedData(tc ast.Expression, dtName string) bool {
	name, ok := a.sharedDataName(tc)
	return ok && name == dtName
}

// tailConstruction returns the construction expression that is e's value — e itself
// when it's a `data` construction (a positional `TupleLiteralExpr` or a nullary
// `DataConstructorExpr`), or the tail of a block. It deliberately does NOT look
// inside an `if`/`match` tail: a construction reached only on one branch isn't
// guaranteed to consume the reuse token on every path through the arm, so it's not
// a safe unconditional reuse target. Returns nil when there's no such tail.
func tailConstruction(e ast.Expression) ast.Expression {
	switch v := e.(type) {
	case *ast.TupleLiteralExpr, *ast.DataConstructorExpr:
		return e
	case *ast.BlockExpr:
		if n := len(v.Statements); n > 0 {
			if es, ok := v.Statements[n-1].(*ast.ExpressionStmt); ok {
				return tailConstruction(es.Expression) // the block's value is its tail expression
			}
		}
	}
	return nil
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
		// A field an arm binds out of the scrutinee is *duplicated*, never moved: the
		// scrutinee's box drops its own fields when it dies (a real drop_fn on release,
		// and in drop-reuse's unique branch), so a moved field would be freed twice.
		// Moving it again — eliding the dup/drop pair when the box is statically or
		// dynamically known unique — is Perceus stage 4 reuse specialization; see the
		// package doc.
		if name, ok := a.reuseSource(e); ok {
			// The scrutinee's box is reclaimed via the runtime reuse token. We still
			// mark the scrutinee's ordinary drop above (robust if the backend doesn't
			// fire reuse), but when it does, the backend retires the scrutinee's slot at
			// the drop-reuse point, which suppresses that drop — the token subsumes it
			// (an arm reuses the box, or frees it). Mark the match and each arm's target.
			a.table.ReuseMatch[e] = name
			if dtName, ok := a.sharedDataName(e.Scrutinee); ok {
				for i := range e.MatchArms {
					if tc := tailConstruction(e.MatchArms[i].Body); tc != nil && a.constructsSharedData(tc, dtName) {
						a.table.ReuseTarget[tc] = true
					}
				}
			}
		}
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

	case *ast.ArrayLiteralExpr:
		// The array takes ownership of managed elements — transfer, like a tuple/
		// struct. A `shared` array's box drops its elements via the per-type drop glue
		// (drop.go); a stack array's managed elements leak conservatively (aggregate
		// drop on copy isn't implemented — see the package doc), never double-free.
		for _, el := range e.Elements {
			a.expr(el, true)
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
	// print/println are compiler-provided builtins whose string parameter is a
	// borrow, so an owned temporary argument (`print("a" ++ b)`) is released after
	// the call rather than conservatively transferred (leaked). Only when no user
	// function shadows the name (lam == nil) — matching the typechecker/backend
	// resolution order.
	builtinBorrows := lam == nil && calleeIsBorrowingBuiltin(e)

	for i, arg := range e.Arguments {
		argOwns := true // conservative default: transfer (leak-safe) for an unknown callee
		if lam != nil {
			argOwns = i < len(lam.Parameters) && paramOwnsArgument(lam.Parameters[i].TypeModifier)
		} else if builtinBorrows {
			argOwns = false
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

// calleeIsBorrowingBuiltin reports whether e is a direct call to a
// compiler-provided builtin whose parameters are borrows (bare), so an owned
// temporary argument must be released after the call rather than conservatively
// transferred (leaked). print/println borrow their `string` argument. Callers
// gate this on the name not being shadowed by a user function.
func calleeIsBorrowingBuiltin(e *ast.FunctionCallExpr) bool {
	id, ok := e.Function.(*ast.IdentifierExpr)
	if !ok {
		return false
	}
	switch id.Name {
	case "print", "println":
		return true
	}
	return false
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
