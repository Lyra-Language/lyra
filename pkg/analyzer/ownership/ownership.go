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
	// A newtype is nominal only — `newtype Email = string` is represented exactly
	// as a string — so managed-ness is a property of the base (types.StripNewtype).
	t = types.StripNewtype(t)
	// A `weak T` is a non-owning reference to a box, and it too has a lifecycle: it
	// holds a *weak* count that keeps the box's memory alive, so a copy takes one and
	// a death drops one. Without that the memory a dead-but-weakly-referenced box
	// occupies is never freed. The backend releases it with the weak shim rather than
	// the strong one (see deepRelease) — a weak reference never owns the value, only
	// the storage.
	if _, ok := t.(types.WeakType); ok {
		return true
	}
	// A function value is a boxed closure: a code pointer paired with a
	// ref-counted environment (closures.go), so it is managed like a string —
	// copying one shares an environment, and the last reference frees it. A
	// captureless closure shares a *pinned* environment, on which retain and
	// release are no-ops, so this needs no special case for it.
	if _, ok := t.(*types.LambdaType); ok {
		return true
	}
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
func OwnsManaged(t types.Type, symTable *symbols.SymbolTable, loc ast.Location) bool {
	return ownsManaged(t, symTable, loc, nil)
}

// ownsManaged is OwnsManaged with the cycle guard threaded through.
//
// **seen tracks newtype names, and it exists because this pass runs on invalid programs.**
// The termination argument below holds for every *well-formed* type: a recursive one must
// break its cycle with a `shared` or `weak` field (lyra-E014), and both are managed
// outright. A newtype cycle — `newtype A = B` beside `newtype B = A` — satisfies none of
// that and is refused by the typechecker, but the driver runs every pass regardless so an
// editor still gets diagnostics from all of them, so "the typechecker rejected it" is not
// a guarantee this walk may rely on. Without the set it recursed until the stack ended,
// taking `lyrac` and the language server down with it.
func ownsManaged(t types.Type, symTable *symbols.SymbolTable, loc ast.Location, seen map[string]bool) bool {
	if t == nil {
		return false
	}
	if IsManaged(t) {
		return true
	}
	resolved := resolveNamedType(t, symTable, loc)
	// A generic instantiation is the one arm this does not share with SharedMutablePath,
	// and the difference is a cycle guard rather than an oversight: that walk carries a
	// `seen` set keyed on the type's name, and this one does not need it. A recursive type
	// must break its cycle with a `shared` or `weak` field (lyra-E014), and both are
	// managed outright — IsManaged answers them above before this can recurse — so a cycle
	// is always cut. SharedMutablePath keeps looking *past* a shared value that has no
	// writable field, which is exactly the case that can come back around.
	if pt, ok := resolved.(types.ParameterizedType); ok {
		// One instantiation of a generic type owns what its *substituted* contents own:
		// `Box<string>` owns a string, `Box<i64>` owns nothing — so the question cannot be
		// answered from the declaration alone, whose field type is the variable `t` and
		// owns nothing at all.
		//
		// Missing this case was a real double free, not a leak. The two halves of the
		// model read the type through different paths: this pass (which decides where a +1
		// is minted) saw the raw ParameterizedType and recorded no retain for a copy, while
		// the backend (which decides where one is released) reads types through
		// recordedType, which normalizes an instantiation to its substituted struct — so it
		// framed and deep-released *both* bindings. Drop twice, dup never.
		inst, ok := instantiateDecl(pt, symTable, loc)
		return ok && ownsManaged(inst, symTable, loc, seen)
	}
	// A newtype is the one component that can lead back to itself without passing through
	// something managed, so it is the one whose name is remembered.
	if ct, ok := resolved.(*types.ConstrainedType); ok {
		if seen[ct.Name] {
			return false
		}
		if seen == nil {
			seen = map[string]bool{}
		}
		seen[ct.Name] = true
	}
	return eachComponent(resolved, func(_ string, comp types.Type) bool {
		return ownsManaged(comp, symTable, loc, seen)
	})
}

// eachComponent calls yield for every **structural component** of t — the types a value of
// t holds inline — passing the field's name where it has one and "" where the position is
// positional (a tuple element, a `data` payload, an array element, a newtype's base). It
// stops and reports true as soon as a yield does.
//
// This is the fold OwnsManaged and SharedMutablePath share. Both used to spell it out, and
// a switch over composite types that exists twice drifts (CLAUDE.md rule 8) — this is the
// family `AnonymousStructType` was missing from in three places at once. Generic
// instantiation is deliberately *not* here: it is a substitution rather than a component,
// and the two callers guard it differently. See OwnsManaged.
func eachComponent(t types.Type, yield func(name string, ct types.Type) bool) bool {
	switch v := t.(type) {
	case *types.ConstrainedType:
		// A newtype owns exactly what its base owns. IsManaged strips a *direct* wrapper;
		// this catches the one arriving as an UnresolvedType — how a field typed `Email`
		// is recorded.
		return yield("", v.Type)
	case types.NamedStructType:
		for _, f := range v.Fields {
			if yield(f.Name, f.Type) {
				return true
			}
		}
	case types.AnonymousStructType:
		for _, f := range v.Fields {
			if yield(f.Name, f.Type) {
				return true
			}
		}
	case types.TupleType:
		for _, e := range v.Elements {
			if yield("", e) {
				return true
			}
		}
	case types.DataType:
		for _, c := range v.Constructors {
			for _, f := range c.FieldTypes() {
				if yield("", f) {
					return true
				}
			}
		}
	case types.StaticArrayType:
		// A `[N]T` holds whatever T holds, once per element.
		return yield("", v.ElementType)
	}
	return false
}

// instantiateDecl substitutes a generic type's arguments into its declaration, giving the
// concrete type one instantiation actually is. False when the name does not resolve.
func instantiateDecl(p types.ParameterizedType, symTable *symbols.SymbolTable, loc ast.Location) (types.Type, bool) {
	if symTable == nil {
		return nil, false
	}
	decl, ok := symTable.LookupTypeFrom(p.Name, loc)
	if !ok {
		return nil, false
	}
	return types.Substitute(decl.Type, ast.BindGenericParams(decl.GenericParams, p.TypeArguments)), true
}

// resolveNamedType resolves an UnresolvedType to the declaration's actual type,
// carrying the reference's own allocation flavor across; any other type is returned
// unchanged. Shallow by design — OwnsManaged recurses field by field anyway.
func resolveNamedType(t types.Type, symTable *symbols.SymbolTable, loc ast.Location) types.Type {
	u, ok := t.(types.UnresolvedType)
	if !ok || symTable == nil {
		return t
	}
	decl, ok := symTable.LookupTypeFrom(u.Name, loc)
	if !ok {
		return t
	}
	return types.WithAllocation(decl.Type, u.Allocation)
}

// SharesMutableState reports whether two copies of a value of type t can observe
// each other's writes — whether copying it duplicates a reference to storage that
// can then be *mutated* through either copy.
//
// It is the third and narrowest of a graded trio, and the distinctions are what make
// it useful rather than alarming:
//
//   - IsManaged — is this value itself a ref-counted reference?
//   - OwnsManaged — does copying it duplicate a reference, so refcounting must run?
//   - SharesMutableState — is that duplication *observable* to a program?
//
// A string is managed and copying one shares its box, so both of the first two say
// yes; a string is immutable, so there is nothing to mutate through and this says no.
// That gap is the whole point: `["hi"; 3]` is correct, unremarkable code and by far
// the commoner spelling, so a diagnostic built on "managed" would fire mostly on it.
//
// What answers yes, each rung measured rather than reasoned from the layout (08/18,
// `[v; n]` then a write through slot 0):
//
//   - a dynamic array `[]T` — always. Its elements live behind the box pointer a
//     copy shares, and `xs[0] = v` and `xs.push(v)` write them.
//   - a `shared` aggregate with any field that is not `readonly` — the box is the
//     value, and a field write goes through it.
//   - any inline aggregate — a struct, tuple, `[N]T`, `data` payload, or one
//     instantiation of a generic type — that *contains* one of the above. The copy
//     is shallow, so `struct Row { cells: []i64 }` shares its cells.
//
// And what does not: a scalar and a `[N]T` of scalars (copied outright), a string
// and a `rune` (immutable), a `shared` scalar (assigning to the binding rebinds it
// rather than writing the box).
//
// **`readonly` is not honoured on the way down**, only at the top of a `shared`
// aggregate, and that asymmetry is measured too. Freezing a field stops the direct
// write — `fs[0].cells[0] = 7` is lyra-E001 — but not the two-line launder that gets
// the same effect:
//
//	let mut c = fs[0].cells
//	c[0] = 7                  // writes every slot's cells
//
// So a frozen field whose *type* shares mutable state still shares it, and treating
// `readonly` as a stopping rule would be a false negative in exactly the shape
// (a struct wrapping an array) the predicate exists for.
//
// A closure is deliberately excluded although it too shares a mutable environment.
// Copying a function value shares its captures in every language that has both, so
// `[counter; 3]` is not the "n independent copies" mistake this is looking for — it
// is what a closure *is*.
func SharesMutableState(t types.Type, symTable *symbols.SymbolTable, loc ast.Location) bool {
	_, shares := SharedMutablePath(t, symTable, loc, nil)
	return shares
}

// SharedMutablePath is SharesMutableState with its evidence: the chain of **struct
// field** names from t down to the thing that actually shares, empty when t is itself
// that thing (or reaches it through a tuple or a `data` payload, whose own spelling
// already shows what they hold).
//
// One implementation, two entry points, for the reason OwnsManaged is the single
// definition of managed-ness: a diagnostic that explains *why* a `Row` aliases has to
// agree with the predicate that decided it does, and a second walk written to answer
// "which field" would be free to drift from the one that answers "whether".
//
// seen guards the recursion by type name; pass nil at the top. OwnsManaged rests on
// lyra-E014 (a recursive type's cycle must pass through a `shared` field, which it
// answers outright) and needs no guard, but this walk can decline a `shared`
// aggregate and keep descending, so the cycle is reachable here.
func SharedMutablePath(t types.Type, symTable *symbols.SymbolTable, loc ast.Location, seen map[string]bool) ([]string, bool) {
	if t == nil {
		return nil, false
	}
	// Nominal only — `newtype Grid = []i64` is represented exactly as its base, and
	// shares exactly what its base shares.
	t = types.StripNewtype(t)
	if types.IsDynamicArray(t) {
		return nil, true
	}
	if seen == nil {
		seen = map[string]bool{}
	}
	if u, isNamed := t.(types.UnresolvedType); isNamed {
		if seen[u.Name] {
			return nil, false
		}
		seen[u.Name] = true
	}
	resolved := resolveNamedType(t, symTable, loc)
	if types.AllocationOf(resolved) == types.Shared && hasWritableField(resolved, symTable, loc) {
		return nil, true
	}
	// The generic arm keeps its own cycle guard — see OwnsManaged for why only this walk
	// needs one: it looks *past* a shared value with no writable field, which is exactly
	// the shape that can come back around to itself.
	if pt, ok := resolved.(types.ParameterizedType); ok {
		// Substituted, for OwnsManaged's reason: `Maybe<[]i64>` shares, `Maybe<i64>` does
		// not, and the declaration alone — whose payload is the variable `t` — cannot tell
		// them apart.
		if seen[pt.Name] {
			return nil, false
		}
		seen[pt.Name] = true
		inst, ok := instantiateDecl(pt, symTable, loc)
		if !ok {
			return nil, false
		}
		return SharedMutablePath(inst, symTable, loc, seen)
	}
	// A struct's field name is prefixed to the path; a tuple element, `data` payload or
	// array element contributes none. No name is prefixed for those because they render
	// their contents in their own spelling — `(i64, []i64)`, `Maybe<[]i64>` — so the
	// sharing part is already on the page, and `.1` or `.Some` would only repeat it. A
	// struct is the case that needs the path, since `Row` says nothing about what it holds.
	var path []string
	if eachComponent(resolved, func(name string, ct types.Type) bool {
		p, shares := SharedMutablePath(ct, symTable, loc, seen)
		if !shares {
			return false
		}
		if name != "" {
			p = append([]string{name}, p...)
		}
		path = p
		return true
	}) {
		return path, true
	}
	return nil, false
}

// hasWritableField reports whether a `shared` aggregate has a field a program can
// write through the box. An all-`readonly` struct has none, and a `shared` scalar has
// no fields at all — assigning to the binding rebinds it rather than writing the box,
// which is why the allocation flavor alone is not the test.
func hasWritableField(t types.Type, symTable *symbols.SymbolTable, loc ast.Location) bool {
	switch v := t.(type) {
	case types.NamedStructType:
		return slices.ContainsFunc(v.Fields, func(f types.StructField) bool { return !f.Frozen })
	case types.AnonymousStructType:
		return slices.ContainsFunc(v.Fields, func(f types.StructField) bool { return !f.Frozen })
	case types.TupleType:
		return len(v.Elements) > 0
	case types.StaticArrayType:
		return v.Size > 0
	case types.DataType:
		return slices.ContainsFunc(v.Constructors, func(c types.DataTypeConstructor) bool {
			return len(c.FieldTypes()) > 0
		})
	case types.ParameterizedType:
		if symTable == nil {
			return false
		}
		decl, ok := symTable.LookupTypeFrom(v.Name, loc)
		if !ok {
			return false
		}
		return hasWritableField(decl.Type, symTable, loc)
	}
	return false
}

// Analyze walks the typed program and returns the retain/release-temp Table.
func Analyze(program *ast.Program, symTable *symbols.SymbolTable, tt *typetable.TypeTable, mt *typetable.MethodTable) *Table {
	a := newAnalyzer(symTable, tt, nil, mt)
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

// AnalyzeLambda analyzes one function body under a type-variable substitution,
// producing a table for that **instantiation** alone.
//
// A generic body cannot be analyzed once and reused, because every decision this
// pass makes turns on whether a value is reference-counted — and with a type
// variable abstract, nothing is. Analyzed generically, `pick(a: t, b: t) -> t`
// records no retain on its returned value and no release for the caller's
// temporaries; at `t = string` that is a double free (measured: an ASan abort, 2
// allocations against 3 releases), and at `t = i64` the very same absence is
// correct. So the body is analyzed once per instantiation, and the backend consults
// the table for the specialization it is lowering.
//
// The tables cannot be merged: they are keyed by AST node, and the *same* node
// carries different annotations in different instantiations — which is precisely
// the information that was missing before.
func AnalyzeLambda(lam *ast.LambdaExpr, symTable *symbols.SymbolTable, tt *typetable.TypeTable, subst map[string]types.Type, mt *typetable.MethodTable) *Table {
	a := newAnalyzer(symTable, tt, subst, mt)
	a.lambda(lam)
	return a.table
}

// newAnalyzer builds an analyzer with an empty table, optionally bound to an
// instantiation's type arguments.
func newAnalyzer(symTable *symbols.SymbolTable, tt *typetable.TypeTable, subst map[string]types.Type, mt *typetable.MethodTable) *analyzer {
	return &analyzer{
		symTable: symTable,
		tt:       tt,
		subst:    subst,
		mt:       mt,
		table: &Table{
			Retain:          map[ast.Expression]bool{},
			ReleaseTemp:     map[ast.Expression]bool{},
			LastUseTransfer: map[ast.Expression]bool{},
			LastUseDrop:     map[ast.Expression]bool{},
			ReuseMatch:      map[*ast.MatchExpr]string{},
			ReuseTarget:     map[ast.Expression]bool{},
		},
	}
}

type analyzer struct {
	symTable *symbols.SymbolTable
	tt       *typetable.TypeTable
	// mt resolves a `.`-call to the trait method it dispatches to, which is the only way
	// this pass can read a *method's* parameter modes: an impl binds patterns, so the
	// `ref`/`mut`/`own` axis lives on the trait's declared signature. Nil-safe — without
	// it every method argument falls back to the conservative transfer below.
	mt *typetable.MethodTable
	// subst binds a generic function's type variables for the instantiation being
	// analyzed. A generic body is analyzed once *per instantiation* (AnalyzeLambda)
	// because managed-ness is a property of the concrete type: with `t` abstract,
	// IsManaged is false and the pass records no retain, release, or drop anywhere —
	// decisions that are simply wrong at `t = string`. Nil for ordinary code.
	subst          map[string]types.Type
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

	// Names referenced anywhere inside a loop body are ineligible, and so are names whose
	// address has been taken — see addressTakenNames.
	loopUsed := map[string]bool{}
	addrTaken := map[string]bool{}
	ast.WalkExpr(lam.Body, onStmt, func(e ast.Expression) bool {
		switch le := e.(type) {
		case *ast.ForLoopExpr:
			collectNames(le.Body, loopUsed)
		case *ast.ForInLoopExpr:
			collectNames(le.Body, loopUsed)
		case *ast.AddressOfExpr:
			noteAddressTaken(le, addrTaken)
		}
		return true
	})

	eligible := func(name string) bool {
		return declCount[name] == 1 && !params[name] && !reassigned[name] &&
			!loopUsed[name] && !addrTaken[name]
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
	addrTaken := map[string]bool{}
	ast.WalkExpr(lam.Body, onStmt, func(e ast.Expression) bool {
		switch le := e.(type) {
		case *ast.ForLoopExpr:
			collectNames(le.Body, loopUsed)
		case *ast.ForInLoopExpr:
			collectNames(le.Body, loopUsed)
		case *ast.AddressOfExpr:
			noteAddressTaken(le, addrTaken)
		}
		return true
	})

	owned := func(name string) bool {
		// An address-taken binding is excluded here for the reason it is excluded from
		// last-use: reuse would hand its cells to something else while a pointer still
		// names them.
		if loopUsed[name] || addrTaken[name] {
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
	t, ok := a.typeOf(e)
	if !ok || types.AllocationOf(t) != types.Shared {
		return "", false
	}
	switch v := t.(type) {
	case types.DataType:
		return v.Name, true
	case types.UnresolvedType:
		if a.symTable != nil {
			if decl, ok := a.symTable.LookupTypeFrom(v.Name, e.GetLocation()); ok {
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

// noteAddressTaken records the binding an `&x` names, which is then excluded from last-use
// optimization.
//
// **Perceus rests on a premise `&` breaks.** Last-use says a binding's final *textual
// mention* is its final *use*, so the reference can be transferred rather than duplicated,
// or dropped there rather than at scope exit. Taking an address is precisely the operation
// that separates the two: a raw pointer keeps storage alive without counting as a reference
// to it, so the mention is not the use and the storage outlives it.
//
// The shape it is written for is `std.ffi`'s own:
//
//	let buf = CBuffer { ptr: unsafe { &xs[0] }, len: 3 }
//
// `&xs[0]` is the last mention of `xs`. Without this, last-use frees the array there and
// `buf.get(i)` reads whatever the allocator did next. It is an *element's* address, which is
// why the root of the place is what gets pinned rather than the operand itself — pinning
// `xs[0]` would pin nothing, since the thing that dies is `xs`.
//
// Pinning is the conservative direction: the binding falls back to a scope-exit release,
// which is where it was before Perceus and is memory-safe. It costs precision in exactly the
// functions that take an address, which is the trade `unsafe` code already makes.
func noteAddressTaken(e *ast.AddressOfExpr, into map[string]bool) {
	if root := ast.RootIdentifierName(e.Operand); root != "" {
		into[root] = true
	}
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
	t, ok := a.typeOf(e)
	return ok && OwnsManaged(t, a.symTable, e.GetLocation())
}

// typeOf is the pass's single type lookup: the recorded type of an expression with
// the current instantiation's type variables substituted. Everything the pass
// decides — whether a value is managed, whether a scrutinee is a reusable `shared
// data`, what convention an indirect callee uses — flows through it, which is what
// lets one generic body yield different (correct) annotations per instantiation.
func (a *analyzer) typeOf(e ast.Expression) (types.Type, bool) {
	t, ok := a.tt.Get(e)
	if !ok {
		return nil, false
	}
	return types.Substitute(t, a.subst), true
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
		// An annotated binding's type comes from the declaration, so it still mentions
		// the enclosing function's type variables inside a generic body — substitute
		// for the instantiation being analyzed (a `let copy: t = x` is managed exactly
		// when this instantiation's `t` is).
		return OwnsManaged(types.Substitute(vds.Type, a.subst), a.symTable, vds.GetLocation())
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
	case *ast.DerefAssignmentStmt:
		// `p^ = v`. The pointee slot takes ownership of the new value, exactly as an
		// interior assignment's does — without the retain, writing a managed value
		// through a pointer leaves two names on one box with one reference between
		// them, and the second release is an ASan-confirmed use-after-free.
		//
		// The pointer operand is walked borrowing: it is a place being written
		// through, not a value being consumed.
		a.expr(s.Target.Operand, false)
		a.expr(s.Value, a.ownsManaged(s.Value))
	case *ast.DestructuringDeclStmt:
		a.destructuringValue(s)
	case *ast.IfDestructuringStmt:
		a.destructuringValue(&s.DestructuringStatement)
		a.block(s.Then, false)
		if s.Else != nil {
			a.block(s.Else, false)
		}
	case *ast.ElseDestructuringStmt:
		a.destructuringValue(&s.DestructuringStatement)
		a.block(s.Else, false)
	case *ast.WithStmt:
		a.expr(s.Arena, false)
		a.block(s.Body, false)
	case *ast.BreakStmt:
		// A loop's value. `break` with a value does not lower yet, so this arm records
		// what the backend will need rather than anything it reads today — but leaving
		// it out is how the *next* feature inherits a silent hole.
		a.expr(s.Value, a.ownsManaged(s.Value))
	case *ast.ReturnStmt:
		if s.Value != nil {
			// Pass the return's ownership need down; managed leaves decide the retain.
			a.expr(s.Value, a.curReturnOwned)
		}
	}
}

// destructuringValue walks the value a destructuring binds from, **borrowing**.
//
// A pattern's bindings borrow out of the scrutinee rather than taking references from it:
// `if let Some(s) = m` reads the payload for the duration of the branch, and the only
// reference is the one `m` already holds and releases at scope exit. Claiming ownership
// here mints a retain the branch never gives back, and suppresses the scrutinee's own
// release — TestExec_DestructuringManagedPayload counts exactly that.
//
// So why walk it at all, if the answer is the same +0 that not walking gave?  Because
// "borrowed" and "unvisited" differ for a **temporary**: `if let Some(s) = make()` has a
// value nothing else names, and only a walk records the release that disposes of it. The
// arm's absence was harmless for the common case of a scrutinee that is a binding, and a
// leak for the one that is a call.
func (a *analyzer) destructuringValue(d *ast.DestructuringDeclStmt) {
	if d.Value != nil {
		a.expr(d.Value, false)
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

	case *ast.LambdaExpr:
		// Creating a closure is an owned producer: it allocates an environment box
		// (rc = 1), or shares the pinned empty one when it captures nothing — on
		// which release is a no-op, so treating both alike costs nothing.
		//
		// Its **captures are not analyzed here**. A capture is a copy taken at
		// creation, not a move out of the enclosing binding, and the backend mints
		// the environment's own +1 on each managed one (buildEnv) — recording a
		// retain here as well would double it. The body is a function in its own
		// right, so it is analyzed as one: its own last-use map, its own frames.
		// Without this the body got no annotations at all and every managed value
		// created inside a closure leaked.
		a.lambda(e)
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

	case *ast.TryExpr:
		// `x?` is a match in disguise — it tests the operand's tag, yields the success
		// payload, and on failure rewraps the error into the enclosing function's
		// return type and returns it. So the operand is **borrowed**, exactly as a
		// match scrutinee is, and the payload this expression yields is a field read
		// out of it: duplicated in an owning position, never moved, because the
		// operand still owns (and drops) everything it holds. Same rule as
		// MemberExpr / IndexExpr / TupleIndexExpr above.
		//
		// The failure path's rewrap is an owning position too, but it has no node of
		// its own to mark, so the backend emits that retain directly (try.go).
		//
		// Falling through to `default` here recorded nothing at all, which for a `?`
		// is not the leak-safe direction the rest of this pass biases toward: the
		// operand's own sub-expressions went unvisited, so a managed value inside one
		// (`parse(name)?`) missed its retain and dangled. See the package doc on why
		// skipping a node is never the conservative choice.
		a.expr(e.Operand, false)
		if needOwned && a.ownsManaged(e) {
			a.table.Retain[e] = true
		}

	case *ast.NullCoalescingExpr:
		// `a ?? b` is a two-armed match in disguise (`match a { Some(v) => v,
		// None => b }`, null_coalescing.go) and follows the MatchExpr rules exactly:
		// the optional is borrowed as a scrutinee, the default is a conditional arm
		// coerced to owned, and the merged value is a uniformly-owned temporary the
		// enclosing statement releases once. The Some arm's payload has no node of
		// its own to mark, so the backend emits its +1 directly — the same
		// arrangement as `?`'s failure rewrap (the TryExpr case above).
		a.expr(e.Optional, false)
		savedCond := a.conditional
		a.conditional = true
		a.expr(e.Default, true)
		a.conditional = savedCond
		if !needOwned {
			a.markMergeTemp(e)
		}

	case *ast.FunctionCallExpr:
		// A type conversion is its operand at run time — the identity forms
		// (`string(e)`, the lyra-E047 read-out spelling) return the operand's own
		// box, and the numeric forms carry no managed value at all — so it is
		// analyzed as the operand standing in this position, needOwned and all.
		// Routed through a.call instead, the unresolved-callee defaults applied:
		// `let s = string(copy.name)` bound the same box with neither a retain nor
		// a matching release, and the conservation count in
		// TestExec_NewtypeOverString_ASan caught the imbalance the day the spelling
		// was introduced.
		if id, ok := e.Function.(*ast.IdentifierExpr); ok && len(e.Arguments) == 1 {
			if _, isConv := types.ConversionTargetName(id.Name); isConv {
				a.expr(e.Arguments[0], needOwned)
				return
			}
		}
		// The universal read-out `base(v)` is the same identity, but its spelling is
		// shadowable by a user binding, so it is recognized by the typechecker's marker
		// rather than by name — a user `base(x)` stays an ordinary call.
		if a.tt.IsBaseReadout(e) {
			a.expr(e.Arguments[0], needOwned)
			return
		}
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

	case *ast.UnsafeBlockExpr:
		// `unsafe { … }` **is its body** — the keyword grants a permission and changes
		// nothing about values — so it is analyzed exactly as a BlockExpr, tail
		// expression included.
		//
		// It could not be, until taking an address stopped defeating last-use
		// (noteAddressTaken). Before that this arm was an ASan-visible use-after-free in
		// `std.ffi` itself: walking the block made `&xs[0]` the last *mention* of `xs` in
		// `CBuffer { ptr: unsafe { &xs[0] }, len: 3 }`, so the array was freed while the
		// pointer still named it. The two changes are one change, and reverting either
		// alone reopens the fault.
		//
		// What this closes is that no retain, drop or transfer was recorded anywhere
		// inside an unsafe block — the whole of a program's FFI and raw-pointer code —
		// which was conservative rather than unsound (every drop deferred to scope exit)
		// and cost a leak per managed value that lived only in there.
		a.block(e.Body, needOwned)

	case *ast.BooleanBinaryOpExpr, *ast.MathBinaryOpExpr, *ast.MathAssignOpExpr,
		*ast.BitwiseNotExpr, *ast.NegationExpr, *ast.NotBooleanExpr,
		*ast.AddressOfExpr, *ast.DerefExpr, *ast.RangeExpr, *ast.GuardExpr:
		// **Borrow-only forms**: the operation owns nothing itself — its result is a
		// number, a bool or a raw pointer — so every child sits in a borrowing
		// position. Recursing is still required, because a managed value can sit
		// *inside* an operand: `consume(p.name) + 1` passes a managed field to an `own`
		// parameter, which is an owning position needing a retain.
		//
		// The arithmetic forms used to fall through to the default (record nothing),
		// justified as safe because "a missed release only leaks". That premise is
		// wrong in the same way the stack-aggregate use-after-free was: a missed
		// *retain* at an owning position is not a leak but a dangling reference — the
		// callee released a reference the caller never granted, so the struct's own
		// drop then freed an already-freed box (ASan-confirmed heap-use-after-free).
		//
		// The last five kinds were still reaching the default until this case gathered
		// them: `!consume(p)`, `&mut p.name`, `q^`, `a..<consume(n)` and a match arm's
		// guard each recorded nothing at all. `&x` borrowing is not merely the
		// conservative choice but the correct one — a raw pointer is not an owner
		// (ownership never crosses the FFI boundary), so taking one must not retain.
		//
		// One asymmetry worth naming: this walks a compound assignment's **left** side,
		// which the hand-written arm skipped. It is inert rather than a change — all
		// eleven compound assignments are arithmetic or bitwise, so the target is a
		// numeric binding and `ownsManaged` answers false before anything is recorded.
		ast.WalkExprChildren(e, func(child ast.Statement) bool {
			a.stmt(child)
			return false
		}, func(child ast.Expression) bool {
			a.expr(child, false)
			return false
		})

	case *ast.ArrayCompExpr:
		// A comprehension builds a fresh array, so its **result is an owning position**
		// exactly as an array literal's elements are: the new array keeps a reference
		// to whatever each iteration produced. The generators are borrowed (it reads
		// elements out of the source, as a for-in does) and so are the guards, which
		// only test.
		//
		// The result is analyzed under `conditional`, for the reason the two loop forms
		// below set it: the result expression runs **once per iteration**, so an owning
		// read of a binding must record a retain rather than a single transfer of a
		// reference that N slots then hold.
		//
		// This is why the silent default could not simply be replaced by a borrow-walk
		// of children. Borrowing the result records a *last-use drop* on
		// `[i in 0..<2 | t]`, so the backend would free `t` inside the loop while the
		// array kept both pointers — turning a latent double-free at scope exit into an
		// immediate one.
		for i := range e.Generators {
			a.expr(e.Generators[i].Value, false)
		}
		for _, g := range e.Guards {
			a.expr(g, false)
		}
		savedCond := a.conditional
		a.conditional = true
		a.expr(e.Result, true)
		a.conditional = savedCond

	case *ast.TupleLiteralExpr:
		// A newtype construction shares this node and is **not** an aggregate: the
		// backend forwards its one operand unchanged (aggregates.go), so it is
		// analyzed as the operand standing in this position — the conversion call's
		// rule above, for the constructor that mirrors it. The aggregate arm below
		// would instead transfer the operand into a tuple that is never built.
		if t, ok := a.typeOf(e); ok {
			if _, isCT := t.(*types.ConstrainedType); isCT && len(e.Elements) == 1 {
				a.expr(e.Elements[0], needOwned)
				return
			}
		}
		// The aggregate takes ownership of managed elements — transfer. A `shared`
		// tuple's box drops them via the per-type drop glue (drop.go).
		for _, el := range e.Elements {
			a.expr(el, true)
		}
		// A **stack** tuple in a borrowing position is a temporary, and its elements'
		// +1s die with it — so it needs the same release any other owned producer gets
		// when its value is discarded (StringConcatExpr above is the model). Without
		// it the retains minted just above are never matched.
		//
		// This is the leak that failed TestExec_ReadLineUnderASan on CI, and it is
		// reached by ordinary code rather than an exotic one: a **multi-clause function
		// desugars to `match (p0, p1) { … }`**, so its scrutinee is exactly this — a
		// stack tuple built at the match site over the parameters. Every call to the
		// prelude's `unwrap_or` therefore leaked one reference to its receiver's box,
		// and `line.unwrap_or("")` in a read loop leaks one box per iteration. A
		// single-scrutinee `match m { … }` never did, because there is no tuple to
		// build — which is why this hid for so long behind a construct that looks like
		// pure sugar.
		//
		// The package doc called this out as a known limitation ("a stack tuple's
		// [elements] leak, since a stack aggregate has no death to hang a drop on").
		// It does have a death: the end of the statement that produced it, which is
		// where the backend already flushes temporaries and where deepRelease knows how
		// to walk an aggregate's managed fields. It is only *unowned* positions that
		// need this — an owning one (a `let`, an element of another aggregate) hands
		// the +1 on to something whose scope exit releases it.
		if !needOwned && a.ownsManaged(e) {
			a.table.ReleaseTemp[e] = true
		}

	case *ast.StructInstanceExpr:
		for i := range e.Fields {
			a.expr(e.Fields[i].Value, true)
		}

	case *ast.AnonymousStructInstanceExpr:
		// The same transfer, and the arm that matters most of the three this type
		// needed: without it the field values fell to the default walk as *unowned*, so
		// a temporary transferred into the struct (`{ m: "a" ++ "b" }`) was released at
		// the end of its own statement while the struct kept the pointer. That is a
		// use-after-free rather than the leak the missing retain/drop glue would cause,
		// and neither ASan nor LeakSanitizer reported it — the freed bytes simply were
		// not reused in between, which is exactly what makes this class expensive.
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

	case *ast.ArrayRepeatExpr:
		// `[v; n]` is the literal above with one element written once, so its value is
		// transferred exactly the same way. The count is a folded integer literal by
		// now and owns nothing, so it is not walked.
		//
		// The array takes **n** references to that one value, and the backend emits the
		// n-1 extra retains (lowerArrayRepeatExpr) — this pass only has to record the
		// transfer of the first, which is what `needOwned` does. Missing this arm sent
		// the value to `default`, where a managed element's transfer went unrecorded.
		a.expr(e.Value, true)

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
		a.block(e.Body, false)
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
		a.block(e.Body, false)
		a.conditional = savedCond

	// **What legitimately reaches here, and why nothing else may.** Recording nothing
	// is sound only for a form with no managed value *anywhere beneath it* — a stronger
	// condition than "this form's own value isn't managed", which is what the arithmetic
	// cases got wrong: they carried owning positions inside their operands. Skipping a
	// node is emphatically **not** the safe default it was once documented to be ("a
	// missed release only leaks"), because a missed retain at an owning position dangles
	// rather than leaks.
	//
	// Six kinds land here, each for a stated reason rather than by omission:
	//
	//   - DataConstructorExpr — a *nullary* constructor, and only ever nullary: the
	//     applied spelling collects to a named TupleLiteralExpr and the backend rejects
	//     a payload-carrying one outright. So it has no child to walk, and it is a fresh
	//     value already at +1.
	//   - AwaitExpr, YieldExpr, YieldFromExpr, ComposeExpr — no backend case exists, so
	//     a program containing one fails to lower before ownership matters. Each needs
	//     an arm here in the same change that lowers it, not later.
	//
	// **UnsafeBlockExpr used to be here**, and getting it out took a rule rather than a
	// case. `unsafe { … }` is its body, so the arm is obvious — and adding it alone broke
	// every FFI test, because walking the block made `&xs[0]` the last *mention* of `xs`
	// and last-use freed the array while the pointer still named it. Perceus rests on
	// "final mention is final use", and `&` is the operation that separates them. The rule
	// is noteAddressTaken; the arm came with it.
	//
	// When adding an expression kind, add its arm; if it borrows its children and owns
	// nothing, that is the multi-type case above rather than a new one.
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
	// An **indirect** call — through a closure value, a function-typed parameter,
	// a field holding one — has no LambdaExpr to resolve, but its callee's static
	// LambdaType carries the same conventions the backend lowers against, so it is
	// not the "unknown callee" the conservative defaults below exist for. Reading
	// it matters most for the *result*: a closure returning a managed value
	// transfers a fresh reference, and treating that as borrowed leaks it.
	calleeType := a.calleeLambdaType(e)
	// print/println are compiler-provided builtins whose string parameter is a
	// borrow, so an owned temporary argument (`print("a" ++ b)`) is released after
	// the call rather than conservatively transferred (leaked). Only when no user
	// function shadows the name (lam == nil) — matching the typechecker/backend
	// resolution order.
	builtinBorrows := lam == nil && calleeIsBorrowingBuiltin(e)
	// …and its mirror on the other side: a builtin *method* whose argument is stored
	// into the receiver rather than read through it. See calleeIsTransferringBuiltin.
	builtinTransfers := lam == nil && calleeIsTransferringBuiltin(e)
	// A `.`-call's modes live on the trait's declared signature — an impl binds patterns,
	// not typed parameters — and the receiver is signature parameter 0 while the arguments
	// start at 1. Getting that offset wrong would read each argument's mode from the
	// parameter to its left, which for `own` is a double free or a leak rather than a type
	// error, so it is spelled out here rather than folded into the loop index.
	methodSig := a.methodSignature(e)

	// …and parameter 0 is the receiver itself. An `own Self` method *consumes* the
	// receiver, so the caller transfers it rather than lending it — without this the
	// caller's frame still drops a box the callee already released, which ASan reports
	// as a heap-use-after-free inside `lyra_rc_release`. The arguments' offset was
	// handled from the start; the receiver was not, because it is not in e.Arguments.
	if member, isDot := e.Function.(*ast.MemberExpr); isDot && methodSig != nil && len(methodSig.Parameters) > 0 {
		if paramOwnsArgument(methodSig.Parameters[0].Borrow) {
			a.expr(member.Object, true)
		}
	}

	for i, arg := range e.Arguments {
		argOwns := true // conservative default: transfer (leak-safe) for an unknown callee
		switch {
		// Ahead of calleeType, which would otherwise answer for these: the typechecker
		// records a *builtin* method's signature on the MemberExpr, and its parameters
		// carry no written mode — which reads as a borrow, the wrong direction here.
		case builtinTransfers:
			argOwns = true
		case lam != nil:
			argOwns = i < len(lam.Parameters) && paramOwnsArgument(lam.Parameters[i].TypeModifier)
		case methodSig != nil:
			argOwns = i+1 < len(methodSig.Parameters) && paramOwnsArgument(methodSig.Parameters[i+1].Borrow)
		case calleeType != nil:
			// A function type *can* now express a mode (`(own i64) -> t`), so read it;
			// an unwritten one is a borrow, which is what the lifted closure body does
			// with it and what every function type meant before modes were collected.
			argOwns = i < len(calleeType.Parameters) && paramOwnsArgument(calleeType.Parameters[i].Borrow)
		case builtinBorrows:
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
	if lam == nil && calleeType != nil {
		resultOwned = isOwnedReturn(calleeType.ReturnType.TypeModifier)
	}
	// A compiler-provided builtin has neither a LambdaExpr nor a LambdaType, so it
	// lands on the "unresolved callee" default above — *borrowed*. That default is
	// the leak-safe one for arguments, but for a **result** it is the unsafe
	// direction: `read_line` hands back a freshly malloc'd string at +1, and calling
	// that borrowed makes the consuming site retain it (+2 for one allocation, a
	// leak) while a discarded call is never released at all. So an owning builtin
	// has to say so explicitly.
	if lam == nil && calleeType == nil && calleeIsOwningBuiltin(e) {
		resultOwned = true
	}
	if resultOwned {
		if !needOwned {
			a.table.ReleaseTemp[e] = true
		}
	} else if needOwned {
		a.table.Retain[e] = true
	}
}

// methodSignature returns the trait signature a `.`-call dispatches to, or nil when the
// call is not a resolved trait-method call (or no MethodTable was supplied).
//
// It is what lets this pass read a *method's* parameter modes at all. Without it every
// method argument fell to the conservative transfer below — leak-safe, and correct while
// trait signatures could not express a mode, but wrong the moment one says `own`.
func (a *analyzer) methodSignature(e *ast.FunctionCallExpr) *types.LambdaType {
	res, ok := a.mt.GetResolution(e)
	if !ok {
		return nil
	}
	return res.Signature
}

// calleeLambdaType returns the callee's static function type when the call goes
// through a function *value*, and nil otherwise (a direct call by name, a method
// call, a type conversion). It is what tells an indirect call apart from a
// genuinely unresolvable one.
func (a *analyzer) calleeLambdaType(e *ast.FunctionCallExpr) *types.LambdaType {
	if a.tt == nil || e.Function == nil {
		return nil
	}
	t, ok := a.typeOf(e.Function)
	if !ok {
		return nil
	}
	lt, _ := types.StripNewtype(t).(*types.LambdaType)
	return lt
}

// resolveCallee returns the LambdaExpr for a direct call to a top-level named
// function, or nil when the callee can't be resolved (a method call, a call
// through a local, a type-conversion call) — in which case call() falls back to
// the leak-safe conservative modes.
func (a *analyzer) resolveCallee(e *ast.FunctionCallExpr) *ast.LambdaExpr {
	// An **overloaded** callee cannot be resolved by name — several declarations share
	// it, told apart by the receiver's type — so the typechecker recorded which one this
	// call picked. Read first, and fall through for every other call, so a singly
	// declared name still resolves exactly as it did.
	if fn, ok := a.tt.Callee(e); ok {
		return fn
	}
	id, ok := e.Function.(*ast.IdentifierExpr)
	if !ok || a.symTable == nil {
		return nil
	}
	// Resolved as the *calling* file sees the name: a module's private function, and a
	// declaration that took a prelude name, are keyed by module, so a bare lookup would
	// hand back another module's function — and this pass reads the callee's parameter
	// modes to decide where a reference is retained.
	fn, _ := a.symTable.LookupFunctionFrom(id.Name, e.GetLocation())
	return fn
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

// calleeIsTransferringBuiltin reports whether e is a call to a compiler-provided
// **method** that takes ownership of an argument — `xs.push(v)`, which stores v into
// the array's buffer.
//
// It exists because the two general answers are both wrong here. The conservative
// default for an unknown callee is transfer, which would be right — but a builtin method
// is not unknown: the typechecker records its signature on the MemberExpr, so `calleeType`
// answers first, and a builtin signature carries no written `own`/`ref`/`mut`, which reads
// as a borrow. The argument would then be released after the call while the array kept the
// pointer, which is a use-after-free rather than a leak (`s.push("a" ++ "b")` printed
// garbage before this).
//
// The alternative was to write `own` on push's parameter in builtinMethodSignature. That
// would reach this pass correctly and *also* engage the use-after-move check, which is a
// different claim: an owning position takes a +1, it does not consume the caller's
// binding, and `xs.push(s); println(s)` is fine — the pass simply retains when the push is
// not s's last use. Keeping the two apart is why this is a hook rather than a mode, and it
// mirrors calleeIsBorrowingBuiltin, which exists for the same reason on the other side.
func calleeIsTransferringBuiltin(e *ast.FunctionCallExpr) bool {
	member, ok := e.Function.(*ast.MemberExpr)
	if !ok {
		return false
	}
	return member.Property.Name == "push"
}

// calleeIsOwningBuiltin reports whether e is a direct call to a compiler-provided
// builtin that returns an **owned** (+1) managed value, which the caller is
// therefore responsible for releasing.
//
// It is the result-side counterpart of calleeIsBorrowingBuiltin, and separate from
// it because the two answer different questions about different halves of a call —
// `print` borrows its argument and returns nothing; `read_line` takes nothing and
// returns an owned string. Folding them into one "builtin conventions" predicate
// would mean a builtin with neither property still having to appear in it.
//
// Callers gate this on the name not being shadowed by a user function (lam == nil),
// matching the typechecker's and backend's resolution order.
func calleeIsOwningBuiltin(e *ast.FunctionCallExpr) bool {
	id, ok := e.Function.(*ast.IdentifierExpr)
	if !ok {
		return false
	}
	return id.Name == "read_line"
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
