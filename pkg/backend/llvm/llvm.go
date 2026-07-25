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
	if res == nil || res.Program == nil || entry == nil {
		return nil, fmt.Errorf("llvm: nil program or entry point")
	}
	m := ir.NewModule()
	l := &lowerer{
		module:             m,
		res:                res,
		locals:             map[string]value.Value{},
		funcs:              map[string]*ir.Func{},
		structTypes:        map[string]*lltypes.StructType{},
		roundingIntrinsics: map[string]*ir.Func{},
		overflowIntrinsics: map[string]*ir.Func{},
		panics:             map[string]*ir.Func{},
		dropFns:            map[string]*ir.Func{},
		cStrings:           map[string]*ir.Global{},
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
	if err := l.lowerEntry(entry); err != nil {
		return nil, err
	}
	if err := l.forEachUserFunction(res.Program, entry.Lambda, l.defineFunction); err != nil {
		return nil, err
	}
	return []byte(m.String()), nil
}

type lowerer struct {
	module      *ir.Module
	res         *driver.Result                 // gives you TypeTable, SymbolTable, MethodTable, …
	funcs       map[string]*ir.Func            // name → its function IR (all declared before any body)
	structTypes map[string]*lltypes.StructType // name → its struct type (for named tuple and struct lowering)
	strLitCount int                            // counter for unique string-literal global names
	memcmp      *ir.Func                       // libc memcmp, declared lazily on first string comparison
	memcpy      *ir.Func                       // libc memcpy, declared lazily on first string concatenation
	write       *ir.Func                       // libc write, declared lazily on first print/println
	snprintf    *ir.Func                       // libc snprintf, declared lazily on first numeric print
	fmtRune     *ir.Func                       // lyra_rune_to_utf8, defined lazily on first rune print
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

	// dropFns caches the per-type recursive drop glue (drop.go), keyed by the
	// payload type's String(). Module-level, not per-function: one @lyra_drop_T
	// serves every release of a T. dropFnCount only keeps the symbol names unique.
	dropFns     map[string]*ir.Func
	dropFnCount int

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
	if v != nil && isManagedLLVMType(v.Type()) {
		if l.res.Ownership.ShouldRetain(expr) {
			l.lowerManagedRetain(end, v)
		}
		if l.res.Ownership.ShouldReleaseTemp(expr) {
			// Record the temporary's Lyra type alongside it: releasing it may free its
			// box, and freeing runs the drop glue for whatever the payload owns.
			ty, _ := l.res.TypeTable.Get(expr)
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

// flushTemps releases every managed temporary awaiting release, each in the block
// it was produced in (so the release dominates the value's uses even across
// branches), then clears the pending list.
func (l *lowerer) flushTemps() error {
	for _, p := range l.pendingReleases {
		if err := l.lowerManagedRelease(p.block, p.val, p.ty); err != nil {
			return err
		}
	}
	l.pendingReleases = nil
	return nil
}

func (l *lowerer) lowerExprDispatch(block *ir.Block, expr ast.Expression) (value.Value, *ir.Block, error) {
	switch e := expr.(type) {
	case *ast.IntegerLiteralExpr:
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
		slot, ok := l.locals[e.Name]
		if !ok {
			return nil, nil, fmt.Errorf("llvm: unbound identifier %q", e.Name)
		}
		ptr := slot.(*ir.InstAlloca)
		return block.NewLoad(ptr.ElemType, slot), block, nil
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
	case *ast.TupleLiteralExpr:
		return l.lowerTupleLiteralExpr(block, e)
	case *ast.TupleIndexExpr:
		return l.lowerTupleIndexExpr(block, e)
	case *ast.StructInstanceExpr:
		return l.lowerStructInstanceExpr(block, e)
	case *ast.MemberExpr:
		return l.lowerMemberExpr(block, e)
	case *ast.DataConstructorExpr:
		return l.lowerDataConstructorExpr(block, e)
	case *ast.MatchExpr:
		return l.lowerMatch(block, e)
	}
	return nil, nil, fmt.Errorf("llvm: expression lowering not implemented for %T", expr)
}
