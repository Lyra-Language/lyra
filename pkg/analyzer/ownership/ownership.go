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
// # Deep ownership (deep-retain-on-copy)
//
// Ownership is **deep**: the question at every owning position is not "is this value
// itself refcounted?" but "does it transitively own anything refcounted?"
// (OwnsManaged). A `struct Person { name: string }` is not itself managed, yet a stack
// aggregate is a *value* — `let q = p` copies it, and the copy points at the same
// string box. Treating that as uninteresting (the old IsManaged test) left the copy
// holding a reference nobody had counted.
//
// That was **not merely a leak**. An uncounted alias is freed out from under whoever
// still holds it as soon as the counted owner dies, which was two ASan-confirmed
// use-after-frees: interior assignment through one copy (`let q = p; p.name = …;
// q.name`), and reading an aggregate by value out of a box whose drop glue then frees
// its fields (`let q = ps[0]` on a `[]Person`, then let the array die).
//
// So a copy of an aggregate is an owning position like any other: it retains each
// managed value the aggregate reaches (the backend's per-type retain glue,
// backend/llvm/retain.go), and the copy's death releases them again (the mirroring
// drop glue). Stack-aggregate bindings are framed and deep-released at scope exit,
// exactly like managed ones. The backend's needsDrop delegates to OwnsManaged so the
// two sides cannot drift apart — this pass decides where a +1 is minted and the
// backend decides where one is released, so any disagreement is a leak or a double
// free.
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

// OwnsManaged reports whether a value of Lyra type t transitively owns any
// reference-counted reference — so copying it duplicates a reference that must be
// retained, and its death must release one. This is what makes ownership *deep*: a
// `struct Person { name: string }` is not itself managed, but a copy of one is a
// second reference to the same string box.
//
// A managed type *is* such a reference and the walk stops there (its own box owns
// whatever it holds); an inline aggregate owns one when any field, element, or
// `data` variant field does. That "by value" stopping rule is also the termination
// argument: a recursive type's cycle must pass through a `shared` field (lyra-E014),
// which is managed, so the recursion returns before re-entering the cycle.
//
// symTable resolves an UnresolvedType — how a reference to another declared type is
// recorded in a field — to that declaration, carrying the reference's own allocation
// flavor across. A nil table (or an unknown name) leaves it unresolved and reports
// false, the leak-safe answer.
//
// This is the **single definition** used by both the ownership pass and the backend
// (whose needsDrop delegates here). They must agree exactly: the pass decides where a
// +1 is minted and the backend decides where one is released, so any divergence is
// either a leak or a double free.
func OwnsManaged(t types.Type, symTable *symbols.SymbolTable) bool {
	if t == nil {
		return false
	}
	if IsManaged(t) {
		return true
	}
	switch v := resolveNamedType(t, symTable).(type) {
	case types.NamedStructType:
		return slices.ContainsFunc(v.Fields, func(f types.StructField) bool {
			return OwnsManaged(f.Type, symTable)
		})
	case types.TupleType:
		return slices.ContainsFunc(v.Elements, func(e types.Type) bool {
			return OwnsManaged(e, symTable)
		})
	case types.DataType:
		return slices.ContainsFunc(v.Constructors, func(c types.DataTypeConstructor) bool {
			return slices.ContainsFunc(c.FieldTypes(), func(f types.Type) bool {
				return OwnsManaged(f, symTable)
			})
		})
	case types.StaticArrayType:
		// A `[N]T` owns whatever T owns, once per element.
		return OwnsManaged(v.ElementType, symTable)
	}
	return false
}

// resolveNamedType resolves an UnresolvedType to the declaration's actual type,
// carrying the reference's own allocation flavor across; any other type is returned
// unchanged. Shallow by design — OwnsManaged recurses field by field anyway.
func resolveNamedType(t types.Type, symTable *symbols.SymbolTable) types.Type {
	u, ok := t.(types.UnresolvedType)
	if !ok || symTable == nil {
		return t
	}
	decl, ok := symTable.Types[u.Name]
	if !ok {
		return t
	}
	return types.WithAllocation(decl.Type, u.Allocation)
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
			if a.bindingOwnsManaged(s) {
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
			if a.bindingOwnsManaged(s) {
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

// ownsManaged reports whether e's recorded type transitively owns a managed value —
// the test that governs every *owning position* decision in this pass.
//
// This is deliberately the *deep* test rather than IsManaged: a `Person` struct with
// a `string` field is not itself managed, but copying one duplicates a reference to
// that string's box, so the copy owes a retain exactly as a bare string copy would.
// Using IsManaged here is what left stack-aggregate copies unretained, which was a
// use-after-free and not merely a leak (see the package doc).
func (a *analyzer) ownsManaged(e ast.Expression) bool {
	t, ok := a.tt.Get(e)
	return ok && OwnsManaged(t, a.symTable)
}

// markMergeTemp marks an if/match expression as an owned temporary to release
// after its statement — the merged (phi) value the backend produces. It's set
// unconditionally; the backend applies it only to an actual managed value (a
// non-managed if/match phi is skipped by the string-type guard in lowerExpr).
func (a *analyzer) markMergeTemp(e ast.Expression) {
	a.table.ReleaseTemp[e] = true
}

// bindingOwnsManaged reports whether a `let`/`var` binding owns (transitively) a
// managed value: its declared type when annotated (reliable), else the initializer's
// recorded type. A binding that does is an *owning* position — its initializer is
// coerced to +1 — and the backend frames it so its scope exit releases what it owns.
func (a *analyzer) bindingOwnsManaged(vds *ast.VarDeclStmt) bool {
	if vds.Type != nil {
		return OwnsManaged(vds.Type, a.symTable)
	}
	return a.ownsManaged(vds.Value)
}

func (a *analyzer) stmt(s ast.AstNode) {
	switch s := s.(type) {
	case *ast.ExpressionStmt:
		a.expr(s.Expression, false) // value discarded → borrowing position
	case *ast.VarDeclStmt:
		// A managed binding owns its initializer (+1); a non-managed one borrows.
		a.expr(s.Value, a.bindingOwnsManaged(s))
	case *ast.VarReassignmentStmt:
		a.expr(s.Value, a.ownsManaged(s.Value))
	case *ast.LValueAssignmentStmt:
		// Interior assignment (`xs[i] = v`, `p.name = v`): the slot takes ownership of
		// the new value (+1) — the backend releases whatever the slot held before. A
		// non-managed target borrows (nothing to own).
		a.expr(s.Value, a.ownsManaged(s.Value))
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
		if !a.ownsManaged(e) {
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
		if needOwned && a.ownsManaged(e) {
			a.table.Retain[e] = true
		}

	case *ast.IndexExpr:
		// Indexing borrows out of the container (array / dynamic array), exactly like
		// a field read out of an aggregate. A managed *element* read into an owning
		// position must be duplicated: the container still owns the element and frees
		// it (its per-type drop glue on release), so a bare bind would free it twice —
		// a double-free / use-after-free. Mirrors the MemberExpr case. Previously an
		// IndexExpr hit `default` and recorded nothing, so the retain was missing.
		a.expr(e.Object, false)
		a.expr(e.Index, false)
		if needOwned && a.ownsManaged(e) {
			a.table.Retain[e] = true
		}

	case *ast.TupleIndexExpr:
		// Positional tuple access (`pair.0`) — same rule as MemberExpr / IndexExpr: a
		// managed element read into an owning position is duplicated, not moved.
		a.expr(e.Object, false)
		if needOwned && a.ownsManaged(e) {
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

	case *ast.MathBinaryOpExpr:
		// Arithmetic borrows its operands — they are numbers, so the operation never
		// owns a managed value itself. Recursing is still required, because a managed
		// value can sit *inside* an operand: `consume(p.name) + 1` passes a managed
		// field to an `own` parameter, which is an owning position needing a retain.
		//
		// These forms used to fall through to the default (record nothing), justified
		// as safe because "a missed release only leaks". That premise is wrong in the
		// same way the stack-aggregate use-after-free was: a missed *retain* at an
		// owning position is not a leak but a dangling reference — the callee released
		// a reference the caller never granted, so the struct's own drop then freed an
		// already-freed box (ASan-confirmed heap-use-after-free).
		a.expr(e.Left, false)
		a.expr(e.Right, false)

	case *ast.MathAssignOpExpr:
		// `total += consume(s)` — the target is a numeric slot; the RHS may contain
		// managed values in owning positions, exactly as above.
		a.expr(e.Right, false)

	case *ast.NegationExpr:
		a.expr(e.Operand, false)

	case *ast.TupleLiteralExpr:
		// The aggregate takes ownership of managed elements — transfer. A `shared`
		// tuple's box drops them via the per-type drop glue (drop.go); a stack tuple's
		// leak, since a stack aggregate has no death to hang a drop on (see the
		// package doc).
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
		// (drop.go); a stack array's managed elements leak, since a stack aggregate has
		// no death to hang a drop on (see the package doc for why that leak is not the
		// whole story).
		for _, el := range e.Elements {
			a.expr(el, true)
		}

	case *ast.ForLoopExpr:
		// A loop's value is discarded (it's a statement). Walk its parts so managed
		// reads inside record their retains. Previously the loop hit `default` and its
		// body was never analyzed, so a borrowed managed value bound into an owning
		// `let` inside the body (`for … { let y = s }`, s an `own` string) was released
		// by the backend's per-iteration frame with *no balancing retain* → a double-
		// free / use-after-free. A binding referenced in the body is excluded from
		// last-use precision (computeLastUse's `loopUsed`), so an owning read there is a
		// retain (a dup), never a transfer/drop that the back-edge would re-run on an
		// already-freed value; `conditional` guards against a stray transfer besides.
		savedCond := a.conditional
		a.conditional = true
		if e.Init != nil {
			a.stmt(e.Init)
		}
		if e.Condition != nil {
			a.expr(*e.Condition, false)
		}
		a.block(&e.Body, false)
		if e.Post != nil {
			a.expr(*e.Post, false)
		}
		a.conditional = savedCond

	case *ast.ForInLoopExpr:
		// Same as the C-style loop: the iterable is borrowed (the loop reads elements
		// out of it and the loop variable borrows each), and the body is analyzed under
		// `conditional` so an owning read of a (loop-referenced, hence last-use-excluded)
		// binding or of the borrowed loop variable records a retain, not a transfer.
		a.expr(e.Iterable, false)
		savedCond := a.conditional
		a.conditional = true
		a.block(&e.Body, false)
		a.conditional = savedCond

	// Anything not listed above records nothing. That is only sound for forms with
	// no managed value *anywhere beneath them* — note this is a stronger condition
	// than "this form's own value isn't managed", which is what the arithmetic cases
	// above got wrong: they carried owning positions inside their operands. Skipping
	// a node is emphatically **not** the safe default it was once documented to be
	// ("a missed release only leaks"), because a missed retain at an owning position
	// dangles rather than leaks. When adding an expression kind, recurse into every
	// sub-expression that can hold a value.
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

	if !a.ownsManaged(e) {
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
