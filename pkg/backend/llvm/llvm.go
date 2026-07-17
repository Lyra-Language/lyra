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
//     inline vs boxed. layout.go/runtime.go provide the building blocks —
//     LLVMPrimitive, SharedBoxType, TagType, DataUnionType, SizeAndAlign, and
//     declareRuntime (wired into Emit) — for lowerType to dispatch over.
//  2. What's next in lowerExpr: string concatenation (`++`) and interpolation
//     (both build a new string → need a heap allocator, `lyra_rc_alloc` + memcpy)
//     and `print`. Already lowering: arithmetic and comparisons on ints and
//     floats, string `==`/`!=`, `&&`/`||`, `if`/blocks, `let`/`var`, calls,
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
//  3. Runtime: a heap allocator (string `++`/interpolation, dynamic values) and
//     `print` output; the overflow trap for todo #2 (via llvm.sadd.with.overflow)
//     and the builtin overflow-arithmetic methods (typechecker/builtins.go →
//     two's-complement +/-/* and llvm.{s,u}{add,sub}.sat).
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
//   - strings.go — string fat-pointer helpers (literals, equality via memcmp)
//   - rounding.go — builtin method calls (x.floor()/.ceil()/.round()) via lazily-declared LLVM intrinsics
//   - layout.go — llir type toolkit + SizeAndAlign; runtime.go — runtime shim declarations
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
	declareRuntime(m)
	l := &lowerer{
		module:             m,
		res:                res,
		locals:             map[string]value.Value{},
		funcs:              map[string]*ir.Func{},
		structTypes:        map[string]*lltypes.StructType{},
		roundingIntrinsics: map[string]*ir.Func{},
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

	// roundingIntrinsics caches lazily-declared llvm.{floor,ceil,round}.<width>
	// intrinsics (rounding.go), keyed by full intrinsic name.
	roundingIntrinsics map[string]*ir.Func

	// Per-function state, reset by beginFunction at the start of each function
	// body (main and every user function get their own).
	locals    map[string]value.Value // name → its alloca (a pointer)
	loops     []loopCtx              // stack of enclosing loops; top is innermost
	retType   lltypes.Type           // the current function's LLVM return type
	retSigned bool                   // whether that return type is a signed integer
	entryABI  bool                   // true only for main (u8 body → i32 ABI slot)
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
func (l *lowerer) lowerExpr(block *ir.Block, expr ast.Expression) (value.Value, *ir.Block, error) {
	switch e := expr.(type) {
	case *ast.IntegerLiteralExpr:
		return constant.NewInt(l.literalIntType(e), e.Value), block, nil
	case *ast.FloatLiteralExpr:
		return constant.NewFloat(l.literalFloatType(e), e.Value), block, nil
	case *ast.StringLiteralExpr:
		return l.lowerStringConstant(block, e.Value), block, nil
	case *ast.StringConcatExpr:
		return nil, nil, fmt.Errorf("llvm: string concatenation (`++`) not implemented yet (needs heap allocation)")
	case *ast.InterpolatedStringExpr:
		return nil, nil, fmt.Errorf("llvm: string interpolation not implemented yet (needs heap allocation)")
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
