// Package llvm is the LLVM IR backend for Lyra. It lowers a typed program from
// pkg/driver to LLVM IR (built with github.com/llir/llvm), which llc/clang then
// compiles.
//
// **README.md beside this file is the inventory** — what lowers, what does not, and
// the reasoning behind the parts that are subtle (the termination discipline, the
// diverging-operand guards, the ASan attribute the tests depend on, the binary cache).
// This comment deliberately does not repeat it. It used to: a ~120-line status list
// lived here and drifted until it was actively wrong, announcing default parameters,
// destructuring parameters, string interpolation and higher-order calls as "deferred
// with loud errors" long after each of them lowered, and describing Emit as a skeleton
// that emits a placeholder body. That is the same drift the workspace CLAUDE.md records
// against its own duplicated copy of the package map, with the same fix: one home for
// the inventory, and a pointer from everywhere else.
//
// Two invariants hold across every file here, and both are load-bearing:
//
//   - **A form that does not lower yet is a hard error, never a guess** — including
//     where that means repeating a check the front end already made. The backend is
//     allowed to refuse; it is not allowed to emit wrong code.
//   - **A well-typed program must never panic the backend.** A residual mismatch that
//     llir would panic on (an aggregate element width, a nil operand from a diverging
//     argument) is either reconciled or reported as an error.
//
// # File organization
//
// The lowerer is one type whose methods are split across files by concern (all package
// llvm, so the grouping is purely for navigation):
//
//	Entry and dispatch
//	  llvm.go             Backend/Emit, the lowerer struct, the central lowerExpr dispatch
//	  functions.go        user functions: declare/define, params, calls, return
//	  control_flow.go     blocks, if, for-loops, break/continue/return, var decls
//
//	Types and layout
//	  lower_types.go      Lyra type -> LLVM type; type-declaration lowering; resolveShape
//	  layout.go           llir type toolkit + SizeAndAlign
//	  type_identity.go    per-module type identity, the backend half
//	  intconst.go         integer bounds and constants at a given width
//
//	Values
//	  aggregates.go       tuple/struct/data construction + field/index access
//	  arrays.go           static arrays
//	  dynarray.go         dynamic arrays
//	  array_comp.go       array comprehensions
//	  strings.go          string fat pointers: literals, equality, ++ concatenation
//	  print.go            print/println, per-type formatting
//	  input.go            read_line: the stdin shim and its Maybe<string> result
//	  random.go           random_seed: the OS-entropy shim (the PRNG is in the prelude)
//	  clock.go            wall_clock_nanos: the clock_gettime shim
//	  tui.go              set_raw_mode / read_key / terminal_size: the terminal shims,
//	                      and the one file that consults the compiling host's OS
//	  arithmetic.go       math, comparisons, &&/||, numeric conversions, width coercions
//	  equality.go         structural == on aggregates: the per-type comparison glue
//	  rounding.go         x.floor()/.ceil()/.round() via LLVM intrinsics
//	  trap.go             checked arithmetic and the trap runtime; the diverged() guard
//	  wrapping.go         the wrapping_*/saturating_* escape hatches
//	  closures.go         boxed closures (dev tier) and indirect calls
//	  try.go              the ? operator
//
//	Patterns
//	  match.go            match dispatch and the scalar comparison ladder
//	  match_aggregate.go  struct/tuple/data patterns (aggPattern* + the shared ladder)
//	  match_array.go      array patterns
//	  destructuring.go    the destructuring statements
//	  lvalue.go           assignment targets, including managed ones
//
//	Generics and traits
//	  generic_types.go    resolveInstantiation: the choke point for parameterized types
//	  monomorphize.go     one emitted function per distinct instantiation
//	  traits.go           trait-impl methods and dispatch
//	  overloads.go        receiver-keyed overloading, the backend half
//
//	Memory
//	  shared.go           shared boxing + the type-dispatching managed retain/release
//	  retain.go           recursive retain glue
//	  drop.go             recursive drop glue
//	  ownership_lower.go  scope-exit release; the managed-frame stack
//	  weak.go             weak references as opaque box pointers
//	  dominators.go       dominator info over a finished function, for exit releases
//	  runtime.go          the ref-counted heap runtime, emitted lazily
//
//	Constraints
//	  constraint_check.go a newtype's `where` constraints, checked at run time
//	  regex_match.go      the DFA tables + driver a `pattern(...)` constraint matches with
package llvm

import (
	"fmt"
	"math/big"

	"github.com/llir/llvm/ir"
	"github.com/llir/llvm/ir/constant"
	lltypes "github.com/llir/llvm/ir/types"
	"github.com/llir/llvm/ir/value"

	"github.com/Lyra-Language/lyra/pkg/analyzer/ownership"
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
// It is called only after analysis is error-free and the entry point resolves
// (cmd/lyrac), so it may assume a well-typed program — which is what lets a form it
// does not lower be a hard error rather than a guess.
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
		overloads:          map[*ast.LambdaExpr]emitted{},
		byRefParams:        map[value.Value]bool{},
		consts:             map[string]*ast.VarDeclStmt{},
		globals:            map[string]*ir.Global{},
		structTypes:        map[string]*lltypes.StructType{},
		traitMethods:       map[string]*ir.Func{},
		roundingIntrinsics: map[string]*ir.Func{},
		overflowIntrinsics: map[string]*ir.Func{},
		panics:             map[string]*ir.Func{},
		dropFns:            map[string]*ir.Func{},
		eqFns:              map[string]*ir.Func{},
		retainFns:          map[string]*ir.Func{},
		cStrings:           map[string]*ir.Global{},
		fmtFloat:           map[string]*ir.Func{},
		regexTables:        map[string]*regexTables{},
		specialized:        map[string]*ir.Func{},
		specializedParams:  map[string][]ast.Parameter{},
		closures:           map[*ast.LambdaExpr]*ir.Func{},
		closureThunks:      map[string]*ir.Func{},
		envDropFns:         map[string]*ir.Func{},
		externs:            map[string]externDecl{},
	}
	// Record top-level `const` declarations so a reference to one inlines its
	// compile-time value (they aren't functions, so forEachUserFunction skips them).
	//
	// A top-level `let`/`var` holding *data* is collected alongside them, and needs
	// storage rather than inlining. Until 08/14 nothing collected it at all: the
	// declaration type-checked, forEachUserFunction skipped it for not being a function,
	// and every reference died as `llvm: unbound identifier` — hazard 5 inverted, and
	// the reason `const` was the only way to give a module a string.
	for _, stmt := range res.Program.Statements {
		vd, ok := stmt.(*ast.VarDeclStmt)
		if !ok {
			continue
		}
		if vd.BindingKind == ast.BindingConst {
			l.consts[vd.Name] = vd
			continue
		}
		if _, isFunc := vd.Value.(*ast.LambdaExpr); !isFunc && vd.Value != nil {
			l.globalDecls = append(l.globalDecls, vd)
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
	// Foreign functions first, so a user function's body can call one — they are
	// declarations either way, and an extern has no body to lower afterwards.
	if err := l.declareExterns(res.Program); err != nil {
		return nil, err
	}
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
	// Declared before main so its initializers — and every function body — can name
	// one; *initialized* at the top of main, in declaration order.
	if err := l.declareGlobals(); err != nil {
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
	// Last, because everything above can queue one: a trait method is emitted on first
	// call, and a specialization or closure body may contain that first call. The loop
	// inside also handles a method whose own body calls another method.
	if err := l.definePendingTraitMethods(); err != nil {
		return nil, err
	}
	return m, nil
}

type lowerer struct {
	module     *ir.Module
	res        *driver.Result              // gives you TypeTable, SymbolTable, MethodTable, …
	funcs      map[string]*ir.Func         // name → its function IR (all declared before any body)
	funcParams map[string][]ast.Parameter  // name → its declared parameters (call sites need the `mut` by-ref modes)
	overloads  map[*ast.LambdaExpr]emitted // receiver-keyed overloads, by declaration (see overloads.go)
	consts     map[string]*ast.VarDeclStmt // top-level `const` name → its declaration (its value is inlined at each use)
	// globals are top-level `let`/`var` bindings whose value is *not* a function —
	// module-level data. Unlike a `const` they have storage, because their value is
	// computed at run time (a string box, an array, a call); unlike a local they outlive
	// every function. The slice preserves declaration order, which is the order they are
	// initialized in.
	globals             map[string]*ir.Global
	globalDecls         []*ast.VarDeclStmt
	traitMethods        map[string]*ir.Func            // emitted trait-impl methods, keyed by mangled symbol
	pendingTraitMethods []pendingTraitMethod           // declared, body not yet lowered (see traits.go)
	structTypes         map[string]*lltypes.StructType // type key → its struct type (for named tuple and struct lowering)

	// currentLoc is a position inside the item being lowered, and is what resolves a
	// named type reference to a declaration — see type_identity.go. Set per top-level
	// item (a type definition, a function body) rather than threaded, because the
	// resolved types reaching lowerType carry no location of their own.
	currentLoc      ast.Location
	strLitCount     int                     // counter for unique string-literal global names
	memcmp          *ir.Func                // libc memcmp, declared lazily on first string comparison
	memcpy          *ir.Func                // libc memcpy, declared lazily on first string concatenation
	write           *ir.Func                // libc write, declared lazily on first print/println
	snprintf        *ir.Func                // libc snprintf, declared lazily on first numeric print
	fmtRune         *ir.Func                // lyra_rune_to_utf8, defined lazily on first rune print
	utf8Decode      *ir.Func                // lyra_utf8_decode, defined lazily on first string for-in
	strRuneOffset   *ir.Func                // lyra_str_rune_offset, defined lazily on first string index/slice
	utf8Count       *ir.Func                // lyra_utf8_count, defined lazily (read_line / interpolation rune counts)
	realloc         *ir.Func                // libc realloc, declared lazily on first dynamic-array push
	fmtI128         *ir.Func                // lyra_i128_to_str, defined lazily on first i128/u128 print
	strtod          *ir.Func                // libc strtod, declared lazily (the float formatter's round-trip check)
	fmod            *ir.Func                // libc fmod, declared lazily (a float `step(...)` constraint check)
	regexMatch      *ir.Func                // lyra_regex_match, the shared DFA driver (regex_match.go)
	regexTables     map[string]*regexTables // compiled pattern -> its constant tables, one per distinct pattern
	fmtFloat        map[string]*ir.Func     // lyra_f{16,32,64}_to_str, one per printed float width
	mulOverflowI128 *ir.Func                // lyra_i128_mul_overflow, defined lazily (compiler-rt's __muloti4 is not linkable on Linux)
	newlineByte     *ir.Global              // interned "\n" byte, for println's trailing newline
	cStrings        map[string]*ir.Global   // interned NUL-terminated C strings (snprintf formats, bool text)

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
	malloc     *ir.Func // libc malloc
	free       *ir.Func // libc free
	readLine   *ir.Func // lyra_read_line: one line of stdin into a fresh box (input.go)
	randomSeed *ir.Func // lyra_random_seed: one word of OS entropy (random.go)
	wallClock  *ir.Func // lyra_wall_clock_nanos: nanoseconds since the epoch (clock.go)
	// The terminal (tui.go). Three builtins, each a libc call with no Lyra spelling;
	// everything layered on them is prelude code.
	setRawMode   *ir.Func // lyra_set_raw_mode: tcsetattr via cfmakeraw, original saved
	readKey      *ir.Func // lyra_read_key: one code point from stdin, Maybe<rune>
	terminalSize *ir.Func // lyra_terminal_size: TIOCGWINSZ, (columns, rows)
	waitForKey   *ir.Func // lyra_wait_for_key_ms: poll(stdin) with a timeout
	rcAlloc      *ir.Func // lyra_rc_alloc: malloc a box, rc = 1
	rcRetain     *ir.Func // lyra_rc_retain: rc += 1 (pinned no-op)
	rcRelease    *ir.Func // lyra_rc_release: rc -= 1, drop + free at 0 (pinned no-op)
	rcDropReuse  *ir.Func // lyra_rc_drop_reuse: unique → return box (reclaim), else decref/null (Perceus reuse)
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
	retLyra   types.Type             // the same return type, unlowered — `?` rebuilds Err/None at it (try.go)
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

	// exitReleases are releases a `break`/`continue` owes but cannot emit yet. Such a
	// jump leaves every statement between it and the loop without reaching their
	// flushes, so it must release their pending temporaries itself — but only the ones
	// live where it stands, which is a question about dominance, and the CFG is not
	// finished while the jump is being lowered. Each entry records the jumping block
	// and a *copy* of the temporaries in question (the pending list is truncated as
	// statements finish, so a slice of it would dangle). resolveExitReleases settles
	// them once the body is complete. Reset by beginFunction.
	exitReleases []exitRelease

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
	// specOwnership is the ownership table computed for the instantiation currently
	// being lowered — see the `ownership()` accessor, which every read goes through.
	specOwnership *ownership.Table

	// externs is every foreign function declared so far, keyed by the C symbol — which
	// is what a `declare` is unique by, so two modules naming one library function share
	// this entry rather than emitting two `declare`s of one name (extern.go).
	externs map[string]externDecl

	closures       map[*ast.LambdaExpr]*ir.Func
	closureThunks  map[string]*ir.Func
	envDropFns     map[string]*ir.Func
	closureEnvDrop *ir.Func
	emptyEnvPtr    value.Value
	closureCount   int
	envDropCount   int

	// eqFns caches the per-type structural equality glue (equality.go), keyed the same
	// way dropFns is — on the base type, so a newtype shares its base's function.
	eqFns     map[string]*ir.Func
	eqFnCount int
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
	// A newtype's `where` constraints, checked here because this is where the value
	// exists. The typechecker recorded only the sites it could *not* settle
	// statically (constraint_check.go), so a literal costs nothing and a runtime
	// value costs one compare-and-branch — the second rung of the same ladder
	// arithmetic overflow and range steps already ride.
	if v != nil {
		if ct, needed := l.res.ConstraintChecks.Get(expr); needed {
			if end, err = l.emitConstraintChecks(end, v, ct); err != nil {
				return nil, nil, err
			}
		}
	}
	// The gate is the *Lyra* type, not the LLVM one, and it is the deep question
	// ("does this value own any refcounted reference?") rather than "is this value
	// itself a box". A stack aggregate lowers to a plain struct/array with no pointer
	// of its own, yet copying one duplicates every managed field inside it — gating on
	// the LLVM type skipped exactly those copies, which is what left them unretained.
	ty, _ := l.recordedType(expr)
	if v != nil && l.needsDrop(ty) {
		if l.ownership().ShouldRetain(expr) {
			// Deep: retain every managed reference reachable by value from v, which for
			// a managed value is just itself and for an aggregate is each managed field.
			if err := l.deepRetain(end, v, ty); err != nil {
				return nil, nil, err
			}
		}
		if l.ownership().ShouldReleaseTemp(expr) {
			// Record the temporary's Lyra type alongside it: releasing it may free its
			// box, and freeing runs the drop glue for whatever the payload owns.
			l.pendingReleases = append(l.pendingReleases, pendingTemp{v, end, ty})
		}
		// An owning last use *transfers* the reference, so retire the slot from its
		// frame right at the move (stage-2 fusion — no scope-exit release). A
		// *borrowing* last use is a drop, but its release must follow the borrow, so
		// it isn't emitted here — dropLastUsesInStmt handles it after the statement.
		if id, ok := expr.(*ast.IdentifierExpr); ok {
			if transfer, isLast := l.ownership().LastUse(expr); isLast && transfer {
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
// release block is chosen so it always follows every use of the temporary, and the
// question that decides it is **dominance**: a temp is released at `end` (the block
// control ends in after the whole statement) exactly when its production block
// dominates `end`, and in its own block otherwise.
//
//   - **Dominating.** The value is defined on every path that reaches `end`, so it is
//     live there. This is what lets a temp used as an *earlier* call argument survive
//     until the call even when a *later* argument branches, moving control into a
//     different block before the call. Releasing it at its production block freed it
//     before the call → a use-after-free.
//   - **Not dominating** — produced inside a branch of the expression (an `&&`/`||`
//     right operand, a match-arm body). Its own block is the only one guaranteed to
//     have produced it; releasing it at `end` would touch an undefined value on the
//     path the branch didn't take.
//
// **It used to ask `p.block == start` instead**, which is the same test only while
// "not the start block" means "conditional". That stopped being true when lowerings
// that branch *unconditionally* arrived — `slice`, `read_line`, `<=>` — each of which
// ends in a continuation block every path reaches. Their temps were released at that
// continuation block, i.e. before the rest of the statement ran, so two `slice` calls
// in one expression freed the first result before allocating the second and the second
// allocation landed on the freed bytes: `"${s.slice(0,2)} ${s.slice(2,4)}"` printed
// `cd cd` (08/07). A proxy for the real question held right up until a new kind of
// block existed, and then failed silently.
//
// Computing dominance *here* — against a CFG the rest of the function has not been
// built into yet — is sound for the reason it is not sound for `resolveExitReleases`
// (see dominators.go): `end` is the block lowering *continues into*, so it never
// becomes the target of a later edge. Nothing added afterwards can create a path to
// `end` that bypasses a block dominating it today. The tree is built lazily, only when
// some temp was produced in a third block, which is the uncommon case.
//
// start/end nil (the flushTemps wrapper, used by early exits) releases everything at
// its production block — there is no statement-end block to move anything to.
func (l *lowerer) flushStmtTemps(start, end *ir.Block) error {
	var dt *domTree
	for i := l.pendingBase; i < len(l.pendingReleases); i++ {
		p := l.pendingReleases[i]
		blk := p.block
		switch {
		case end == nil || p.block == end:
			// Nowhere to move it to, or already there.
		case p.block == start:
			// The common case, and one dominance would also answer — kept as a test so
			// the ordinary statement never builds a tree.
			blk = end
		default:
			if dt == nil {
				dt = newDomTree(end.Parent)
			}
			if dt.dominates(p.block, end) {
				blk = end
			}
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

// exitRelease is one `break`/`continue`'s deferred obligation: the temporaries the
// jump skipped past, and the block it jumps from.
type exitRelease struct {
	block *ir.Block
	temps []pendingTemp
}

// recordExitReleases notes that the jump in block owes releases for the temporaries
// the loop's statements have pending — those recorded at or above the loop's own
// base. The base matters: a temporary belonging to a statement *outside* the loop is
// still reached by that statement's flush after the loop exits, so releasing it here
// as well would be a double free. This is the temporary-side mirror of
// releaseManagedFramesFrom's frameDepth.
//
// The temporaries are copied because pendingReleases is truncated as statements
// complete; the entries must outlive that.
func (l *lowerer) recordExitReleases(block *ir.Block, tempBase int) {
	if tempBase >= len(l.pendingReleases) {
		return
	}
	temps := make([]pendingTemp, len(l.pendingReleases)-tempBase)
	copy(temps, l.pendingReleases[tempBase:])
	l.exitReleases = append(l.exitReleases, exitRelease{block: block, temps: temps})
}

// resolveExitReleases emits the releases recorded by break/continue, now that fn is
// complete and its dominator tree is therefore final. A temporary is released only if
// the block that produced it dominates the jumping block — otherwise the jumped-from
// path may never have produced the value, and freeing it would be a double free
// rather than the leak that skipping it costs. See dominators.go.
//
// The releases land before each jump's terminator (llir prints a block's instructions
// ahead of its terminator), and each is straight-line, so no block is split.
func (l *lowerer) resolveExitReleases(fn *ir.Func) error {
	if len(l.exitReleases) == 0 {
		return nil
	}
	dt := newDomTree(fn)
	for _, ex := range l.exitReleases {
		for _, p := range ex.temps {
			if !dt.dominates(p.block, ex.block) {
				continue
			}
			if err := l.deepRelease(ex.block, p.val, p.ty); err != nil {
				return err
			}
		}
	}
	l.exitReleases = nil
	return nil
}

// releaseTempsOnExit releases, *in* exit, the pending temporaries that are dead on a
// path leaving the statement early — a `?` propagating, and eventually break/continue.
//
// It differs from flushStmtTemps in the one way that matters here: **it does not
// truncate the pending list.** An early exit is one path out of a statement that
// still has another, and that other path reaches the statement's own flush, which
// must still release these. Truncating would release them here and nowhere else,
// leaking on every non-exiting path. So this emits releases and leaves the list
// alone; each path ends up releasing exactly once.
//
// `from` is the block the exiting branch was emitted in. Only temporaries produced
// *there* are released, because only that block is known to dominate exit — it is
// exit's predecessor. A temporary produced inside a conditional sub-expression (an
// `&&` right operand, a match-arm body) is not dominated, and releasing it here
// would touch a value undefined on the path that branch did not take; those are
// skipped and leak, which is the conservative direction (never a double free).
// Closing that residue needs real dominance information, not a wider block test.
//
// skip is a value whose reference *transfers* out on this path (the `?` operand,
// whose reference moves into the rebuilt error) and so must not be released.
func (l *lowerer) releaseTempsOnExit(exit, from *ir.Block, skip value.Value) error {
	for i := l.pendingBase; i < len(l.pendingReleases); i++ {
		p := l.pendingReleases[i]
		if p.block != from || (skip != nil && p.val == skip) {
			continue
		}
		if err := l.deepRelease(exit, p.val, p.ty); err != nil {
			return err
		}
	}
	return nil
}

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
			if e.IsWide() {
				v, _ = new(big.Float).SetInt(e.BigValue()).Float64()
			}
			return floatConst(ft, v), block, nil
		}
		// A **wide** literal (>64 bits, so i128/u128 only) carries its magnitude as a
		// big.Int; `Value` is 0 and means nothing. llir's constant.Int is a big.Int
		// underneath, so the constant is exact — set it rather than going through the
		// int64 constructor, which is what silently emitted 0.
		if e.IsWide() {
			c := constant.NewInt(l.literalIntType(e), 0)
			c.X = e.BigValue()
			return c, block, nil
		}
		return constant.NewInt(l.literalIntType(e), e.Value), block, nil
	case *ast.FloatLiteralExpr:
		return floatConst(l.literalFloatType(e), e.Value), block, nil
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
		if slot, ok := l.slotFor(e.Name); ok {
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
		if key := l.funcKey(e.Name, e.GetLocation()); l.funcs[key] != nil {
			v, err := l.namedFunctionValue(block, key)
			return v, block, err
		}
		return nil, nil, fmt.Errorf("llvm: unbound identifier %q", e.Name)
	case *ast.BooleanBinaryOpExpr:
		return l.lowerBooleanBinaryOpExpr(block, e)
	case *ast.NotBooleanExpr:
		return l.lowerNotBooleanExpr(block, e)
	case *ast.MathBinaryOpExpr:
		return l.lowerMathBinaryOpExpr(block, e)
	case *ast.MathAssignOpExpr:
		return l.lowerMathAssignOp(block, e)
	case *ast.NegationExpr:
		return l.lowerNegationExpr(block, e)
	case *ast.BitwiseNotExpr:
		return l.lowerBitwiseNotExpr(block, e)
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
	case *ast.ArrayRepeatExpr:
		return l.lowerArrayRepeatExpr(block, e)
	case *ast.ArrayCompExpr:
		return l.lowerArrayComp(block, e)
	case *ast.IndexExpr:
		return l.lowerIndexExpr(block, e)
	case *ast.AnonymousStructInstanceExpr:
		return l.lowerAnonymousStructInstanceExpr(block, e)
	case *ast.StructInstanceExpr:
		return l.lowerStructInstanceExpr(block, e)
	case *ast.MemberExpr:
		return l.lowerMemberExpr(block, e)
	case *ast.DataConstructorExpr:
		return l.lowerDataConstructorExpr(block, e)
	case *ast.MatchExpr:
		return l.lowerMatch(block, e)
	case *ast.TryExpr:
		return l.lowerTryExpr(block, e)
	case *ast.NullCoalescingExpr:
		return l.lowerNullCoalescing(block, e)
	case *ast.LambdaExpr:
		return l.lowerLambdaExpr(block, e)
	case *ast.AddressOfExpr:
		return l.lowerAddressOf(block, e)
	case *ast.DerefExpr:
		return l.lowerDeref(block, e)
	case *ast.UnsafeBlockExpr:
		return l.lowerUnsafeBlock(block, e)
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
// slotFor answers "where does this name's storage live?" — the local first, then the
// module-level slot a top-level `let`/`var` holding data gets.
//
// **One lookup rather than four.** Reading an identifier, assigning to one, reassigning a
// whole binding and compound-assigning to one each asked the question separately, and each
// answered it with `l.locals` alone — so adding globals meant the same three lines in four
// files, which is the drift hazard 8 exists to name. The ordering is the part that must not
// vary: a local of the same name shadows the global, and a fourth site quietly disagreeing
// about that would be a wrong *value*, not an error.
func (l *lowerer) slotFor(name string) (value.Value, bool) {
	if slot, ok := l.locals[name]; ok {
		return slot, true
	}
	if g, ok := l.globals[name]; ok {
		return g, true
	}
	return nil, false
}

func slotElemType(slot value.Value) (lltypes.Type, error) {
	pt, ok := slot.Type().(*lltypes.PointerType)
	if !ok {
		return nil, fmt.Errorf("llvm: local slot is not a pointer (%s)", slot.Type())
	}
	return pt.ElemType, nil
}

// declareGlobals emits one module-level slot per top-level `let`/`var` holding data,
// zero-initialized. They are *filled* at the top of main (initGlobals), because their
// values are computed at run time — a string is a ref-counted box, an array is an
// allocation, and neither is an LLVM constant.
//
// Declared before main and before any function body so either can name one: a global's
// own initializer may call a top-level function, and a function may read a global, and
// neither ordering can be resolved by emitting them lazily.
func (l *lowerer) declareGlobals() error {
	for _, vd := range l.globalDecls {
		t, ok := l.recordedType(vd.Value)
		if !ok {
			return fmt.Errorf("llvm: no type recorded for top-level %q", vd.Name)
		}
		ty, err := l.lowerType(t)
		if err != nil {
			return fmt.Errorf("llvm: top-level %q: %w", vd.Name, err)
		}
		g := l.module.NewGlobalDef("lyra_global_"+vd.Name, constant.NewZeroInitializer(ty))
		l.globals[vd.Name] = g
	}
	return nil
}

// initGlobals emits the stores that fill those slots, in declaration order, and returns
// the block to carry on in.
//
// **Declaration order is the initialization order**, which is what makes a global
// referring to an earlier one work and a forward reference not. That is the same rule the
// use-before-declaration checker already enforces on the way in, so the ordering here is
// not a second policy — it is the first one, honoured.
//
// A managed value stored here is owned by the global for the life of the program and is
// never released: there is no scope for it to leave. That is a deliberate leak of a fixed
// amount, the same trade every language makes for module-level data, and it is why the
// ownership pass is not consulted — it reasons about scopes, and a global has none.
func (l *lowerer) initGlobals(block *ir.Block) (*ir.Block, error) {
	for _, vd := range l.globalDecls {
		g, ok := l.globals[vd.Name]
		if !ok {
			continue
		}
		v, next, err := l.lowerExpr(block, vd.Value)
		if err != nil {
			return nil, fmt.Errorf("llvm: top-level %q: %w", vd.Name, err)
		}
		block = next
		if diverged(v, block) {
			return block, nil
		}
		block.NewStore(v, g)
	}
	return block, nil
}
