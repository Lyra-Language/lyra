// Package llvm is the (in-progress) LLVM IR backend for Lyra. It lowers a typed
// program from pkg/driver to LLVM IR (built with github.com/llir/llvm), which
// llc/clang then compiles.
//
// # Status: early
//
// Emit defines `@main` and lowers its body via lowerExpr, which so far handles
// integer/bool literals, arithmetic (`+ - * / % %% -(unary)`, incl. Odin-style
// floored `%%` vs truncated `%`), int-to-int numeric conversions (`i8(x)`,
// `u32(x)`, …), integer comparisons (`< <= > >= == !=` → icmp), short-circuit
// `&&`/`||` (cond-br + phi), blocks (value = last expression), `if`/`else`
// (cond-br + phi diamond; one-armed `if` as a statement), `let`/`var` bindings,
// reassignment and compound assignment (`i += 1`), `for` loops with
// `break`/`continue` (cond/body/post/exit CFG; all forms — infinite,
// condition-only, and three-clause `for var i = 0; i < n; i += 1`), and
// user-defined functions with calls, `return`, and recursion. Any other body
// form errors (the build fails loudly rather than emitting wrong code).
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
// ALLOCATION.md's flavor lowering lands). data/newtype/constrained decls error
// loudly. Instances of these types (construction, field access) aren't lowered
// yet — only the type shapes.
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
//     via lookupNamedType) already lower; remaining is data/sum → a tagged union
//     { tag, payload } per DATA_LAYOUT.md. `stack` values lower by value,
//     `shared` values to a pointer to a ref-counted box — see ALLOCATION.md. The
//     two docs compose: the sum-type layout is the payload; the flavor decides
//     inline vs boxed. layout.go/runtime.go provide the building blocks —
//     LLVMPrimitive, SharedBoxType, TagType, DataUnionType, SizeAndAlign, and
//     declareRuntime (wired into Emit) — for lowerType to dispatch over.
//  2. Grow lowerExpr further: `match`, struct construction, and struct field
//     access (arithmetic, comparisons, `&&`/`||`, `if`/blocks, `let`/`var`,
//     calls, and tuple instances — construction + positional `.0` access via
//     insertvalue/extractvalue — already lower). Mutable locals are modeled as `alloca` + load/store
//     (let mem2reg build SSA) rather than hand-written phi nodes. Float
//     literals/arithmetic and any conversion touching float are deferred: a float
//     literal is now reachable via a `let x: f64 = …` binding, but it errors
//     loudly (FloatLiteralExpr is unhandled) rather than miscompiling — no float
//     value can reach the u8 exit code until float→int lowering exists; see
//     lowerNumericConversion's doc comment. Note lowerExpr returns the block
//     control ends in, not just a value — threaded so a branching form (`if`)
//     can move the insertion point; every non-branching case returns its own
//     block unchanged.
//  3. Runtime shims: print, and the overflow trap for todo #2 (via
//     llvm.sadd.with.overflow); the builtin overflow-arithmetic methods
//     (typechecker/builtins.go) lower to two's-complement +/-/* and
//     llvm.{s,u}{add,sub}.sat.
package llvm

import (
	"fmt"
	"slices"

	"github.com/llir/llvm/ir"
	"github.com/llir/llvm/ir/constant"
	"github.com/llir/llvm/ir/enum"
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
	declareRuntime(m)
	l := &lowerer{
		module:      m,
		res:         res,
		locals:      map[string]value.Value{},
		funcs:       map[string]*ir.Func{},
		structTypes: map[string]*lltypes.StructType{},
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

	// Per-function state, reset by beginFunction at the start of each function
	// body (main and every user function get their own).
	locals    map[string]value.Value // name → its alloca (a pointer)
	loops     []loopCtx              // stack of enclosing loops; top is innermost
	retType   lltypes.Type           // the current function's LLVM return type
	retSigned bool                   // whether that return type is a signed integer
	entryABI  bool                   // true only for main (u8 body → i32 ABI slot)
}

func (l *lowerer) lowerTypeDeclarations(program *ast.Program) error {
	for _, statement := range program.Statements {
		if typeDeclStmt, ok := statement.(*ast.TypeDeclStmt); ok {
			if err := l.lowerTypeDecl(typeDeclStmt); err != nil {
				return err
			}
		}
	}
	return nil
}

func (l *lowerer) lowerTypeDecl(typeDeclStmt *ast.TypeDeclStmt) error {
	if typeDeclStmt.Name == "?" {
		return nil
	}
	switch t := typeDeclStmt.Type.(type) {
	case types.TupleType, types.NamedStructType:
		// Key by the declaration's name (typeDeclStmt.Name), not t.GetName():
		// TupleType.GetName() renders the full shape ("Point(i32, i32)"), while
		// the definition pass and lowerType both look the type up by its bare
		// declared name ("Point").
		return l.declareNamedStruct(typeDeclStmt.Name)
	default:
		return fmt.Errorf("llvm: unsupported type %s", t)
	}
}

// declareNamedStruct registers an empty, named LLVM struct type as a placeholder
// for a tuple/struct declaration. The definition pass (lowerTupleDef/
// lowerStructDef) fills in its fields; declaring every named type first lets a
// later type's fields reference an earlier one — and a type reference itself,
// once boxed layout lands.
func (l *lowerer) declareNamedStruct(name string) error {
	if name == "" {
		return fmt.Errorf("llvm: tuple or struct type must have a name")
	}
	st := lltypes.NewStruct()     // empty placeholder
	l.module.NewTypeDef(name, st) // registers it, sets TypeName — st now has identity
	l.structTypes[name] = st
	return nil
}

func (l *lowerer) lowerTypeDefinitions(program *ast.Program) error {
	for _, statement := range program.Statements {
		if typeDeclStmt, ok := statement.(*ast.TypeDeclStmt); ok {
			if err := l.lowerTypeDef(typeDeclStmt); err != nil {
				return err
			}
		}
	}
	return nil
}

func (l *lowerer) lowerTypeDef(typeDeclStmt *ast.TypeDeclStmt) error {
	if typeDeclStmt.Name == "?" {
		return nil
	}
	switch t := typeDeclStmt.Type.(type) {
	case types.TupleType:
		return l.lowerTupleDef(t)
	case types.NamedStructType:
		return l.lowerStructDef(t)
	default:
		return fmt.Errorf("llvm: unsupported type %s", t)
	}
}

func (l *lowerer) lowerTupleDef(t types.TupleType) error {
	st := l.structTypes[t.Name]
	for _, element := range t.Elements {
		elementType, err := l.lowerType(element)
		if err != nil {
			return err
		}
		st.Fields = append(st.Fields, elementType)
	}
	return nil
}

func (l *lowerer) lowerStructDef(t types.NamedStructType) error {
	st := l.structTypes[t.Name]
	for _, field := range t.Fields {
		fieldType, err := l.lowerType(field.Type)
		if err != nil {
			return err
		}
		st.Fields = append(st.Fields, fieldType)
	}
	return nil
}

// beginFunction resets the per-function lowering state before a body is lowered.
func (l *lowerer) beginFunction(retType lltypes.Type, retSigned, entryABI bool) {
	l.locals = map[string]value.Value{}
	l.loops = nil
	l.retType = retType
	l.retSigned = retSigned
	l.entryABI = entryABI
}

// emitReturn lowers a `ret` for the current function, coercing val to the
// function's return type. main is the one special case: its Lyra u8 value goes
// through the C ABI's i32 slot (coerce to u8, then zero-extend). A nil val is a
// bare `return` (or a void function) → `ret void`.
func (l *lowerer) emitReturn(block *ir.Block, val value.Value) error {
	if l.entryABI {
		if val == nil {
			block.NewRet(constant.NewInt(lltypes.I32, 0))
			return nil
		}
		u8 := coerceIntWidth(block, val, false, lltypes.I8)
		block.NewRet(block.NewZExt(u8, lltypes.I32))
		return nil
	}
	if val == nil {
		block.NewRet(nil) // ret void
		return nil
	}
	intTy, ok := l.retType.(*lltypes.IntType)
	if !ok {
		return fmt.Errorf("llvm: return of non-integer type %s not implemented", l.retType)
	}
	block.NewRet(coerceIntWidth(block, val, l.retSigned, intTy))
	return nil
}

// loopCtx records the blocks a break/continue in the current loop jumps to. It's
// pushed while lowering a loop body and popped after, so a labeled break/continue
// can walk the stack for its target.
type loopCtx struct {
	breakTarget    *ir.Block // where `break` transfers control (the loop's exit block)
	continueTarget *ir.Block // where `continue` transfers control (the loop's post block)
	label          string    // the loop's label, "" if unlabeled
}

// loopTarget returns the loop a break/continue refers to: the innermost loop for
// an empty label, or the nearest enclosing loop with a matching label. The
// typechecker already validates that break/continue sit inside a loop and that
// any label resolves, so a miss here is a backend invariant violation.
func (l *lowerer) loopTarget(label string) (loopCtx, error) {
	if len(l.loops) == 0 {
		return loopCtx{}, fmt.Errorf("llvm: break/continue outside a loop")
	}
	if label == "" {
		return l.loops[len(l.loops)-1], nil
	}
	for _, v := range slices.Backward(l.loops) {
		if v.label == label {
			return v, nil
		}
	}
	return loopCtx{}, fmt.Errorf("llvm: no enclosing loop labeled %q", label)
}

// lowerEntry defines `@main` and returns the entry function's value as the
// process exit code. A u8 entry returns its body's value; a void entry runs the
// body for effect (none expressible yet) and returns 0.
//
// `@main` is declared `i32`, not the u8 that Lyra's entry-point convention
// exposes to the user — that's the actual C ABI signature the C runtime startup
// code expects (verified: clang emits `define i32 @main()` for a trivial C
// program), regardless of what a language lets its own `main` return. The u8→i32
// coercion (coerce to u8, then zero-extend) is the `entryABI` path of emitReturn,
// which lowerEntry shares with every explicit `return` and the implicit tail
// return; that's why main is set up with beginFunction like any other function.
func (l *lowerer) lowerEntry(entry *driver.EntryPoint) error {
	fn := l.module.NewFunc("main", lltypes.I32)
	l.beginFunction(lltypes.I32, false, true) // entryABI: emitReturn handles the u8→i32 coercion
	block := fn.NewBlock("entry")

	switch entry.Returns {
	case driver.EntryReturnExitCode:
		v, block, err := l.lowerExpr(block, entry.Lambda.Body)
		if err != nil {
			return err
		}
		// `block` here is whatever block the body's evaluation ends in — for an
		// `if` body that's the merge block, not the entry block — so the `ret` is
		// emitted in the right place. Guard on Term: the body may now end in an
		// explicit `return` (which already emitted the ret and sealed the block).
		if block.Term == nil {
			if err := l.emitReturn(block, v); err != nil {
				return err
			}
		}
	default: // EntryReturnVoid — nothing observable to run yet; exit 0.
		block.NewRet(constant.NewInt(lltypes.I32, 0))
	}
	return nil
}

// forEachUserFunction calls fn for every top-level `let name = <lambda>` binding
// except the entry lambda (main, emitted by lowerEntry). Used for both the
// declare and define passes so they walk the program identically.
func (l *lowerer) forEachUserFunction(program *ast.Program, entry *ast.LambdaExpr, fn func(*ast.VarDeclStmt, *ast.LambdaExpr) error) error {
	for _, stmt := range program.Statements {
		decl, ok := stmt.(*ast.VarDeclStmt)
		if !ok {
			continue
		}
		lambda, ok := decl.Value.(*ast.LambdaExpr)
		if !ok || lambda == entry {
			continue
		}
		if err := fn(decl, lambda); err != nil {
			return err
		}
	}
	return nil
}

// declareFunction emits the function's signature (an ir.Func with no body) and
// records it in l.funcs, so calls can resolve it before its body is lowered.
// Several forms are deferred with a loud error rather than mis-lowered.
func (l *lowerer) declareFunction(decl *ast.VarDeclStmt, fn *ast.LambdaExpr) error {
	if len(fn.LambdaClauses) > 0 {
		return fmt.Errorf("llvm: multi-clause functions are not implemented yet (%q)", decl.Name)
	}
	if _, isVoid := fn.ReturnType.Type.(types.VoidType); isVoid {
		return fmt.Errorf("llvm: void-returning functions are not implemented yet (%q)", decl.Name)
	}
	if fn.ReturnType.Type == nil {
		return fmt.Errorf("llvm: function %q needs a return type annotation", decl.Name)
	}
	retType, err := l.lowerType(fn.ReturnType.Type)
	if err != nil {
		return err
	}
	irParams := make([]*ir.Param, 0, len(fn.Parameters))
	for _, param := range fn.Parameters {
		if param.DefaultValue != nil {
			return fmt.Errorf("llvm: default parameter values are not implemented yet (%q)", decl.Name)
		}
		irParam, err := l.lowerParameter(param)
		if err != nil {
			return err
		}
		irParams = append(irParams, irParam)
	}
	l.funcs[decl.Name] = l.module.NewFunc(decl.Name, retType, irParams...)
	return nil
}

// defineFunction lowers a declared function's body: bind each parameter into a
// fresh alloca (so the body reads it like any local), lower the body, and emit
// the implicit tail return (unless the body already ended in an explicit one).
func (l *lowerer) defineFunction(decl *ast.VarDeclStmt, fn *ast.LambdaExpr) error {
	irFn := l.funcs[decl.Name]
	retType, err := l.lowerType(fn.ReturnType.Type)
	if err != nil {
		return err
	}
	l.beginFunction(retType, returnSigned(fn), false)

	entry := irFn.NewBlock("entry")
	for i, param := range fn.Parameters {
		ident, ok := param.Pattern.(*ast.IdentifierPattern)
		if !ok {
			return fmt.Errorf("llvm: destructuring parameters are not implemented yet (%q)", decl.Name)
		}
		p := irFn.Params[i]
		slot := entry.NewAlloca(p.Type())
		entry.NewStore(p, slot)
		l.locals[ident.Name] = slot
	}

	v, end, err := l.lowerExpr(entry, fn.Body)
	if err != nil {
		return err
	}
	if end.Term == nil {
		if err := l.emitReturn(end, v); err != nil {
			return err
		}
	}
	return nil
}

// returnSigned reports whether fn's declared return type is a signed integer
// (so emitReturn widens with sext rather than zext when it must).
func returnSigned(fn *ast.LambdaExpr) bool {
	p, ok := fn.ReturnType.Type.(types.PrimitiveType)
	return ok && IsSignedInt(p.Name)
}

func (l *lowerer) lowerParameter(param ast.Parameter) (*ir.Param, error) {
	irType, err := l.lowerType(param.Type)
	if err != nil {
		return nil, err
	}
	return ir.NewParam(param.GetName(), irType), nil
}

func (l *lowerer) lowerType(lyraType types.Type) (lltypes.Type, error) {
	switch t := lyraType.(type) {
	case types.PrimitiveType:
		irType, ok := LLVMPrimitive(t.Name)
		if !ok {
			return nil, fmt.Errorf("unknown primitive type: %s", t.Name)
		}
		return irType, nil
	case types.TupleType:
		// A named tuple resolves to the struct type registered in the declaration
		// pass (key by t.Name, not GetName() which renders the full shape — see
		// declareNamedStruct). An anonymous tuple (`(1, 2)`) has no declaration,
		// so build its structural struct type on the fly from its elements.
		if types.IsAnonymousTupleName(t.Name) {
			return l.lowerAnonymousTupleType(t)
		}
		return l.lookupNamedType(t.Name)
	case types.NamedStructType:
		return l.lookupNamedType(t.Name)
	case types.UnresolvedType:
		// A named type referenced from another type declaration's field/element
		// (`struct Line { a: Point }`) stays an UnresolvedType — the typechecker
		// doesn't rewrite it to the concrete tuple/struct. Both declaration
		// passes ran first, so the name resolves against structTypes.
		return l.lookupNamedType(t.Name)
	default:
		return nil, fmt.Errorf("unknown type: %s", lyraType)
	}
}

// lookupNamedType returns the LLVM struct type registered for a named tuple or
// struct. It must already exist: every top-level type decl is declared (via
// declareNamedStruct) before any definition is lowered, so a field/element
// referencing another named type always resolves.
func (l *lowerer) lookupNamedType(name string) (lltypes.Type, error) {
	st, ok := l.structTypes[name]
	if !ok {
		return nil, fmt.Errorf("llvm: unknown named type %q", name)
	}
	return st, nil
}

// lowerAnonymousTupleType builds the (unnamed) LLVM struct type for an anonymous
// tuple `(1, 2)` from its element types. Unlike a named tuple it has no
// declaration to register against, and LLVM struct types are structural, so a
// fresh NewStruct is the whole representation — two anonymous tuples of the same
// shape lower to equal (interchangeable) LLVM types.
func (l *lowerer) lowerAnonymousTupleType(t types.TupleType) (*lltypes.StructType, error) {
	fields := make([]lltypes.Type, len(t.Elements))
	for i, elem := range t.Elements {
		ft, err := l.lowerType(elem)
		if err != nil {
			return nil, err
		}
		fields[i] = ft
	}
	return lltypes.NewStruct(fields...), nil
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
	}
	return nil, nil, fmt.Errorf("llvm: expression lowering not implemented for %T", expr)
}

// lowerTupleLiteralExpr lowers tuple construction (`Point(3, 4)`, `(1, 2)`) to an
// SSA aggregate: start from an undef struct and `insertvalue` each element in
// declaration order. The result is a first-class struct *value* — a `let` binding
// then allocas it by its `.Type()` and store/loads it like any scalar (mem2reg
// promotes it), and lowerTupleIndexExpr reads elements back with `extractvalue`.
//
// Only tuple-typed literals lower here. A capitalized call that the typechecker
// resolved to a data-type constructor (`Some(42)`) records a DataType, not a
// TupleType — that lowering (tagged unions, DATA_LAYOUT.md) isn't implemented, so
// it errors loudly rather than mis-building a struct.
func (l *lowerer) lowerTupleLiteralExpr(block *ir.Block, e *ast.TupleLiteralExpr) (value.Value, *ir.Block, error) {
	recorded, ok := l.res.TypeTable.Get(e)
	if !ok {
		return nil, nil, fmt.Errorf("llvm: no type recorded for tuple literal")
	}
	tupleType, ok := recorded.(types.TupleType)
	if !ok {
		return nil, nil, fmt.Errorf("llvm: tuple literal lowering not implemented for %s", recorded)
	}
	llType, err := l.lowerType(tupleType)
	if err != nil {
		return nil, nil, err
	}
	structType, ok := llType.(*lltypes.StructType)
	if !ok {
		return nil, nil, fmt.Errorf("llvm: tuple type %s did not lower to a struct", tupleType)
	}

	var agg value.Value = constant.NewUndef(structType)
	for i, elemExpr := range e.Elements {
		var elemVal value.Value
		elemVal, block, err = l.lowerExpr(block, elemExpr)
		if err != nil {
			return nil, nil, err
		}
		agg = block.NewInsertValue(agg, elemVal, uint64(i))
	}
	return agg, block, nil
}

// lowerTupleIndexExpr lowers positional tuple access (`pair.0`) to an
// `extractvalue` on the (already first-class) struct value the object lowers to.
// The typechecker validated the index is in range, so it maps straight to the
// struct element position.
func (l *lowerer) lowerTupleIndexExpr(block *ir.Block, e *ast.TupleIndexExpr) (value.Value, *ir.Block, error) {
	obj, block, err := l.lowerExpr(block, e.Object)
	if err != nil {
		return nil, nil, err
	}
	if _, ok := obj.Type().(*lltypes.StructType); !ok {
		return nil, nil, fmt.Errorf("llvm: tuple index on non-struct value of type %s", obj.Type())
	}
	return block.NewExtractValue(obj, uint64(e.Index)), block, nil
}

// literalIntType returns the LLVM integer type an integer literal should lower
// at: the concrete width the typechecker recorded for it (via context-directed
// literal-width inference), or i64 when the literal has no resolved context (an
// unannotated binding, or a literal whose value didn't fit the context width so
// the typechecker deliberately left it untyped — see propagateLiteralType).
func (l *lowerer) literalIntType(e ast.Expression) *lltypes.IntType {
	if t, ok := l.res.TypeTable.Get(e); ok {
		if p, ok := t.(types.PrimitiveType); ok {
			if ll, ok := LLVMPrimitive(p.Name); ok {
				if it, ok := ll.(*lltypes.IntType); ok {
					return it
				}
			}
		}
	}
	return lltypes.I64
}

func (l *lowerer) lowerBooleanBinaryOpExpr(block *ir.Block, e *ast.BooleanBinaryOpExpr) (value.Value, *ir.Block, error) {
	left, block, err := l.lowerExpr(block, e.Left)
	if err != nil {
		return nil, nil, err
	}

	switch e.Operator {
	case ast.BooleanBinaryOpAnd:
		return l.lowerBooleanAnd(block, left, e.Right)
	case ast.BooleanBinaryOpOr:
		return l.lowerBooleanOr(block, left, e.Right)
	}

	right, block, err := l.lowerExpr(block, e.Right)
	if err != nil {
		return nil, nil, err
	}
	// Comparisons are integer-only for now (bool `==`/`!=` included — i1 is
	// an integer type). `icmp` requires both operands to have the same
	// integer type, so two cases are reported explicitly rather than emitting
	// invalid IR clang would reject: a float operand (would need `fcmp`, and no
	// float value is lowerable yet anyway), and a width mismatch. With
	// context-directed literal-width inference a literal sibling now takes the
	// concrete operand's width (`i8(x) < 3` → both i8), so the width guard is
	// defensive: it fires only when a literal is too large for that width and
	// was left untyped (`i8(x) < 300`), lowering to the i64 default — loud, not
	// miscompiled.
	lt, lok := left.Type().(*lltypes.IntType)
	rt, rok := right.Type().(*lltypes.IntType)
	if !lok || !rok {
		return nil, nil, fmt.Errorf("llvm: comparison of non-integer operands not implemented (%s, %s)", left.Type(), right.Type())
	}
	if lt.BitSize != rt.BitSize {
		return nil, nil, fmt.Errorf("llvm: comparison of mismatched integer widths not implemented (%s vs %s)", left.Type(), right.Type())
	}
	signed, err := l.getIntSignedness(e.Left)
	if err != nil {
		return nil, nil, err
	}
	var cmpOp enum.IPred
	switch e.Operator {
	case ast.BooleanBinaryOpEq:
		cmpOp = enum.IPredEQ
	case ast.BooleanBinaryOpNEq:
		cmpOp = enum.IPredNE
	case ast.BooleanBinaryOpLT:
		if signed {
			cmpOp = enum.IPredSLT
		} else {
			cmpOp = enum.IPredULT
		}
	case ast.BooleanBinaryOpLTE:
		if signed {
			cmpOp = enum.IPredSLE
		} else {
			cmpOp = enum.IPredULE
		}
	case ast.BooleanBinaryOpGT:
		if signed {
			cmpOp = enum.IPredSGT
		} else {
			cmpOp = enum.IPredUGT
		}
	case ast.BooleanBinaryOpGTE:
		if signed {
			cmpOp = enum.IPredSGE
		} else {
			cmpOp = enum.IPredUGE
		}
	default:
		return nil, nil, fmt.Errorf("llvm: boolean operator %v not implemented", e.Operator)
	}
	return block.NewICmp(cmpOp, left, right), block, nil
}

// lowerBooleanAnd lowers `a && b` with short-circuit semantics: b is evaluated
// only when a is true. It's `if a { b } else { false }` (lowerIf's diamond),
// with one simplification — the `else { false }` branch is *virtual*: it needs
// no block of its own, because there's nothing to compute. The cond-br's false
// edge points straight at the merge block, and the phi supplies the constant
// `false` for that predecessor. So only rhsBlock (where b is evaluated) plus
// merge are created, not two branch blocks.
//
// leftVal is a's already-lowered i1 value; block is where a *finished* (a may
// itself have branched). rightExpr is b's AST node, lowered lazily inside
// rhsBlock so it doesn't run when a is false. The phi's predecessors are `block`
// (a-false edge → false) and rhsEnd (a-true edge → b's value) — rhsEnd, not
// rhsBlock, since b can itself contain an `if` that moves control onward.
func (l *lowerer) lowerBooleanAnd(block *ir.Block, leftVal value.Value, rightExpr ast.Expression) (value.Value, *ir.Block, error) {
	fn := block.Parent
	rhsBlock := fn.NewBlock("")
	mergeBlock := fn.NewBlock("")
	block.NewCondBr(leftVal, rhsBlock, mergeBlock) // a true → eval b; a false → skip to merge
	rightVal, rhsEnd, err := l.lowerExpr(rhsBlock, rightExpr)
	if err != nil {
		return nil, nil, err
	}
	rhsEnd.NewBr(mergeBlock)

	phi := mergeBlock.NewPhi(
		ir.NewIncoming(constant.NewInt(lltypes.I1, 0), block), // a was false ⇒ false
		ir.NewIncoming(rightVal, rhsEnd),                      // a was true  ⇒ b's value
	)
	return phi, mergeBlock, nil
}

// lowerBooleanOr lowers `a || b` with short-circuit semantics: b is evaluated
// only when a is false. The mirror of lowerBooleanAnd — it's
// `if a { true } else { b }`, so the cond-br targets are swapped (a true skips
// straight to merge; a false evaluates b) and the virtual constant branch
// supplies `true`. See lowerBooleanAnd's comment for the block/phi reasoning.
func (l *lowerer) lowerBooleanOr(block *ir.Block, leftVal value.Value, rightExpr ast.Expression) (value.Value, *ir.Block, error) {
	fn := block.Parent
	rhsBlock := fn.NewBlock("")
	mergeBlock := fn.NewBlock("")
	block.NewCondBr(leftVal, mergeBlock, rhsBlock) // a true → skip to merge; a false → eval b
	rightVal, rhsEnd, err := l.lowerExpr(rhsBlock, rightExpr)
	if err != nil {
		return nil, nil, err
	}
	rhsEnd.NewBr(mergeBlock)

	phi := mergeBlock.NewPhi(
		ir.NewIncoming(constant.NewInt(lltypes.I1, 1), block), // a was true  ⇒ true
		ir.NewIncoming(rightVal, rhsEnd),                      // a was false ⇒ b's value
	)
	return phi, mergeBlock, nil
}

func (l *lowerer) lowerMathBinaryOpExpr(block *ir.Block, e *ast.MathBinaryOpExpr) (value.Value, *ir.Block, error) {
	left, block, err := l.lowerExpr(block, e.Left)
	if err != nil {
		return nil, nil, err
	}
	right, block, err := l.lowerExpr(block, e.Right)
	if err != nil {
		return nil, nil, err
	}
	signed, err := l.getIntSignedness(e.Left)
	if err != nil {
		return nil, nil, err
	}
	v, err := l.applyIntMathOp(block, e.Operator, left, right, signed)
	return v, block, err
}

// applyIntMathOp emits the instruction(s) for a binary integer arithmetic op on
// two already-lowered same-width values. Shared by lowerMathBinaryOpExpr and
// lowerMathAssignOp (`i += x` is `i = i <op> x`). signed selects sdiv/srem vs
// udiv/urem and the floored-remainder fixup; add/sub/mul are signedness-agnostic.
func (l *lowerer) applyIntMathOp(block *ir.Block, op ast.MathBinaryOp, left, right value.Value, signed bool) (value.Value, error) {
	switch op {
	case ast.MathBinaryOpAdd:
		return block.NewAdd(left, right), nil
	case ast.MathBinaryOpSub:
		return block.NewSub(left, right), nil
	case ast.MathBinaryOpMul:
		return block.NewMul(left, right), nil
	case ast.MathBinaryOpDiv:
		if signed {
			return block.NewSDiv(left, right), nil
		}
		return block.NewUDiv(left, right), nil
	case ast.MathBinaryOpMod:
		// Mod (%): Odin's "modulo (truncated)" — sign follows the
		// dividend, exactly what LLVM's srem/urem give natively.
		// 11 % -3 = 2.
		if signed {
			return block.NewSRem(left, right), nil
		}
		return block.NewURem(left, right), nil
	case ast.MathBinaryOpRemainder:
		// Remainder (%%): Odin's "remainder (floored)" — sign follows the
		// divisor, distinct from Mod above. 11 %% -3 = -1 (vs Mod's 2).
		// Unsigned floored remainder is identical to truncated (every
		// value is non-negative, so there's nothing to floor), hence
		// urem directly; the signed case needs lowerFlooredSRem's fixup.
		if signed {
			return l.lowerFlooredSRem(block, left, right), nil
		}
		return block.NewURem(left, right), nil
	default:
		return nil, fmt.Errorf("llvm: math binary op lowering not implemented for %v", op)
	}
}

// mathAssignToBinaryOp maps a compound-assignment operator to the plain binary op
// it applies (`+=` → `+`), so lowerMathAssignOp reuses applyIntMathOp.
var mathAssignToBinaryOp = map[ast.MathAssignOp]ast.MathBinaryOp{
	ast.MathAssignOpAdd:       ast.MathBinaryOpAdd,
	ast.MathAssignOpSub:       ast.MathBinaryOpSub,
	ast.MathAssignOpMul:       ast.MathBinaryOpMul,
	ast.MathAssignOpDiv:       ast.MathBinaryOpDiv,
	ast.MathAssignOpMod:       ast.MathBinaryOpMod,
	ast.MathAssignOpRemainder: ast.MathBinaryOpRemainder,
}

// lowerMathAssignOp lowers a compound assignment (`i += x`) to load / op / store
// against the target's alloca: load the current value, apply the binary op with
// the lowered RHS, store the result back. The target is always a local binding
// (an lvalue name), so its slot is in l.locals. Signedness comes from the RHS,
// whose width the typechecker propagated to match the target (checkMathAssignOp),
// so both operands share a width for the op.
func (l *lowerer) lowerMathAssignOp(block *ir.Block, e *ast.MathAssignOpExpr) (value.Value, *ir.Block, error) {
	slot, ok := l.locals[e.Left.Name]
	if !ok {
		return nil, nil, fmt.Errorf("llvm: compound assignment to unbound identifier %q", e.Left.Name)
	}
	binOp, ok := mathAssignToBinaryOp[e.Operator]
	if !ok {
		return nil, nil, fmt.Errorf("llvm: compound assignment %q not implemented", e.Operator)
	}
	ptr := slot.(*ir.InstAlloca)
	cur := block.NewLoad(ptr.ElemType, slot)
	rhs, block, err := l.lowerExpr(block, e.Right)
	if err != nil {
		return nil, nil, err
	}
	signed, err := l.getIntSignedness(e.Right)
	if err != nil {
		return nil, nil, err
	}
	result, err := l.applyIntMathOp(block, binOp, cur, rhs, signed)
	if err != nil {
		return nil, nil, err
	}
	block.NewStore(result, slot)
	// A compound assignment yields no value (see the typechecker's void result).
	return nil, block, nil
}

func (l *lowerer) lowerNegationExpr(block *ir.Block, e *ast.NegationExpr) (value.Value, *ir.Block, error) {
	operand, block, err := l.lowerExpr(block, e.Operand)
	if err != nil {
		return nil, nil, err
	}
	// Branch on the already-lowered value's own LLVM type rather than a
	// second TypeTable lookup: the typechecker (inferNegationExpr) already
	// rejects a non-numeric or unsigned operand, so by the time a
	// well-typed program reaches here the operand is always a signed int
	// or a float.
	switch t := operand.Type().(type) {
	case *lltypes.IntType:
		// LLVM IR has no dedicated integer negate; `sub 0, x` is the
		// standard idiom (what clang emits for unary minus on an int).
		// Deliberately plain `sub`, not `sub nsw`: an nsw flag tells the
		// optimizer overflow is undefined behavior, which conflicts with
		// Lyra's "checked arithmetic by default" goal (todo #2) — revisit
		// once overflow trapping exists.
		return block.NewSub(constant.NewInt(t, 0), operand), block, nil
	case *lltypes.FloatType:
		return block.NewFNeg(operand), block, nil
	default:
		return nil, nil, fmt.Errorf("llvm: negation lowering not implemented for operand type %s", operand.Type())
	}
}

func (l *lowerer) lowerFunctionCallExpr(block *ir.Block, e *ast.FunctionCallExpr) (value.Value, *ir.Block, error) {
	ident, ok := e.Function.(*ast.IdentifierExpr)
	if !ok {
		// Higher-order calls (calling a lambda value / a function-typed local)
		// aren't lowered yet — only direct calls by name.
		return nil, nil, fmt.Errorf("llvm: only direct calls by function name are implemented, got %T callee", e.Function)
	}
	// A type-name callee is a numeric conversion (`i32(x)`), not a function call.
	if targetName := types.PrimitiveTypeName(ident.Name); IsNumericConversionTarget(targetName) {
		return l.lowerNumericConversion(block, e, targetName)
	}
	fn, ok := l.funcs[ident.Name]
	if !ok {
		return nil, nil, fmt.Errorf("llvm: call to unknown function %q", ident.Name)
	}
	// Arguments match the parameters positionally. The typechecker validated
	// arity and assignability, and propagated each parameter's width onto its
	// argument (inferLambdaCall), so a literal arg already lowers at the param's
	// width — no coercion needed here.
	args := make([]value.Value, 0, len(e.Arguments))
	for _, argExpr := range e.Arguments {
		var (
			v   value.Value
			err error
		)
		v, block, err = l.lowerExpr(block, argExpr)
		if err != nil {
			return nil, nil, err
		}
		args = append(args, v)
	}
	return block.NewCall(fn, args...), block, nil
}

// lowerBlockStmts lowers each statement of be into block, threading the current
// block (a nested `if`/loop moves control onward). It returns the value of the
// last statement — nil when that statement is a binding/reassignment/loop or the
// block terminated early via break/continue — so it serves both value-position
// blocks (via lowerBlock) and effect-position blocks (loop and one-armed-if
// bodies, via lowerForEffect).
//
// Break/continue are the first constructs that terminate a block mid-stream:
// anything after them is unreachable, so the loop stops once the current block
// has a terminator (`block.Term != nil`) rather than lowering into a sealed block
// (which would be invalid IR).
func (l *lowerer) lowerBlockStmts(block *ir.Block, be *ast.BlockExpr) (value.Value, *ir.Block, error) {
	var v value.Value
	for _, stmt := range be.Statements {
		if block.Term != nil {
			break // a prior break/continue sealed this block; the rest is unreachable
		}
		var err error
		switch s := stmt.(type) {
		case *ast.ExpressionStmt:
			v, block, err = l.lowerExpr(block, s.Expression)
		case *ast.VarDeclStmt:
			block, err = l.lowerVarDecl(block, s)
			v = nil // a binding is not itself the block's value
		case *ast.VarReassignmentStmt:
			block, err = l.lowerVarReassignment(block, s)
			v = nil
		case *ast.BreakStmt:
			err = l.lowerBreak(block, s)
			v = nil
		case *ast.ContinueStmt:
			err = l.lowerContinue(block, s)
			v = nil
		case *ast.ReturnStmt:
			block, err = l.lowerReturn(block, s)
			v = nil
		default:
			return nil, nil, fmt.Errorf("llvm: block statement lowering not implemented for %T", stmt)
		}
		if err != nil {
			return nil, nil, err
		}
	}
	return v, block, nil
}

// lowerBlock lowers a value-position block: its value is the value of its last
// statement (matching the typechecker's inferBlockType). It requires that value
// to exist — a block used where a value is needed must end in an expression.
func (l *lowerer) lowerBlock(block *ir.Block, be *ast.BlockExpr) (value.Value, *ir.Block, error) {
	v, end, err := l.lowerBlockStmts(block, be)
	if err != nil {
		return nil, nil, err
	}
	if v == nil && end.Term == nil {
		return nil, nil, fmt.Errorf("llvm: block has no value (empty, or last statement is not an expression)")
	}
	return v, end, nil
}

// lowerForEffect lowers an expression for its side effects, discarding any value.
// A block goes through lowerBlockStmts (so a body ending in a reassignment or
// break is fine — no value required); any other expression through lowerExpr.
func (l *lowerer) lowerForEffect(block *ir.Block, expr ast.Expression) (*ir.Block, error) {
	if be, ok := expr.(*ast.BlockExpr); ok {
		_, end, err := l.lowerBlockStmts(block, be)
		return end, err
	}
	_, end, err := l.lowerExpr(block, expr)
	return end, err
}

// lowerBreak / lowerContinue transfer control to the target loop's exit / post
// block. The block is sealed by the resulting `br`; lowerBlockStmts stops after.
func (l *lowerer) lowerBreak(block *ir.Block, s *ast.BreakStmt) error {
	if s.Value != nil {
		return fmt.Errorf("llvm: break with a value (loop as expression) not implemented")
	}
	ctx, err := l.loopTarget(s.Label)
	if err != nil {
		return err
	}
	block.NewBr(ctx.breakTarget)
	return nil
}

func (l *lowerer) lowerContinue(block *ir.Block, s *ast.ContinueStmt) error {
	ctx, err := l.loopTarget(s.Label)
	if err != nil {
		return err
	}
	block.NewBr(ctx.continueTarget)
	return nil
}

// lowerReturn lowers an explicit `return [value]`, emitting the `ret` via
// emitReturn (which coerces to the current function's return type) and sealing
// the block. Returns the block the value evaluation ended in — a value
// containing an `if` moves control onward before the `ret` — so lowerBlockStmts
// sees a sealed block and stops.
func (l *lowerer) lowerReturn(block *ir.Block, s *ast.ReturnStmt) (*ir.Block, error) {
	if s.Value == nil {
		return block, l.emitReturn(block, nil)
	}
	v, block, err := l.lowerExpr(block, s.Value)
	if err != nil {
		return nil, err
	}
	return block, l.emitReturn(block, v)
}

// lowerForLoop lowers a C-style `for` loop to the standard cond/body/post/exit
// CFG with a back-edge:
//
//	init (once, current block)
//	br cond
//	cond: <condition> ; cond_br body, exit   (nil condition → br body: infinite loop)
//	body: <body for effect> ; br post
//	post: <post> ; br cond                    (continue targets post, so it runs)
//	exit: ...                                 (break targets exit; control continues here)
//
// A loop is a statement (no value), so it returns a nil value and the exit block.
//
// Every fall-through `br` is guarded by `end.Term == nil`: a body (or post) that
// ended in break/continue/return has already sealed its block, and emitting a
// second terminator would be invalid IR.
//
// All three forms reach here: infinite (`for {}`, nil condition → an
// unconditional branch into the body), condition-only (`for cond {}`), and the
// three-clause `for var i = 0; i < n; i += 1` (Init via lowerVarDecl, Post a
// MathAssignOpExpr via lowerMathAssignOp).
func (l *lowerer) lowerForLoop(block *ir.Block, e *ast.ForLoopExpr) (value.Value, *ir.Block, error) {
	if e.Init != nil {
		var err error
		if block, err = l.lowerVarDecl(block, e.Init); err != nil {
			return nil, nil, err
		}
	}

	fn := block.Parent
	condBlock := fn.NewBlock("")
	bodyBlock := fn.NewBlock("")
	postBlock := fn.NewBlock("") // continue target; brs to cond (with the post effect, if any)
	exitBlock := fn.NewBlock("") // break target; where control continues after the loop
	block.NewBr(condBlock)

	// Condition. A nil condition is an infinite loop (exit only via break), so it
	// branches unconditionally into the body.
	if e.Condition != nil {
		condVal, condEnd, err := l.lowerExpr(condBlock, *e.Condition)
		if err != nil {
			return nil, nil, err
		}
		condEnd.NewCondBr(condVal, bodyBlock, exitBlock)
	} else {
		condBlock.NewBr(bodyBlock)
	}

	// Body, lowered for effect with this loop pushed so break/continue resolve.
	l.loops = append(l.loops, loopCtx{breakTarget: exitBlock, continueTarget: postBlock, label: e.Label})
	bodyEnd, err := l.lowerForEffect(bodyBlock, &e.Body)
	l.loops = l.loops[:len(l.loops)-1]
	if err != nil {
		return nil, nil, err
	}
	if bodyEnd.Term == nil {
		bodyEnd.NewBr(postBlock)
	}

	// Post, then back to the condition.
	if e.Post != nil {
		postEnd, err := l.lowerForEffect(postBlock, *e.Post)
		if err != nil {
			return nil, nil, err
		}
		if postEnd.Term == nil {
			postEnd.NewBr(condBlock)
		}
	} else {
		postBlock.NewBr(condBlock)
	}

	return nil, exitBlock, nil
}

func (l *lowerer) lowerVarDecl(block *ir.Block, vds *ast.VarDeclStmt) (*ir.Block, error) {
	init, block, err := l.lowerExpr(block, vds.Value)
	if err != nil {
		return nil, err
	}
	// Alloca in the *entry* block (mem2reg only promotes entry-block allocas).
	entry := block.Parent.Blocks[0]
	slot := entry.NewAlloca(init.Type())
	block.NewStore(init, slot)
	l.locals[vds.Name] = slot // later re-declaration of the same name just overwrites
	return block, nil
}

func (l *lowerer) lowerVarReassignment(block *ir.Block, vrs *ast.VarReassignmentStmt) (*ir.Block, error) {
	rhsVal, block, err := l.lowerExpr(block, vrs.Value)
	if err != nil {
		return nil, err
	}
	// Store into the existing alloca; the locals entry stays the alloca slot
	// (a pointer), NOT the stored value — a later read loads from it. Overwriting
	// it with rhsVal would break the next IdentifierExpr load (slot.(*InstAlloca)).
	block.NewStore(rhsVal, l.locals[vrs.Name])
	return block, nil
}

// lowerIf lowers an if/else expression to the standard four-block diamond with
// a phi at the merge:
//
//	         cond br
//	current ─────────┬──> then ──br──┐
//	                 └──> else ──br──┴──> merge: phi [thenVal, thenEnd], [elseVal, elseEnd]
//
// Each branch computes its value, then jumps to a shared merge block whose phi
// selects the result based on which predecessor control arrived from — and that
// phi is the if-expression's value.
//
// A two-armed `if` is a value (both branches feed the phi). A one-armed `if`
// (no `else`) only reaches here as a statement — the typechecker rejects a
// one-armed `if` in value position (checkIfExpr) — so it produces no value: the
// false edge goes straight to merge and there is no phi. This is what lets
// `if cond { break }` lower inside a loop body.
//
// The phi's incoming predecessors and the branches into merge use the block
// each branch *ends in* (thenEnd/elseEnd), NOT the block we started it in: a
// branch whose body contains its own `if` will have moved control into a
// different block by the time it produces its value. Using the start block here
// would be the classic phi/branch bug.
func (l *lowerer) lowerIf(block *ir.Block, e *ast.IfExpr) (value.Value, *ir.Block, error) {
	if e.Condition == nil || e.Then == nil {
		return nil, nil, fmt.Errorf("llvm: if lowering requires a condition and a then branch")
	}
	cond, block, err := l.lowerExpr(block, e.Condition)
	if err != nil {
		return nil, nil, err
	}
	fn := block.Parent

	// One-armed `if` as a statement: no value, no phi. The then branch is lowered
	// for effect and may seal its own block (a `break`/`continue`), so the
	// fall-through to merge is guarded.
	if e.Else == nil {
		thenBlock := fn.NewBlock("")
		mergeBlock := fn.NewBlock("")
		block.NewCondBr(cond, thenBlock, mergeBlock)
		thenEnd, err := l.lowerForEffect(thenBlock, e.Then)
		if err != nil {
			return nil, nil, err
		}
		if thenEnd.Term == nil {
			thenEnd.NewBr(mergeBlock)
		}
		return nil, mergeBlock, nil
	}

	thenBlock := fn.NewBlock("")
	elseBlock := fn.NewBlock("")
	mergeBlock := fn.NewBlock("")
	block.NewCondBr(cond, thenBlock, elseBlock)

	thenVal, thenEnd, err := l.lowerExpr(thenBlock, e.Then)
	if err != nil {
		return nil, nil, err
	}
	thenEnd.NewBr(mergeBlock)

	elseVal, elseEnd, err := l.lowerExpr(elseBlock, e.Else)
	if err != nil {
		return nil, nil, err
	}
	elseEnd.NewBr(mergeBlock)

	// Both incoming values must share an LLVM type for a well-formed phi. The
	// typechecker's branchCommonType guarantees the branches are type-
	// compatible; a genuine width mismatch that slipped through would produce
	// invalid IR that clang rejects (loud), not silently-wrong code.
	phi := mergeBlock.NewPhi(
		ir.NewIncoming(thenVal, thenEnd),
		ir.NewIncoming(elseVal, elseEnd),
	)
	return phi, mergeBlock, nil
}

// lowerNumericConversion lowers a Lyra type-conversion call (`i8(x)`,
// `u32(x)`, … — Pit-of-Success #5's one conversion syntax) to the matching
// LLVM conversion instruction: identity (same width), `trunc` (narrowing), or
// `sext`/`zext` (widening, picked from the *source* Lyra type's signedness —
// LLVM's integer types don't carry that themselves; width/kind come from the
// already-lowered argument's own LLVM type).
//
// Int-only for now, not by oversight: a float target or source is deferred,
// since there is no valid, type-checked Lyra program that can reach it today
// (verified — see the error site below) given the current scope (no
// `let`/blocks yet, `main` must return i64, no float→int builtin).
func (l *lowerer) lowerNumericConversion(block *ir.Block, call *ast.FunctionCallExpr, targetName types.PrimitiveTypeName) (value.Value, *ir.Block, error) {
	if len(call.Arguments) != 1 {
		return nil, nil, fmt.Errorf("llvm: type conversion %q expects 1 argument, got %d", targetName, len(call.Arguments))
	}
	arg, block, err := l.lowerExpr(block, call.Arguments[0])
	if err != nil {
		return nil, nil, err
	}
	srcT, ok := l.res.TypeTable.Get(call.Arguments[0])
	if !ok {
		return nil, nil, fmt.Errorf("llvm: type not found for %T", call.Arguments[0])
	}
	srcP, ok := srcT.(types.PrimitiveType)
	if !ok {
		return nil, nil, fmt.Errorf("llvm: type not found for %T", call.Arguments[0])
	}
	dstLL, ok := LLVMPrimitive(targetName)
	if !ok {
		return nil, nil, fmt.Errorf("llvm: no LLVM representation for %q", targetName)
	}

	dst, ok := dstLL.(*lltypes.IntType)
	if !ok {
		// A float target (or float source): deferred, not just unimplemented
		// by oversight. Given today's scope — no `let`/blocks yet (a single
		// expression is the whole entry body), `main` must return i64, and
		// there's no float→int builtin — there is no valid, type-checked
		// Lyra program that can reach this path at all (verified: even
		// `i64(f64(200))` is rejected at typecheck, "use floor()/ceil()/
		// round()"). Implement once one of those changes (print, non-main
		// function returns, or rounding builtins) makes it observable/
		// testable, rather than shipping an instruction sequence nothing can
		// ever exercise.
		return nil, nil, fmt.Errorf("llvm: conversion to/from float not implemented yet (%s to %s)", arg.Type(), targetName)
	}
	if _, ok := arg.Type().(*lltypes.IntType); !ok {
		return nil, nil, fmt.Errorf("llvm: conversion from %s to %s not implemented", arg.Type(), targetName)
	}
	return coerceIntWidth(block, arg, IsSignedInt(srcP.Name), dst), block, nil
}

// coerceIntWidth adjusts v (an integer value) to dst's bit width: unchanged if
// already that width, `trunc` if narrower, or `sext`/`zext` if wider — the
// widening choice comes from srcSigned (v's *source* Lyra type's signedness;
// LLVM integers don't carry that themselves). Shared by lowerNumericConversion
// (an explicit `i8(x)`-style conversion call) and lowerEntry (coercing the
// entry body's value to the declared return width — see its doc comment for
// why that coercion is needed at all).
func coerceIntWidth(block *ir.Block, v value.Value, srcSigned bool, dst *lltypes.IntType) value.Value {
	src := v.Type().(*lltypes.IntType)
	switch {
	case dst.BitSize == src.BitSize:
		return v
	case dst.BitSize < src.BitSize:
		return block.NewTrunc(v, dst)
	case srcSigned:
		return block.NewSExt(v, dst)
	default:
		return block.NewZExt(v, dst)
	}
}

// lowerFlooredSRem computes the floored-division remainder of two signed
// integers of the same LLVM type — Odin's "remainder (floored)" (%%), sign
// follows the divisor, distinct from the truncated remainder LLVM's srem
// gives natively (sign follows the dividend, which is what Lyra's `%` uses
// directly via a plain block.NewSRem).
//
// The fixup is the standard branchless idiom: take the truncated remainder,
// and if it's non-zero and its sign disagrees with the divisor's, add the
// divisor back (this is exactly what floors it — the same correction
// CPython's `%` applies internally). Built with `select` rather than extra
// basic blocks/branches, since this sits inside a single expression.
func (l *lowerer) lowerFlooredSRem(block *ir.Block, left, right value.Value) value.Value {
	zero := constant.NewInt(left.Type().(*lltypes.IntType), 0)
	r := block.NewSRem(left, right)
	rNeg := block.NewICmp(enum.IPredSLT, r, zero)
	divisorNeg := block.NewICmp(enum.IPredSLT, right, zero)
	signsDiffer := block.NewXor(rNeg, divisorNeg)
	nonZero := block.NewICmp(enum.IPredNE, r, zero)
	needsFixup := block.NewAnd(nonZero, signsDiffer)
	fixed := block.NewAdd(r, right)
	return block.NewSelect(needsFixup, fixed, r)
}

func (l *lowerer) getIntSignedness(e ast.Expression) (bool, error) {
	t, ok := l.res.TypeTable.Get(e)
	if !ok {
		return false, fmt.Errorf("llvm: type not found for %T", e)
	}
	pt, ok := t.(types.PrimitiveType) // assert it's a primitive
	if !ok {
		return false, fmt.Errorf("llvm: type not found for %T", e)
	}
	return IsSignedInt(pt.Name), nil
}
