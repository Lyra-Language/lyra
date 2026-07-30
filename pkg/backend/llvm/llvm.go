// Package llvm is the (in-progress) LLVM IR backend for Lyra. It lowers a typed
// program from pkg/driver to LLVM IR (built with github.com/llir/llvm), which
// llc/clang then compiles.
//
// # Status: early
//
// Emit defines `@main` and lowers its body via lowerExpr, which so far handles
// integer/float/string/bool literals, arithmetic on ints and floats
// (`+ - * / % %% -(unary)`, incl. Odin-style floored `%%` vs truncated `%`),
// numeric conversions (int↔int, int→float, float widening — `i8(x)`, `f64(n)`, …),
// comparisons (`< <= > >= == !=` → icmp/fcmp, and string `==`/`!=` via a length
// check + memcmp), short-circuit `&&`/`||` (cond-br + phi), blocks (value = last
// expression), `if`/`else` (cond-br + phi diamond; one-armed `if` as a statement),
// `let`/`var` bindings, reassignment and compound assignment (`i += 1`), `for`
// loops with `break`/`continue` (cond/body/post/exit CFG; all forms — infinite,
// condition-only, and three-clause `for var i = 0; i < n; i += 1`), user-defined
// functions with calls, `return`, and recursion, tuple/struct/`data` construction
// and field/index access, `match` over every scrutinee kind, and explicit
// float→int rounding (`x.floor()`/`.ceil()`/`.round()`, a builtin method call —
// see rounding.go). Any other body form errors (the build fails loudly rather
// than emitting wrong code).
//
// Functions lower in two passes (Emit): every user function is declared before
// any body, so a call — from main, between functions, or recursive — resolves
// against l.funcs. main is emitted by lowerEntry (special i32 ABI); the rest by
// declareFunction/defineFunction. Each body gets fresh per-function state via
// beginFunction; params bind as entry-block allocas like `let`/`var`. Deferred
// with loud errors: void/multi-clause functions, default params, destructuring
// params, and higher-order (lambda-value) calls.
//
// Type declarations lower before any function, also in two passes
// (lowerTypeDeclarations then lowerTypeDefinitions): each `tuple`/`struct` decl
// becomes a named LLVM struct type. declareNamedStruct registers an empty
// placeholder for every decl first (keyed by its declared name in
// l.structTypes), then lowerTupleDef/lowerStructDef fill in the fields — so a
// field may reference another named type in any source order, forward references
// included (`struct Line { a: Point }` → `%Line = type { %Point, %Point }`).
// Fields lower by value for now (a `shared` field becomes a pointer-to-box once
// ALLOCATION.md's flavor lowering lands). A `data` decl lowers the same way to its
// tagged-union layout (lowerDataDef, DATA_LAYOUT.md); `newtype`/constrained decls
// still error loudly. Instances of these types — construction, field/index access,
// and `match` — all lower (aggregates.go, match.go, match_aggregate.go).
//
// Break/continue make a block terminate mid-stream, so lowering now follows a
// termination discipline: lowerBlockStmts stops at a sealed block and every
// fall-through `br` is guarded by `end.Term == nil`.
//
// # Where to build
//
// The lowering grows out from lowerEntry in roughly this order (see
// lyra/todo.md's backend section):
//
//  1. lowerType(t types.Type) — Lyra type → an llir `types.Type`. Scalars
//     (i8..i64/u* → iN, f16/32/64 → half/float/double, bool → i1) and named
//     tuple/struct references (→ the struct type the type-decl passes registered,
//     via lookupNamedType) lower; `data`/sum decls lower to their tagged union
//     { tag, payload-blob } per DATA_LAYOUT.md (lowerDataDef via DataUnionType) —
//     both the layout and values of that type (construction, `match`) lower.
//     A `string` lowers to a fat pointer { i8*, i64 } (STRING_LAYOUT.md).
//     `stack` values lower by value,
//     `shared` values to a pointer to a ref-counted box — see ALLOCATION.md. The
//     two docs compose: the sum-type layout is the payload; the flavor decides
//     inline vs boxed. layout.go provides the building blocks — LLVMPrimitive,
//     SharedBoxType, TagType, DataUnionType, SizeAndAlign — for lowerType to
//     dispatch over; runtime.go emits the ref-counted heap runtime (lyra_rc_*)
//     as real function bodies, lazily, the first time a value hits the heap.
//  2. What's next in lowerExpr: string interpolation (value→string formatting of
//     its segments). Already lowering: `print`/`println` (per-type output, print.go),
//     arithmetic and comparisons on ints and floats, string `==`/`!=` and `++`
//     (concatenation, heap-allocated via lyra_rc_alloc + memcpy), `&&`/`||`,
//     `if`/blocks, `let`/`var`, calls,
//     tuple/struct instances, `data` construction (alloca + typed payload store),
//     strings (fat pointer, STRING_LAYOUT.md), and `match` over every scrutinee
//     kind — `data` (tag switch), int/float/string scalar (comparison ladder),
//     and struct/tuple (shared aggregate ladder) — with nested struct/tuple/`data`
//     sub-patterns (aggPatternTest/aggPatternBind recurse, a nested data tag
//     becoming a comparison), value-testing payload sub-patterns (`Some(0)`), and
//     arm guards (both falling back from the tag switch to the shared ladder).
//     Floats: literals (context-inferred width, default f64), `fadd`/`fsub`/
//     `fmul`/`fdiv`, `frem`/floored-`frem`, `fneg`, `fcmp` (ordered except `!=`),
//     int→float/widening conversions, and params/returns. The `i64(x)`-style
//     conversion call still rejects float→int (lossy, no rounding mode implied);
//     the explicit escape hatch is `x.floor()`/`.ceil()`/`.round()` (rounding.go:
//     the matching `llvm.<op>.<width>` intrinsic + `fptosi` to a fixed i64 —
//     narrow further via `i32(x.floor())`).
//     Mutable locals are modeled as `alloca` + load/store (let mem2reg build SSA)
//     rather than hand-written phi nodes. Note lowerExpr returns the block control
//     ends in, not just a value — threaded so a branching form (`if`) can move the
//     insertion point; every non-branching case returns its own block unchanged.
//  3. Runtime: the ref-counted heap allocator exists (runtime.go —
//     lyra_rc_alloc/retain/release on libc malloc/free), and the ownership model
//     (pkg/analyzer/ownership + ownership_lower.go) frees managed strings —
//     retain on copy, transfer on return/own-arg, release at scope exit, with the
//     placement dominating its uses. Integer arithmetic is fully checked
//     (trap.go, Pit-of-Success #2): `+`/`-`/`*` via llvm.*.with.overflow,
//     `/`/`%`/`%%` against divide-by-zero and INT_MIN/-1, and `-INT_MIN` — each a
//     branch to a trap that reports and exit(101)s. The explicit escape hatches
//     `x.wrapping_{add,sub,mul}(y)` / `x.saturating_{add,sub,mul}(y)` lower too
//     (wrapping.go: wrapping = raw two's-complement, saturating add/sub =
//     llvm.{s,u}{add,sub}.sat, saturating mul = with.overflow + a select to the
//     bound). Still to come: strings-in-aggregates and break/continue paths (leak
//     conservatively today).
//
// # File organization
//
// The lowerer is one type whose methods are split across files by concern (all
// package llvm, so the grouping is purely for navigation):
//
//   - llvm.go — Backend/Emit entry point, the lowerer struct, the central lowerExpr dispatch
//   - lower_types.go — Lyra type → LLVM type; type-declaration lowering; resolve*Type helpers
//   - functions.go — user functions: entry, declare/define, params, return
//   - control_flow.go — blocks, if, for-loops, break/continue/return, var decls
//   - aggregates.go — tuple/struct/data construction + field/index access
//   - match.go — match dispatch, guards, and the scalar (int/float/string) if-else ladder
//   - match_aggregate.go — struct/tuple/data pattern matching (aggPattern* + shared ladder + data payloads)
//   - arithmetic.go — math ops, comparisons, &&/||, numeric conversions, width coercions
//   - strings.go — string fat-pointer helpers (literals as pinned boxes, equality via memcmp, ++ concatenation)
//   - shared.go — `shared` value boxing + the type-dispatching managed retain/release
//   - ownership_lower.go — scope-exit release of managed bindings (the managed-frame stack)
//   - rounding.go — builtin method calls (x.floor()/.ceil()/.round()) via lazily-declared LLVM intrinsics
//   - layout.go — llir type toolkit + SizeAndAlign
//   - runtime.go — the ref-counted heap runtime (lyra_rc_alloc/retain/release), emitted lazily
package llvm

import (
	"fmt"

	"github.com/llir/llvm/ir"
	"github.com/llir/llvm/ir/constant"
	lltypes "github.com/llir/llvm/ir/types"
	"github.com/llir/llvm/ir/value"

	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/backend"
	"github.com/Lyra-Language/lyra/pkg/driver"
	"github.com/Lyra-Language/lyra/pkg/types"
)

// Backend is the LLVM IR code generator.
type Backend struct{}

// New returns an LLVM backend.
func New() *Backend { return &Backend{} }

// Compile-time assertion that Backend satisfies the contract.
var _ backend.Backend = (*Backend)(nil)

// Name identifies the target.
func (*Backend) Name() string { return "llvm" }

// Emit lowers the program to LLVM IR text.
//
// SKELETON: only the entry-function shell is emitted, with a placeholder body.
// Replace lowerEntry's body with real lowering; grow the type/expression/
// statement lowering alongside it.
func (b *Backend) Emit(res *driver.Result, entry *driver.EntryPoint) ([]byte, error) {
	m, err := b.emitModule(res, entry)
	if err != nil {
		return nil, err
	}
	return []byte(m.String()), nil
}

// emitModule is Emit before serialization: it returns the built *ir.Module rather
// than its text. Emit is the Backend-interface entry point; this exists so callers
// that want to *analyze* the result (the conservation check in the tests, which
// walks the CFG looking for allocations that can reach a return unreleased) get the
// real structure instead of re-parsing the printed form.
func (b *Backend) emitModule(res *driver.Result, entry *driver.EntryPoint) (*ir.Module, error) {
	if res == nil || res.Program == nil || entry == nil {
		return nil, fmt.Errorf("llvm: nil program or entry point")
	}
	m := ir.NewModule()
	l := &lowerer{
		module:             m,
		res:                res,
		locals:             map[string]value.Value{},
		funcs:              map[string]*ir.Func{},
		funcParams:         map[string][]ast.Parameter{},
		byRefParams:        map[value.Value]bool{},
		consts:             map[string]*ast.VarDeclStmt{},
		structTypes:        map[string]*lltypes.StructType{},
		roundingIntrinsics: map[string]*ir.Func{},
		overflowIntrinsics: map[string]*ir.Func{},
		panics:             map[string]*ir.Func{},
		dropFns:            map[string]*ir.Func{},
		retainFns:          map[string]*ir.Func{},
		cStrings:           map[string]*ir.Global{},
		specialized:        map[string]*ir.Func{},
		specializedParams:  map[string][]ast.Parameter{},
		closures:           map[*ast.LambdaExpr]*ir.Func{},
		closureThunks:      map[string]*ir.Func{},
		envDropFns:         map[string]*ir.Func{},
	}
	// Record top-level `const` declarations so a reference to one inlines its
	// compile-time value (they aren't functions, so forEachUserFunction skips them).
	for _, stmt := range res.Program.Statements {
		if vd, ok := stmt.(*ast.VarDeclStmt); ok && vd.BindingKind == ast.BindingConst {
			l.consts[vd.Name] = vd
		}
	}
	// Lower type declarations
	if err := l.lowerTypeDeclarations(res.Program); err != nil {
		return nil, err
	}
	// Lower type definitions
	if err := l.lowerTypeDefinitions(res.Program); err != nil {
		return nil, err
	}
	// Two passes so a call — from main, between functions, or a recursive
	// self-call — can reference any function before its body exists: declare all
	// user functions, then lower main (whose body may call them), then lower the
	// user-function bodies.
	if err := l.forEachUserFunction(res.Program, entry.Lambda, l.declareFunction); err != nil {
		return nil, err
	}
	// Every *nested* lambda is lifted to a function of its own (closures.go), and
	// like the named ones they are all declared before any body so a creation site
	// can reference one. Their bodies are lowered last, after every enclosing
	// function — never re-entrantly at the creation site, which would mean saving
	// and restoring the whole per-function lowering state mid-expression.
	nested := collectNestedLambdas(res.Program, entry.Lambda)
	for _, fn := range nested {
		if err := l.declareClosure(fn); err != nil {
			return nil, err
		}
	}
	// One function per distinct instantiation of a generic one (monomorphize.go),
	// declared alongside the rest so a call resolves before any body exists.
	if err := l.declareSpecializations(); err != nil {
		return nil, err
	}
	if err := l.lowerEntry(entry); err != nil {
		return nil, err
	}
	if err := l.forEachUserFunction(res.Program, entry.Lambda, l.defineFunction); err != nil {
		return nil, err
	}
	for _, fn := range nested {
		if err := l.defineClosure(fn); err != nil {
			return nil, err
		}
	}
	if err := l.defineSpecializations(); err != nil {
		return nil, err
	}
	return m, nil
}

type lowerer struct {
	module      *ir.Module
	res         *driver.Result                 // gives you TypeTable, SymbolTable, MethodTable, …
	funcs       map[string]*ir.Func            // name → its function IR (all declared before any body)
	funcParams  map[string][]ast.Parameter     // name → its declared parameters (call sites need the `mut` by-ref modes)
	consts      map[string]*ast.VarDeclStmt    // top-level `const` name → its declaration (its value is inlined at each use)
	structTypes map[string]*lltypes.StructType // name → its struct type (for named tuple and struct lowering)
	strLitCount int                            // counter for unique string-literal global names
	memcmp      *ir.Func                       // libc memcmp, declared lazily on first string comparison
	memcpy      *ir.Func                       // libc memcpy, declared lazily on first string concatenation
	write       *ir.Func                       // libc write, declared lazily on first print/println
	snprintf    *ir.Func                       // libc snprintf, declared lazily on first numeric print
	fmtRune     *ir.Func                       // lyra_rune_to_utf8, defined lazily on first rune print
	utf8Decode  *ir.Func                       // lyra_utf8_decode, defined lazily on first string for-in
	fmtI128     *ir.Func                       // lyra_i128_to_str, defined lazily on first i128/u128 print
	newlineByte *ir.Global                     // interned "\n" byte, for println's trailing newline
	cStrings    map[string]*ir.Global          // interned NUL-terminated C strings (snprintf formats, bool text)

	// roundingIntrinsics caches lazily-declared llvm.{floor,ceil,round}.<width>
	// intrinsics (rounding.go), keyed by full intrinsic name.
	roundingIntrinsics map[string]*ir.Func

	// Checked-arithmetic traps (trap.go): overflowIntrinsics caches the lazily-
	// declared llvm.{s,u}{add,sub,mul}.with.overflow.iN and .sat.iN intrinsics by
	// full name; panics caches the noreturn trap functions by name (overflow,
	// divide-by-zero); exit is libc's.
	overflowIntrinsics map[string]*ir.Func
	panics             map[string]*ir.Func
	exit               *ir.Func

	// The ref-counted heap runtime (runtime.go), emitted lazily into the module
	// the first time a value needs the heap (today: string concatenation). nil
	// until ensureRCRuntime runs; all five are populated together.
	malloc      *ir.Func // libc malloc
	free        *ir.Func // libc free
	rcAlloc     *ir.Func // lyra_rc_alloc: malloc a box, rc = 1
	rcRetain    *ir.Func // lyra_rc_retain: rc += 1 (pinned no-op)
	rcRelease   *ir.Func // lyra_rc_release: rc -= 1, drop + free at 0 (pinned no-op)
	rcDropReuse *ir.Func // lyra_rc_drop_reuse: unique → return box (reclaim), else decref/null (Perceus reuse)
	// The weak half of the protocol: a weak reference keeps a box's *memory* alive
	// without keeping its value alive, so the counts are independent — the payload
	// dies at strong 0, the memory is freed at weak 0.
	rcWeakRetain  *ir.Func // lyra_rc_weak_retain: weak += 1
	rcWeakRelease *ir.Func // lyra_rc_weak_release: weak -= 1, free when both counts are 0
	rcUpgrade     *ir.Func // lyra_rc_upgrade: strong != 0 → strong += 1 and return the box, else null

	// Per-function state, reset by beginFunction at the start of each function
	// body (main and every user function get their own).
	locals    map[string]value.Value // name → its alloca (a pointer)
	loops     []loopCtx              // stack of enclosing loops; top is innermost
	retType   lltypes.Type           // the current function's LLVM return type
	retSigned bool                   // whether that return type is a signed integer
	entryABI  bool                   // true only for main (u8 body → i32 ABI slot)

	// Ownership bookkeeping for managed (ref-counted) values — strings today (see
	// pkg/analyzer/ownership and ALLOCATION.md). Both reset by beginFunction.
	//   - managedFrames is a stack of scope frames; each holds the allocas of the
	//     managed bindings declared in that scope, released at the scope's exit
	//     (frame[0] is the function root: `own` params).
	//   - pendingReleases are managed temporaries (owned values consumed in a
	//     borrowing position) awaiting release at the end of the current statement.
	//     Each remembers the block it was produced in, so its release is emitted
	//     there (dominating its uses) even when the statement spans branches — a
	//     temp built in an `&&` right-hand block, say, is freed in that block, not
	//     in the merge block it doesn't dominate.
	managedFrames   [][]managedSlot
	pendingReleases []pendingTemp
	// pendingBase is the index into pendingReleases below which temporaries belong
	// to an *enclosing* statement still being lowered. A nested block/branch lowered
	// mid-expression (a call argument, an `if`/`match` operand) must not flush those
	// — freeing an outer temp before the enclosing expression consumes it is a
	// use-after-free. Each statement-sequence scope raises the base to the current
	// length on entry and restores it on exit; a flush only ever touches [base:].
	pendingBase int

	// byRefParams holds the slots of this function's by-reference `mut` parameters
	// (functions.go, paramIsByRef). Such a slot is the *caller's* storage, handed in
	// as a pointer parameter rather than an entry-block alloca, which two things
	// need to know: releaseOldTarget (writing a managed field through it releases the
	// caller's reference, which is correct precisely because it is not a duplicate),
	// and any code that would otherwise assume every slot is an *ir.InstAlloca.
	// Reset by beginFunction.
	byRefParams map[value.Value]bool

	// Closures (closures.go). A function value is `{ i8* fn, i8* env }`, so every
	// lambda used as one is lifted to a top-level function taking its environment
	// as a leading parameter.
	//   - closures maps each nested lambda to its lifted function; all are declared
	//     before any body, like named functions, so a creation site resolves.
	//   - closureThunks caches the per-function adapter that lets a *named* function
	//     be used as a value without giving every direct call an env parameter.
	//   - emptyEnvPtr is the one pinned static environment every captureless
	//     function value shares; envDropFns caches the per-capture-set drop glue,
	//     reached at release time through the closureEnvDrop trampoline.
	// Monomorphization (monomorphize.go): one emitted function per distinct
	// instantiation of a generic one, keyed by the instantiation's stable Key().
	// typeSubst is the substitution installed while a specialization is being
	// lowered — the two type accessors (lowerType, recordedType) consult it, which
	// is what makes the shared body concrete without cloning any node.
	specialized       map[string]*ir.Func
	specializedParams map[string][]ast.Parameter
	typeSubst         map[string]types.Type

	closures       map[*ast.LambdaExpr]*ir.Func
	closureThunks  map[string]*ir.Func
	envDropFns     map[string]*ir.Func
	closureEnvDrop *ir.Func
	emptyEnvPtr    value.Value
	closureCount   int
	envDropCount   int

	// dropFns caches the per-type recursive drop glue (drop.go), keyed by the
	// payload type's String(). Module-level, not per-function: one @lyra_drop_T
	// serves every release of a T. dropFnCount only keeps the symbol names unique.
	dropFns     map[string]*ir.Func
	dropFnCount int

	// retainFns caches the per-type recursive *retain* glue (retain.go) the same
	// way — the mirror of dropFns, and what makes a copy of a stack aggregate a real
	// +1 on each managed field it duplicates (deep-retain-on-copy). The two caches
	// are separate but their generated bodies must cover the same fields.
	retainFns     map[string]*ir.Func
	retainFnCount int

	// reuseToken is the Perceus reuse token (an i8* box-or-null from
	// lyra_rc_drop_reuse) of the reuse-`match` currently being lowered, live only
	// across its arms. A reuse-target construction in an arm consumes it (writes into
	// the reclaimed box when non-null) and clears it; an arm that doesn't consume it
	// frees it. nil when no reuse match is in flight.
	reuseToken value.Value
}

// pendingTemp is a managed temporary awaiting release, tagged with the block it
// was produced in (where its release must go to stay dominated) and its Lyra type
// (which selects the drop glue for whatever its payload owns).
type pendingTemp struct {
	val   value.Value
	block *ir.Block
	ty    types.Type
}

// managedSlot is a managed binding's alloca paired with the Lyra type it holds.
// The type is carried because the release site needs it to pick the value's drop
// glue (drop.go) — the LLVM type alone can't say which fields of a `data` payload
// blob are managed.
type managedSlot struct {
	slot value.Value
	ty   types.Type
}

// lowerExpr lowers a Lyra expression to an LLVM value, appending any
// instructions it needs to block. It returns both the value and *the block
// control ends up in* — for a straight-line expression that's the same block
// it was given, but a branching form (an `if`) leaves control in a different
// (merge) block, and callers must keep lowering into that one. This is the
// Go-explicit version of what LLVM's C++ IRBuilder tracks as an implicit
// "current insertion point"; llir has no such hidden state, so we thread it.
//
// It returns an error (rather than emitting wrong code) for a form that isn't
// handled yet, so `lyrac build` fails loudly.
//
// Integer literals lower at the width the typechecker recorded for them
// (literalIntType) — context-directed literal-width inference pushes the
// surrounding width (an annotation, a concrete sibling operand, a declared
// return type) onto the literal, so `i8(x) < 3` lowers `3` as i8. A literal with
// no resolved context (e.g. an unannotated `let x = 5`) defaults to i64.
//
// lowerExpr wraps the dispatch (lowerExprDispatch) with the ownership actions the
// analysis pass computed for this expression node (pkg/analyzer/ownership): a
// borrowed managed value flowing into an owning position is retained here; an
// owned managed temporary flowing into a borrowing position is scheduled for
// release at the end of the enclosing statement (flushTempReleases). Both operate
// on the value in the block control *ends* in, and only on a real string value.
func (l *lowerer) lowerExpr(block *ir.Block, expr ast.Expression) (value.Value, *ir.Block, error) {
	v, end, err := l.lowerExprDispatch(block, expr)
	if err != nil {
		return nil, nil, err
	}
	// The gate is the *Lyra* type, not the LLVM one, and it is the deep question
	// ("does this value own any refcounted reference?") rather than "is this value
	// itself a box". A stack aggregate lowers to a plain struct/array with no pointer
	// of its own, yet copying one duplicates every managed field inside it — gating on
	// the LLVM type skipped exactly those copies, which is what left them unretained.
	ty, _ := l.recordedType(expr)
	if v != nil && l.needsDrop(ty) {
		if l.res.Ownership.ShouldRetain(expr) {
			// Deep: retain every managed reference reachable by value from v, which for
			// a managed value is just itself and for an aggregate is each managed field.
			if err := l.deepRetain(end, v, ty); err != nil {
				return nil, nil, err
			}
		}
		if l.res.Ownership.ShouldReleaseTemp(expr) {
			// Record the temporary's Lyra type alongside it: releasing it may free its
			// box, and freeing runs the drop glue for whatever the payload owns.
			l.pendingReleases = append(l.pendingReleases, pendingTemp{v, end, ty})
		}
		// An owning last use *transfers* the reference, so retire the slot from its
		// frame right at the move (stage-2 fusion — no scope-exit release). A
		// *borrowing* last use is a drop, but its release must follow the borrow, so
		// it isn't emitted here — dropLastUsesInStmt handles it after the statement.
		if id, ok := expr.(*ast.IdentifierExpr); ok {
			if transfer, isLast := l.res.Ownership.LastUse(expr); isLast && transfer {
				if slot, found := l.locals[id.Name]; found {
					l.retireManagedSlot(slot)
				}
			}
		}
	}
	return v, end, nil
}

// flushStmtTemps releases this statement scope's managed temporaries (those at or
// above pendingBase), then truncates the pending list back to pendingBase. The
// release block is chosen so it always follows every use of the temporary:
//   - a temp produced in the statement's *start* block (start) is released at `end`
//     (the block control ends in after the whole statement). The start block
//     dominates end, so the value is live there — this is what lets a temp used as
//     an *earlier* call argument survive until the call, even when a *later*
//     argument contains a branch that moves control into a different (merge) block
//     before the call. Releasing it in the start block (the old behavior) freed it
//     before the call → a use-after-free.
//   - a temp produced anywhere else — inside a branch of the expression (an
//     `&&`/`||` right operand, a match-arm body) — is released in its own block,
//     the only block guaranteed to have produced it and where it is consumed;
//     releasing it in `end` would touch an undefined value on the path the branch
//     didn't take.
// start/end nil (the flushTemps wrapper, used by early exits) releases everything at
// its production block — no temp's block equals nil, so the first case never fires.
func (l *lowerer) flushStmtTemps(start, end *ir.Block) error {
	for i := l.pendingBase; i < len(l.pendingReleases); i++ {
		p := l.pendingReleases[i]
		blk := p.block
		if p.block == start {
			blk = end
		}
		if err := l.deepRelease(blk, p.val, p.ty); err != nil {
			return err
		}
	}
	l.pendingReleases = l.pendingReleases[:l.pendingBase]
	return nil
}

// flushTemps releases this scope's pending temporaries at their production blocks —
// used at an early exit (break/continue/return), which has no single statement-end
// block to move them to.
func (l *lowerer) flushTemps() error { return l.flushStmtTemps(nil, nil) }

func (l *lowerer) lowerExprDispatch(block *ir.Block, expr ast.Expression) (value.Value, *ir.Block, error) {
	switch e := expr.(type) {
	case *ast.IntegerLiteralExpr:
		// An int literal the typechecker adapted to a float context (`let x: f64
		// = 5` — propagateLiteralType, or the annotation record in checkVarDecl)
		// is a float constant, not an i64 one.
		if ft, ok := l.literalRecordedFloatType(e); ok {
			v := float64(e.Value)
			if e.Unsigned {
				v = float64(uint64(e.Value))
			}
			return constant.NewFloat(ft, v), block, nil
		}
		return constant.NewInt(l.literalIntType(e), e.Value), block, nil
	case *ast.FloatLiteralExpr:
		return constant.NewFloat(l.literalFloatType(e), e.Value), block, nil
	case *ast.CharacterLiteralExpr:
		// A rune is a Unicode code point, represented as i32.
		return constant.NewInt(lltypes.I32, int64(e.Value)), block, nil
	case *ast.StringLiteralExpr:
		return l.lowerStringConstant(block, e.Value), block, nil
	case *ast.StringConcatExpr:
		return l.lowerStringConcat(block, e)
	case *ast.InterpolatedStringExpr:
		return l.lowerInterpolatedString(block, e)
	case *ast.BooleanLiteralExpr:
		bit := int64(0)
		if e.Value {
			bit = 1
		}
		return constant.NewInt(lltypes.I1, bit), block, nil
	case *ast.IdentifierExpr:
		if slot, ok := l.locals[e.Name]; ok {
			elem, err := slotElemType(slot)
			if err != nil {
				return nil, nil, err
			}
			return block.NewLoad(elem, slot), block, nil
		}
		// A reference to a top-level `const`: inline its value expression (a const is
		// a compile-time constant, immutable, with no storage of its own). The value
		// node carries the width the typechecker recorded for it, matching this use's
		// type. The optimizer folds any repeated constant computation.
		if cd, ok := l.consts[e.Name]; ok && cd.Value != nil {
			return l.lowerExpr(block, cd.Value)
		}
		// A top-level function named in value position (`apply(double, 3)`): build a
		// closure value over it. Its environment is the shared pinned empty one — a
		// named function captures nothing — so this costs no allocation.
		if _, ok := l.funcs[e.Name]; ok {
			v, err := l.namedFunctionValue(block, e.Name)
			return v, block, err
		}
		return nil, nil, fmt.Errorf("llvm: unbound identifier %q", e.Name)
	case *ast.BooleanBinaryOpExpr:
		return l.lowerBooleanBinaryOpExpr(block, e)
	case *ast.MathBinaryOpExpr:
		return l.lowerMathBinaryOpExpr(block, e)
	case *ast.MathAssignOpExpr:
		return l.lowerMathAssignOp(block, e)
	case *ast.NegationExpr:
		return l.lowerNegationExpr(block, e)
	case *ast.FunctionCallExpr:
		return l.lowerFunctionCallExpr(block, e)
	case *ast.BlockExpr:
		return l.lowerBlock(block, e)
	case *ast.IfExpr:
		return l.lowerIf(block, e)
	case *ast.ForLoopExpr:
		return l.lowerForLoop(block, e)
	case *ast.ForInLoopExpr:
		return l.lowerForInLoop(block, e)
	case *ast.TupleLiteralExpr:
		return l.lowerTupleLiteralExpr(block, e)
	case *ast.TupleIndexExpr:
		return l.lowerTupleIndexExpr(block, e)
	case *ast.ArrayLiteralExpr:
		return l.lowerArrayLiteralExpr(block, e)
	case *ast.IndexExpr:
		return l.lowerIndexExpr(block, e)
	case *ast.StructInstanceExpr:
		return l.lowerStructInstanceExpr(block, e)
	case *ast.MemberExpr:
		return l.lowerMemberExpr(block, e)
	case *ast.DataConstructorExpr:
		return l.lowerDataConstructorExpr(block, e)
	case *ast.MatchExpr:
		return l.lowerMatch(block, e)
	case *ast.LambdaExpr:
		return l.lowerLambdaExpr(block, e)
	}
	return nil, nil, fmt.Errorf("llvm: expression lowering not implemented for %T", expr)
}

// slotElemType returns the type of the value a local's slot holds.
//
// A slot is always a pointer, but not always an *ir.InstAlloca: a by-reference
// `mut` parameter's slot is the incoming pointer parameter itself — the caller's
// storage (functions.go, paramIsByRef). Both spellings carry the pointee in their
// LLVM type, so read it from there rather than asserting the alloca, which would
// panic on a by-ref parameter.
func slotElemType(slot value.Value) (lltypes.Type, error) {
	pt, ok := slot.Type().(*lltypes.PointerType)
	if !ok {
		return nil, fmt.Errorf("llvm: local slot is not a pointer (%s)", slot.Type())
	}
	return pt.ElemType, nil
}
