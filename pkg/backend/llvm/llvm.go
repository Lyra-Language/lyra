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
//     via lookupNamedType) lower; `data`/sum decls lower to their tagged union
//     { tag, payload-blob } per DATA_LAYOUT.md (lowerDataDef via DataUnionType) —
//     the layout is done; a value of that type (construction/match) is not.
//     `stack` values lower by value,
//     `shared` values to a pointer to a ref-counted box — see ALLOCATION.md. The
//     two docs compose: the sum-type layout is the payload; the flavor decides
//     inline vs boxed. layout.go/runtime.go provide the building blocks —
//     LLVMPrimitive, SharedBoxType, TagType, DataUnionType, SizeAndAlign, and
//     declareRuntime (wired into Emit) — for lowerType to dispatch over.
//  2. Grow lowerExpr further: string `match` and float. Already lowering:
//     arithmetic, comparisons, `&&`/`||`, `if`/blocks, `let`/`var`, calls, tuple
//     and struct instances, `data` construction (alloca + typed payload store),
//     and `match` over every scrutinee kind — `data` (tag switch), bool/integer
//     scalar (comparison ladder), and struct/tuple (shared aggregate ladder) —
//     with nested struct/tuple/`data` sub-patterns (aggPatternTest/aggPatternBind
//     recurse, a nested data tag becoming a comparison), value-testing payload
//     sub-patterns (`Some(0)`) and arm guards (both falling back from the tag
//     switch to the shared ladder). Mutable locals are modeled as `alloca` + load/store
//     (let mem2reg build SSA) rather than hand-written phi nodes. Floats lower —
//     literals (at their context-inferred width, default f64), arithmetic
//     (`fadd`/`fsub`/`fmul`/`fdiv`, `frem` for `%`, floored `frem` for `%%`),
//     `fneg`, comparisons (`fcmp`, ordered except `!=`), and float params/returns.
//     A float still can't reach the u8 exit code directly (no float→int
//     conversion — see lowerNumericConversion's doc), so a float is observed
//     through a comparison; float `match` is still deferred (loud error). Note
//     lowerExpr returns the block control ends in, not just a value — threaded so
//     a branching form (`if`) can move the insertion point; every non-branching
//     case returns its own block unchanged.
//  3. Runtime shims: print, and the overflow trap for todo #2 (via
//     llvm.sadd.with.overflow); the builtin overflow-arithmetic methods
//     (typechecker/builtins.go) lower to two's-complement +/-/* and
//     llvm.{s,u}{add,sub}.sat.
package llvm

import (
	"fmt"
	"slices"
	"strconv"

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
	case types.TupleType, types.NamedStructType, types.DataType:
		// Key by the declaration's name (typeDeclStmt.Name), not t.GetName():
		// TupleType.GetName() renders the full shape ("Point(i32, i32)"), while
		// the definition pass and lowerType both look the type up by its bare
		// declared name ("Point"). A `data` type registers the same way — its
		// definition is the tagged union `{ tag, payload-blob }`.
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
	case types.DataType:
		return l.lowerDataDef(t)
	default:
		return fmt.Errorf("llvm: unsupported type %s", t)
	}
}

// lowerDataDef fills a `data` type's registered placeholder with its tagged-union
// layout `{ iTAG, [K x iA] }` (DATA_LAYOUT.md): a tag sized to the variant count,
// followed by a payload blob sized/aligned to the largest variant. DataUnionType
// (layout.go) computes the shape; an all-nullary `data` (an enum) is just
// `{ iTAG }` with no blob.
//
// A by-value reference to another named type in a payload (`data W = Wrap(P)`)
// arrives as an UnresolvedType, which SizeAndAlign can't size on its own, so the
// payload types are first resolved through the symbol table (resolveForLayout).
// DataUnionType still fails (ok=false) for a genuinely un-sizeable payload — a
// string field or an un-monomorphized generic — a loud error, not a wrong layout.
// (A recursive reference must be `shared`, i.e. a pointer per lyra-E014, which is
// pointer-sized and stops the resolution before it recurses.)
func (l *lowerer) lowerDataDef(t types.DataType) error {
	st := l.structTypes[t.Name]
	resolved := l.resolveForLayout(t).(types.DataType)
	union, ok := DataUnionType(resolved)
	if !ok {
		return fmt.Errorf("llvm: cannot lay out data type %q yet (a variant payload isn't sizeable — e.g. a string field or an un-monomorphized generic)", t.Name)
	}
	st.Fields = append(st.Fields, union.Fields...)
	return nil
}

// resolveForLayout deep-resolves a type's UnresolvedType leaves against the
// symbol table so SizeAndAlign can size it — a named-type reference in a `data`
// payload or a struct field (`Wrap(P)`, `struct S { p: P }`) is stored as just a
// name. It short-circuits a `shared` reference (pointer-sized, so its referent is
// never chased) — which is also what keeps resolution finite: a recursive type's
// cycle must pass through a `shared` field (lyra-E014), so every by-value chain is
// acyclic and terminates.
func (l *lowerer) resolveForLayout(t types.Type) types.Type {
	switch v := t.(type) {
	case types.UnresolvedType:
		if v.Allocation == types.Shared {
			return t // a pointer; don't chase the referent (it may be recursive)
		}
		decl, ok := l.res.SymbolTable.Types[v.Name]
		if !ok {
			return t // unknown name; SizeAndAlign will fail loudly downstream
		}
		return l.resolveForLayout(types.WithAllocation(decl.Type, v.Allocation))
	case types.NamedStructType:
		fields := make([]types.StructField, len(v.Fields))
		for i, f := range v.Fields {
			f.Type = l.resolveForLayout(f.Type)
			fields[i] = f
		}
		v.Fields = fields
		return v
	case types.TupleType:
		elems := make([]types.Type, len(v.Elements))
		for i, e := range v.Elements {
			elems[i] = l.resolveForLayout(e)
		}
		v.Elements = elems
		return v
	case types.DataType:
		ctors := make([]types.DataTypeConstructor, len(v.Constructors))
		for i, c := range v.Constructors {
			params := make([]types.Type, len(c.Params))
			for j, p := range c.Params {
				params[j] = l.resolveForLayout(p)
			}
			ctors[i] = types.DataTypeConstructor{Name: c.Name, Params: params}
		}
		v.Constructors = ctors
		return v
	case types.StaticArrayType:
		v.ElementType = l.resolveForLayout(v.ElementType)
		return v
	}
	return t
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
	if floatTy, ok := l.retType.(*lltypes.FloatType); ok {
		block.NewRet(coerceFloatWidth(block, val, floatTy))
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
	case types.DataType:
		// A `data` value resolves to its registered tagged-union struct.
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
	case *ast.FloatLiteralExpr:
		return constant.NewFloat(l.literalFloatType(e), e.Value), block, nil
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

// lowerTupleLiteralExpr lowers tuple construction (`Point(3, 4)`, `(1, 2)`) to an
// SSA aggregate: start from an undef struct and `insertvalue` each element in
// declaration order. The result is a first-class struct *value* — a `let` binding
// then allocas it by its `.Type()` and store/loads it like any scalar (mem2reg
// promotes it), and lowerTupleIndexExpr reads elements back with `extractvalue`.
//
// Only tuple-typed literals build a plain aggregate here. A capitalized call the
// typechecker resolved to a data constructor (`Cons(1, tail)`) records a DataType,
// not a TupleType — that's a positional variant, routed to lowerDataConstruction
// (the tagged union, DATA_LAYOUT.md).
func (l *lowerer) lowerTupleLiteralExpr(block *ir.Block, e *ast.TupleLiteralExpr) (value.Value, *ir.Block, error) {
	recorded, ok := l.res.TypeTable.Get(e)
	if !ok {
		return nil, nil, fmt.Errorf("llvm: no type recorded for tuple literal")
	}
	if dt, ok := recorded.(types.DataType); ok {
		return l.lowerDataConstruction(block, dt, e.Name, e.Elements)
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
		elemVal, err = l.coerceAggregateElem(block, elemVal, structType.Fields[i], elemExpr)
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

// lowerStructInstanceExpr lowers struct construction (`Node { value: 3 }`) to a
// first-class struct value, the same insertvalue-over-undef shape as a tuple.
// The one extra concern is ordering: a struct literal names its fields and may
// list them in any order, but the LLVM struct is in *declaration* order — so the
// fields are keyed by name and built in the declared order (also the index each
// insertvalue targets).
//
// Deferred with a loud error: record-update syntax (`P { base | f: v }`), a
// missing field relying on a default value, and an inline-record data
// constructor (which records the owning DataType, not a struct — that's the
// data/tagged-union work).
func (l *lowerer) lowerStructInstanceExpr(block *ir.Block, e *ast.StructInstanceExpr) (value.Value, *ir.Block, error) {
	if e.BaseStruct != nil {
		return nil, nil, fmt.Errorf("llvm: struct record-update syntax not implemented yet (%q)", e.Name)
	}
	recorded, ok := l.res.TypeTable.Get(e)
	if !ok {
		return nil, nil, fmt.Errorf("llvm: no type recorded for struct instance %q", e.Name)
	}
	structType, ok := recorded.(types.NamedStructType)
	if !ok {
		return nil, nil, fmt.Errorf("llvm: struct instance lowering not implemented for %s", recorded)
	}
	llType, err := l.lowerType(structType)
	if err != nil {
		return nil, nil, err
	}
	structTy, ok := llType.(*lltypes.StructType)
	if !ok {
		return nil, nil, fmt.Errorf("llvm: struct type %s did not lower to a struct", structType.Name)
	}

	// Key each supplied field's value by name (a positional literal field — Name
	// "" — takes the declared name at that position), then build in declared order.
	valueByName := make(map[string]ast.Expression, len(e.Fields))
	for i, f := range e.Fields {
		name := f.Name
		if name == "" {
			name = structType.Fields[i].Name
		}
		valueByName[name] = f.Value
	}

	var agg value.Value = constant.NewUndef(structTy)
	for i, declField := range structType.Fields {
		valExpr, ok := valueByName[declField.Name]
		if !ok {
			return nil, nil, fmt.Errorf("llvm: struct %s field %q has no value (default values not implemented yet)", structType.Name, declField.Name)
		}
		var v value.Value
		v, block, err = l.lowerExpr(block, valExpr)
		if err != nil {
			return nil, nil, err
		}
		agg = block.NewInsertValue(agg, v, uint64(i))
	}
	return agg, block, nil
}

// lowerMemberExpr lowers struct field access (`node.value`) to an `extractvalue`
// on the object's struct value. The field's position comes from the object's
// declared struct type (looked up by name), since the LLVM struct type carries
// no field names. A method call (`obj.method()`) never reaches here — it's a
// FunctionCallExpr whose callee is the MemberExpr — so this is field access only.
func (l *lowerer) lowerMemberExpr(block *ir.Block, e *ast.MemberExpr) (value.Value, *ir.Block, error) {
	if e.Optional {
		return nil, nil, fmt.Errorf("llvm: optional member access (?.) not implemented yet")
	}
	objType, ok := l.res.TypeTable.Get(e.Object)
	if !ok {
		return nil, nil, fmt.Errorf("llvm: no type recorded for member-access object")
	}
	fields, ok := l.namedStructFields(objType)
	if !ok {
		return nil, nil, fmt.Errorf("llvm: field access on non-struct type %s not implemented", objType)
	}
	idx := -1
	for i, f := range fields {
		if f.Name == e.Property.Name {
			idx = i
			break
		}
	}
	if idx < 0 {
		return nil, nil, fmt.Errorf("llvm: struct has no field %q", e.Property.Name)
	}
	obj, block, err := l.lowerExpr(block, e.Object)
	if err != nil {
		return nil, nil, err
	}
	if _, ok := obj.Type().(*lltypes.StructType); !ok {
		return nil, nil, fmt.Errorf("llvm: member access on non-struct value of type %s", obj.Type())
	}
	return block.NewExtractValue(obj, uint64(idx)), block, nil
}

// namedStructFields returns the declared fields (name + order) of a named-struct
// type. It resolves an UnresolvedType — which is how a field or binding typed as
// another named struct is recorded — through the symbol table, so nested field
// access (`line.start.x`) finds the inner struct's fields too.
func (l *lowerer) namedStructFields(t types.Type) ([]types.StructField, bool) {
	switch s := t.(type) {
	case types.NamedStructType:
		return s.Fields, true
	case types.UnresolvedType:
		if decl, ok := l.res.SymbolTable.Types[s.Name]; ok {
			if ns, ok := decl.Type.(types.NamedStructType); ok {
				return ns.Fields, true
			}
		}
	}
	return nil, false
}

// lowerDataConstructorExpr lowers a nullary data constructor (`Red`, `Nil`,
// `None` — a DataConstructorExpr with no payload). It records the owning DataType,
// so construction is just materializing the union with the variant's tag and no
// payload.
func (l *lowerer) lowerDataConstructorExpr(block *ir.Block, e *ast.DataConstructorExpr) (value.Value, *ir.Block, error) {
	if e.Value != nil {
		return nil, nil, fmt.Errorf("llvm: non-nullary DataConstructorExpr %q not expected here", e.Constructor)
	}
	recorded, ok := l.res.TypeTable.Get(e)
	if !ok {
		return nil, nil, fmt.Errorf("llvm: no type recorded for data constructor %q", e.Constructor)
	}
	dt, ok := recorded.(types.DataType)
	if !ok {
		return nil, nil, fmt.Errorf("llvm: data constructor %q did not record a data type (got %s)", e.Constructor, recorded)
	}
	return l.lowerDataConstruction(block, dt, e.Constructor, nil)
}

// lowerDataConstruction materializes a `data` value of type dt for the variant
// ctorName with positional argument expressions args (empty for a nullary
// variant). Following DATA_LAYOUT.md, it goes through memory rather than an SSA
// aggregate, because the payload blob (`[K x iA]`) is reinterpreted as this
// variant's payload struct: alloca the union, store the tag, and — for a variant
// with a payload — GEP the blob field, bitcast it to the variant's payload-struct
// pointer, and store the built payload struct. The union value is then loaded back
// as a first-class value so it flows through `let`/calls like a tuple or struct.
//
// A `shared` payload field (a recursive variant like `Cons(i64, shared List)`)
// needs ref-counted-box allocation (ALLOCATION.md), which isn't lowered yet, so
// it errors loudly. Inline-record variants (`Node { … }`) route through
// lowerStructInstanceExpr and are deferred there.
func (l *lowerer) lowerDataConstruction(block *ir.Block, dt types.DataType, ctorName string, args []ast.Expression) (value.Value, *ir.Block, error) {
	tag := -1
	var ctor types.DataTypeConstructor
	for i, c := range dt.Constructors {
		if c.Name == ctorName {
			tag, ctor = i, c
			break
		}
	}
	if tag < 0 {
		return nil, nil, fmt.Errorf("llvm: data type %q has no constructor %q", dt.Name, ctorName)
	}
	fields := ctor.FieldTypes()
	if len(args) != len(fields) {
		return nil, nil, fmt.Errorf("llvm: constructor %q expects %d argument(s), got %d", ctorName, len(fields), len(args))
	}

	llType, err := l.lowerType(dt)
	if err != nil {
		return nil, nil, err
	}
	unionTy, ok := llType.(*lltypes.StructType)
	if !ok {
		return nil, nil, fmt.Errorf("llvm: data type %q did not lower to a struct", dt.Name)
	}

	// Alloca the union in the entry block (mem2reg-promotable), then fill it.
	entry := block.Parent.Blocks[0]
	slot := entry.NewAlloca(unionTy)

	// Store the tag (field 0).
	tagTy := unionTy.Fields[0].(*lltypes.IntType)
	tagPtr := block.NewGetElementPtr(unionTy, slot,
		constant.NewInt(lltypes.I32, 0), constant.NewInt(lltypes.I32, 0))
	block.NewStore(constant.NewInt(tagTy, int64(tag)), tagPtr)

	// Store the payload (field 1, the blob) reinterpreted as this variant's
	// payload struct — only when the variant carries fields (a nullary variant of
	// a type that *has* payloads just leaves the blob undefined).
	if len(fields) > 0 {
		payloadStructTy, err := l.dataPayloadStructType(ctor)
		if err != nil {
			return nil, nil, err
		}
		var payload value.Value = constant.NewUndef(payloadStructTy)
		for i, argExpr := range args {
			var v value.Value
			v, block, err = l.lowerExpr(block, argExpr)
			if err != nil {
				return nil, nil, err
			}
			v, err = l.coerceAggregateElem(block, v, payloadStructTy.Fields[i], argExpr)
			if err != nil {
				return nil, nil, err
			}
			payload = block.NewInsertValue(payload, v, uint64(i))
		}
		blobPtr := block.NewGetElementPtr(unionTy, slot,
			constant.NewInt(lltypes.I32, 0), constant.NewInt(lltypes.I32, 1))
		typedPtr := block.NewBitCast(blobPtr, lltypes.NewPointer(payloadStructTy))
		block.NewStore(payload, typedPtr)
	}

	return block.NewLoad(unionTy, slot), block, nil
}

// dataPayloadStructType is the LLVM struct of a variant's payload fields, in
// order — what gets stored into (and later read from) the union's payload blob.
// A `shared` field is deferred (it needs ref-counted-box allocation).
func (l *lowerer) dataPayloadStructType(ctor types.DataTypeConstructor) (*lltypes.StructType, error) {
	fieldTypes := ctor.FieldTypes()
	fields := make([]lltypes.Type, len(fieldTypes))
	for i, p := range fieldTypes {
		if types.AllocationOf(p) == types.Shared {
			return nil, fmt.Errorf("llvm: `shared` payload field in constructor %q not implemented yet (needs ref-counted allocation)", ctor.Name)
		}
		ft, err := l.lowerType(p)
		if err != nil {
			return nil, err
		}
		fields[i] = ft
	}
	return lltypes.NewStruct(fields...), nil
}

// resolveDataType resolves a scrutinee type to its DataType — directly, or via
// the symbol table for an UnresolvedType naming a non-generic data type. A
// generic (ParameterizedType) data type is not handled (monomorphization TODO).
func (l *lowerer) resolveDataType(t types.Type) (types.DataType, bool) {
	switch v := t.(type) {
	case types.DataType:
		return v, true
	case types.UnresolvedType:
		if decl, ok := l.res.SymbolTable.Types[v.Name]; ok {
			if dt, ok := decl.Type.(types.DataType); ok {
				return dt, true
			}
		}
	}
	return types.DataType{}, false
}

// lowerMatch lowers a `match` expression, dispatching on the scrutinee kind:
// `data` (tag switch, or the if-else ladder when a payload value test or a guard
// rules the switch out), struct/tuple (shared aggregate ladder), and bool/integer
// scalar (comparison ladder). String, float, and array scrutinees are deferred
// with a loud error (those types don't lower at all).
func (l *lowerer) lowerMatch(block *ir.Block, e *ast.MatchExpr) (value.Value, *ir.Block, error) {
	scrutType, ok := l.res.TypeTable.Get(e.Scrutinee)
	if !ok {
		return nil, nil, fmt.Errorf("llvm: no type recorded for match scrutinee")
	}
	if dt, ok := l.resolveDataType(scrutType); ok {
		return l.lowerDataMatch(block, e, dt)
	}
	if st, ok := l.resolveStructType(scrutType); ok {
		return l.lowerStructMatch(block, e, st)
	}
	if tt, ok := l.resolveTupleType(scrutType); ok {
		return l.lowerTupleMatch(block, e, tt)
	}
	// A scalar scrutinee (bool or a concrete integer) lowers to an if-else ladder
	// of comparisons. Detected by whether the scrutinee's primitive maps to an
	// LLVM integer type (i1 for bool, iN for ints) — float and string fall through.
	if prim, ok := scrutType.(types.PrimitiveType); ok {
		if ll, ok := LLVMPrimitive(prim.Name); ok {
			if _, isInt := ll.(*lltypes.IntType); isInt {
				return l.lowerScalarMatch(block, e, prim)
			}
		}
	}
	return nil, nil, fmt.Errorf("llvm: match on %s not implemented yet (only data types, structs, tuples, and integer/bool scalars)", scrutType)
}

// lowerScalarMatch lowers a `match` on a bool or integer scrutinee as an if-else
// ladder: each non-catch-all arm becomes a comparison that cond-brs to the arm
// body or on to the next test, in source order (first match wins). A wildcard or
// identifier arm is an unconditional match that ends the ladder (an identifier
// binds the scrutinee value); later arms are unreachable. An `if` guard adds a
// second test after the pattern (via lowerGuardedArmBody): when it fails, control
// falls through to the next arm, so a *guarded* catch-all doesn't end the ladder.
// Arm bodies feed a merge phi, so the match is a value like `if`.
//
// (A pure-literal match would lower more compactly to an LLVM `switch`; the
// ladder is used uniformly because a range pattern — `0..<10` — isn't a single
// switch case. The optimizer recovers the switch for the literal-only shape.)
//
// The fall-through past the last test is `unreachable`: a bool match covering
// true/false, or an exhaustive integer match, never reaches it; a non-exhaustive
// integer match was already warned by the typechecker.
// matchHasGuard reports whether any arm carries an `if` guard.
func matchHasGuard(e *ast.MatchExpr) bool {
	for _, arm := range e.MatchArms {
		if arm.Guard != nil {
			return true
		}
	}
	return false
}

// lowerGuardedArmBody lowers an arm's body starting from `matched` — a block in
// which the pattern has already matched and its bindings are installed. With no
// guard it lowers the body directly there. With a guard it evaluates the guard
// condition in `matched` (the bindings are in scope, so `Some(x) if x > 0` works)
// and cond-branches to the body (guard true) or to `next` (guard false — so the
// following arm is tried, exactly as a failed pattern test does). `lowerBody` is
// the ladder's per-arm epilogue: lower the body and wire its value into the merge
// phi.
func (l *lowerer) lowerGuardedArmBody(matched *ir.Block, guard *ast.GuardExpr, body ast.Expression, next *ir.Block, lowerBody func(*ir.Block, ast.Expression) error) error {
	if guard == nil {
		return lowerBody(matched, body)
	}
	guardVal, gEnd, err := l.lowerExpr(matched, guard.Condition)
	if err != nil {
		return err
	}
	bodyBlock := matched.Parent.NewBlock("")
	gEnd.NewCondBr(guardVal, bodyBlock, next)
	return lowerBody(bodyBlock, body)
}

func (l *lowerer) lowerScalarMatch(block *ir.Block, e *ast.MatchExpr, scrutPrim types.PrimitiveType) (value.Value, *ir.Block, error) {
	scrut, block, err := l.lowerExpr(block, e.Scrutinee)
	if err != nil {
		return nil, nil, err
	}
	intTy, ok := scrut.Type().(*lltypes.IntType)
	if !ok {
		return nil, nil, fmt.Errorf("llvm: scalar match scrutinee did not lower to an integer (%s)", scrut.Type())
	}
	isBool := scrutPrim.Name == types.Boolean
	signed := IsSignedInt(scrutPrim.Name)
	fn := block.Parent

	merge := fn.NewBlock("")
	type incoming struct {
		val value.Value
		end *ir.Block
	}
	var incomings []incoming
	lowerBody := func(b *ir.Block, body ast.Expression) error {
		val, end, err := l.lowerExpr(b, body)
		if err != nil {
			return err
		}
		if end.Term == nil {
			end.NewBr(merge)
			incomings = append(incomings, incoming{val, end})
		}
		return nil
	}

	current := block
	sealed := false // an unguarded catch-all consumed the fall-through
	for _, arm := range e.MatchArms {
		switch p := arm.Pattern.(type) {
		case *ast.WildcardPattern, *ast.IdentifierPattern:
			if ip, ok := p.(*ast.IdentifierPattern); ok && ip.Name != "_" {
				slot := fn.Blocks[0].NewAlloca(intTy)
				current.NewStore(scrut, slot)
				l.locals[ip.Name] = slot
			}
			if arm.Guard == nil {
				if err := lowerBody(current, arm.Body); err != nil {
					return nil, nil, err
				}
				sealed = true
			} else {
				// A guarded catch-all may fail, so it doesn't seal the ladder.
				next := fn.NewBlock("")
				if err := l.lowerGuardedArmBody(current, arm.Guard, arm.Body, next, lowerBody); err != nil {
					return nil, nil, err
				}
				current = next
			}
		default:
			cond, err := l.scalarMatchTest(current, scrut, arm.Pattern, isBool, signed)
			if err != nil {
				return nil, nil, err
			}
			bodyBlock := fn.NewBlock("")
			nextBlock := fn.NewBlock("")
			current.NewCondBr(cond, bodyBlock, nextBlock)
			if err := l.lowerGuardedArmBody(bodyBlock, arm.Guard, arm.Body, nextBlock, lowerBody); err != nil {
				return nil, nil, err
			}
			current = nextBlock
		}
		if sealed {
			break // remaining arms are unreachable after an unconditional match
		}
	}
	if !sealed {
		current.NewUnreachable()
	}

	if len(incomings) == 0 {
		return nil, merge, nil
	}
	incs := make([]*ir.Incoming, len(incomings))
	for i, in := range incomings {
		incs[i] = ir.NewIncoming(in.val, in.end)
	}
	return merge.NewPhi(incs...), merge, nil
}

// scalarMatchTest builds the i1 "does the scrutinee match this pattern?" test for
// one arm of a scalar match: an `icmp eq` for a literal, or a two-sided range
// check (`scrut >= lo && scrut </<= hi`) for a range pattern. signed selects the
// signed vs unsigned comparison predicates.
func (l *lowerer) scalarMatchTest(block *ir.Block, scrut value.Value, pattern ast.Pattern, isBool, signed bool) (value.Value, error) {
	intTy := scrut.Type().(*lltypes.IntType)
	switch p := pattern.(type) {
	case *ast.LiteralPattern:
		s, ok := p.Value.(string)
		if !ok {
			return nil, fmt.Errorf("llvm: unexpected literal pattern value %T", p.Value)
		}
		if isBool {
			var bit int64
			switch s {
			case "true":
				bit = 1
			case "false":
				bit = 0
			default:
				return nil, fmt.Errorf("llvm: non-bool literal pattern %q on a bool scrutinee", s)
			}
			return block.NewICmp(enum.IPredEQ, scrut, constant.NewInt(intTy, bit)), nil
		}
		n, err := strconv.ParseInt(s, 0, 64)
		if err != nil {
			return nil, fmt.Errorf("llvm: invalid integer literal pattern %q: %v", s, err)
		}
		return block.NewICmp(enum.IPredEQ, scrut, constant.NewInt(intTy, n)), nil
	case *ast.RangePattern:
		lo, ok := constIntFromExpr(p.Start, intTy)
		if !ok {
			return nil, fmt.Errorf("llvm: unsupported range start in match pattern")
		}
		hi, ok := constIntFromExpr(p.End, intTy)
		if !ok {
			return nil, fmt.Errorf("llvm: unsupported range end in match pattern")
		}
		gePred := enum.IPredUGE
		if signed {
			gePred = enum.IPredSGE
		}
		geLo := block.NewICmp(gePred, scrut, lo)
		var hiPred enum.IPred
		switch {
		case p.EndOperator == "<" && signed:
			hiPred = enum.IPredSLT
		case p.EndOperator == "<":
			hiPred = enum.IPredULT
		case signed:
			hiPred = enum.IPredSLE
		default:
			hiPred = enum.IPredULE
		}
		leHi := block.NewICmp(hiPred, scrut, hi)
		return block.NewAnd(geLo, leHi), nil
	default:
		return nil, fmt.Errorf("llvm: match pattern %T not implemented for a scalar scrutinee", pattern)
	}
}

// constIntFromExpr builds an integer constant of type ty from a range-bound
// expression: an integer literal, or a negated one (`-128`). Returns ok=false for
// a bound the backend can't fold at compile time.
func constIntFromExpr(e ast.Expression, ty *lltypes.IntType) (value.Value, bool) {
	switch v := e.(type) {
	case *ast.IntegerLiteralExpr:
		return constant.NewInt(ty, v.Value), true
	case *ast.NegationExpr:
		if inner, ok := v.Operand.(*ast.IntegerLiteralExpr); ok {
			return constant.NewInt(ty, -inner.Value), true
		}
	}
	return nil, false
}

// resolveStructType resolves a scrutinee type to its NamedStructType — directly,
// or via the symbol table for an UnresolvedType naming a struct.
func (l *lowerer) resolveStructType(t types.Type) (types.NamedStructType, bool) {
	switch v := t.(type) {
	case types.NamedStructType:
		return v, true
	case types.UnresolvedType:
		if decl, ok := l.res.SymbolTable.Types[v.Name]; ok {
			if st, ok := decl.Type.(types.NamedStructType); ok {
				return st, true
			}
		}
	}
	return types.NamedStructType{}, false
}

// lowerStructMatch lowers a `match` on a struct value via the shared aggregate
// ladder: a struct pattern `{ x, y }` binds fields (structPatternTest returns nil
// → unconditional), while a literal field sub-pattern (`{ x: 0, y }`) makes it
// conditional on that field.
func (l *lowerer) lowerStructMatch(block *ir.Block, e *ast.MatchExpr, st types.NamedStructType) (value.Value, *ir.Block, error) {
	scrut, block, err := l.lowerExpr(block, e.Scrutinee)
	if err != nil {
		return nil, nil, err
	}
	structTy, ok := scrut.Type().(*lltypes.StructType)
	if !ok {
		return nil, nil, fmt.Errorf("llvm: struct match scrutinee did not lower to a struct (%s)", scrut.Type())
	}
	return l.lowerAggregateMatch(block, e, scrut, structTy,
		func(b *ir.Block, pat ast.Pattern) (value.Value, error) {
			return l.aggPatternTest(b, scrut, pat, st)
		},
		func(b *ir.Block, pat ast.Pattern) error {
			return l.aggPatternBind(b, scrut, pat, st)
		},
	)
}

// lowerAggregateMatch lowers a `match` as an if-else ladder driven by the `test`
// and `bind` closures. Used for a single-shape aggregate (a struct or a tuple —
// no variant tag, so no `switch`) and, when a payload value test rules out the
// tag `switch`, for a `data` scrutinee too (its closures encode the tag check in
// `test`). A `_`/identifier arm is an unconditional catch-all (an identifier binds
// the whole value). Every other arm calls `test` for its condition in the current
// block (nil → unconditional) and, on the taken path, `bind` for its sub-pattern
// bindings; the scrutinee-specific pattern shape lives entirely in those two
// closures. An `if` guard (any arm, catch-all included) is a further test after
// binding — when it fails, control falls through to the next arm, so a guarded
// arm never seals the ladder (lowerGuardedArmBody). Arms feed a merge phi, so the
// match is a value like `if`; the unmatched fall-through is `unreachable`.
func (l *lowerer) lowerAggregateMatch(
	block *ir.Block, e *ast.MatchExpr, scrut value.Value, aggTy *lltypes.StructType,
	test func(b *ir.Block, pat ast.Pattern) (value.Value, error),
	bind func(b *ir.Block, pat ast.Pattern) error,
) (value.Value, *ir.Block, error) {
	fn := block.Parent
	merge := fn.NewBlock("")
	type incoming struct {
		val value.Value
		end *ir.Block
	}
	var incomings []incoming

	lowerArmInto := func(b *ir.Block, body ast.Expression) error {
		val, end, err := l.lowerExpr(b, body)
		if err != nil {
			return err
		}
		if end.Term == nil {
			end.NewBr(merge)
			incomings = append(incomings, incoming{val, end})
		}
		return nil
	}

	current := block
	sealed := false
	for _, arm := range e.MatchArms {
		if ip, isCatchAll := matchCatchAll(arm.Pattern); isCatchAll {
			if ip != nil { // an identifier catch-all binds the whole aggregate value
				slot := fn.Blocks[0].NewAlloca(aggTy)
				current.NewStore(scrut, slot)
				l.locals[ip.Name] = slot
			}
			if arm.Guard == nil {
				if err := lowerArmInto(current, arm.Body); err != nil {
					return nil, nil, err
				}
				sealed = true
				break
			}
			// A guarded catch-all may fail, so it doesn't seal the ladder.
			next := fn.NewBlock("")
			if err := l.lowerGuardedArmBody(current, arm.Guard, arm.Body, next, lowerArmInto); err != nil {
				return nil, nil, err
			}
			current = next
			continue
		}
		cond, err := test(current, arm.Pattern)
		if err != nil {
			return nil, nil, err
		}
		if cond == nil { // no literal sub-pattern → the arm's pattern always matches
			if err := bind(current, arm.Pattern); err != nil {
				return nil, nil, err
			}
			if arm.Guard == nil {
				if err := lowerArmInto(current, arm.Body); err != nil {
					return nil, nil, err
				}
				sealed = true
				break
			}
			// The pattern matched unconditionally, but a guard can still fail — bind
			// its variables (done above) then test the guard, falling to `next` if false.
			next := fn.NewBlock("")
			if err := l.lowerGuardedArmBody(current, arm.Guard, arm.Body, next, lowerArmInto); err != nil {
				return nil, nil, err
			}
			current = next
			continue
		}
		bodyBlock := fn.NewBlock("")
		nextBlock := fn.NewBlock("")
		current.NewCondBr(cond, bodyBlock, nextBlock)
		if err := bind(bodyBlock, arm.Pattern); err != nil {
			return nil, nil, err
		}
		if err := l.lowerGuardedArmBody(bodyBlock, arm.Guard, arm.Body, nextBlock, lowerArmInto); err != nil {
			return nil, nil, err
		}
		current = nextBlock
	}
	if !sealed {
		current.NewUnreachable()
	}

	if len(incomings) == 0 {
		return nil, merge, nil
	}
	incs := make([]*ir.Incoming, len(incomings))
	for i, in := range incomings {
		incs[i] = ir.NewIncoming(in.val, in.end)
	}
	return merge.NewPhi(incs...), merge, nil
}

// matchCatchAll reports whether a pattern is an unconditional catch-all (a
// wildcard or an identifier). The returned *IdentifierPattern is non-nil only for
// a real binding identifier (name != "_") — the name the whole scrutinee value
// binds to — and nil for a wildcard or `_`.
func matchCatchAll(pat ast.Pattern) (*ast.IdentifierPattern, bool) {
	switch p := pat.(type) {
	case *ast.WildcardPattern:
		return nil, true
	case *ast.IdentifierPattern:
		if p.Name == "_" {
			return nil, true
		}
		return p, true
	}
	return nil, false
}

// aggPatternTest builds the i1 condition that a first-class struct/tuple value
// `val` (of Lyra type valType) matches `pat` — the AND of a scalar comparison per
// literal/range sub-pattern, recursing into nested struct/tuple sub-patterns via
// `extractvalue` (safe on a single-shape aggregate, no tag/branch needed). Returns
// nil when the pattern imposes no test (all identifier/wildcard/shorthand
// bindings). A nested `data` sub-pattern contributes its tag check plus a test for
// each value-testing payload field (`Some(0)`), computed branchlessly and ANDed.
func (l *lowerer) aggPatternTest(block *ir.Block, val value.Value, pat ast.Pattern, valType types.Type) (value.Value, error) {
	switch p := pat.(type) {
	case nil, *ast.WildcardPattern, *ast.IdentifierPattern:
		return nil, nil // binding leaves impose no test
	case *ast.LiteralPattern, *ast.RangePattern:
		prim, ok := valType.(types.PrimitiveType)
		if !ok {
			return nil, fmt.Errorf("llvm: literal pattern on non-scalar value of type %s", valType)
		}
		return l.scalarMatchTest(block, val, pat, prim.Name == types.Boolean, IsSignedInt(prim.Name))
	case *ast.StructPattern:
		st, ok := l.resolveStructType(valType)
		if !ok {
			return nil, fmt.Errorf("llvm: struct pattern on non-struct value of type %s", valType)
		}
		var cond value.Value
		for _, f := range p.Fields {
			if isBindingLeaf(f.Pattern) {
				continue
			}
			idx, ftype, ok := structFieldIndexAndType(st, f.Name)
			if !ok {
				return nil, fmt.Errorf("llvm: struct %s has no field %q", st.Name, f.Name)
			}
			c, err := l.aggPatternTest(block, block.NewExtractValue(val, uint64(idx)), f.Pattern, ftype)
			if err != nil {
				return nil, err
			}
			cond = andConds(block, cond, c)
		}
		return cond, nil
	case *ast.TuplePattern:
		tt, ok := l.resolveTupleType(valType)
		if !ok {
			return nil, fmt.Errorf("llvm: tuple pattern on non-tuple value of type %s", valType)
		}
		var cond value.Value
		for i, el := range p.Elements {
			if isBindingLeaf(el) {
				continue
			}
			if i >= len(tt.Elements) {
				return nil, fmt.Errorf("llvm: tuple pattern element %d out of range", i)
			}
			c, err := l.aggPatternTest(block, block.NewExtractValue(val, uint64(i)), el, tt.Elements[i])
			if err != nil {
				return nil, err
			}
			cond = andConds(block, cond, c)
		}
		return cond, nil
	case *ast.DataPattern:
		// A `data` sub-pattern's test is its tag check (`extractvalue`-the-tag ==
		// the variant index), ANDed with a value test for each payload field that
		// imposes one (`Some(0)`, or a nested data pattern). The payload test is
		// computed unconditionally and ANDed after the tag check: when the tag
		// doesn't match, the payload blob reinterpreted as this variant is
		// meaningless, but the tag comparison has already forced the whole
		// condition false — so reading those bits is harmless (they stay within the
		// union's own stack blob, sized to the largest variant).
		dt, ok := l.resolveDataType(valType)
		if !ok {
			return nil, fmt.Errorf("llvm: data pattern on non-data value of type %s", valType)
		}
		ctor, idx, ok := findConstructor(dt, p.Name)
		if !ok {
			return nil, fmt.Errorf("llvm: %q is not a constructor of %s", p.Name, dt.Name)
		}
		unionSt, ok := val.Type().(*lltypes.StructType)
		if !ok {
			return nil, fmt.Errorf("llvm: data value did not lower to a struct (%s)", val.Type())
		}
		tagTy := unionSt.Fields[0].(*lltypes.IntType)
		tag := block.NewExtractValue(val, 0)
		cond := value.Value(block.NewICmp(enum.IPredEQ, tag, constant.NewInt(tagTy, int64(idx))))

		fieldPatterns, err := payloadFieldPatterns(p, ctor)
		if err != nil {
			return nil, err
		}
		if slices.ContainsFunc(fieldPatterns, patternHasTest) {
			payload, err := l.extractDataPayload(block, val, ctor)
			if err != nil {
				return nil, err
			}
			fieldTypes := ctor.FieldTypes()
			for i, fp := range fieldPatterns {
				c, err := l.aggPatternTest(block, block.NewExtractValue(payload, uint64(i)), fp, fieldTypes[i])
				if err != nil {
					return nil, err
				}
				cond = andConds(block, cond, c)
			}
		}
		return cond, nil
	default:
		return nil, fmt.Errorf("llvm: match sub-pattern %T not implemented yet", pat)
	}
}

// aggPatternBind binds the identifier leaves of a struct/tuple pattern into
// l.locals for the arm body, recursing into nested struct/tuple sub-patterns via
// `extractvalue`. The alloca type is the extracted value's own LLVM type, so nested
// aggregate fields bind correctly. Literal/range/wildcard sub-patterns bind
// nothing; a nested `data` sub-pattern is deferred.
func (l *lowerer) aggPatternBind(block *ir.Block, val value.Value, pat ast.Pattern, valType types.Type) error {
	switch p := pat.(type) {
	case *ast.IdentifierPattern:
		if p.Name != "_" {
			l.bindValue(block, p.Name, val)
		}
		return nil
	case nil, *ast.WildcardPattern, *ast.LiteralPattern, *ast.RangePattern:
		return nil // no binding
	case *ast.StructPattern:
		st, ok := l.resolveStructType(valType)
		if !ok {
			return fmt.Errorf("llvm: struct pattern on non-struct value of type %s", valType)
		}
		for _, f := range p.Fields {
			idx, ftype, ok := structFieldIndexAndType(st, f.Name)
			if !ok {
				return fmt.Errorf("llvm: struct %s has no field %q", st.Name, f.Name)
			}
			fieldVal := block.NewExtractValue(val, uint64(idx))
			if f.Pattern == nil { // shorthand `{ x }` binds x
				l.bindValue(block, f.Name, fieldVal)
				continue
			}
			if err := l.aggPatternBind(block, fieldVal, f.Pattern, ftype); err != nil {
				return err
			}
		}
		return nil
	case *ast.TuplePattern:
		tt, ok := l.resolveTupleType(valType)
		if !ok {
			return fmt.Errorf("llvm: tuple pattern on non-tuple value of type %s", valType)
		}
		for i, el := range p.Elements {
			if i >= len(tt.Elements) {
				return fmt.Errorf("llvm: tuple pattern element %d out of range", i)
			}
			if err := l.aggPatternBind(block, block.NewExtractValue(val, uint64(i)), el, tt.Elements[i]); err != nil {
				return err
			}
		}
		return nil
	case *ast.DataPattern:
		// Reached only on the taken path (aggPatternTest's tag check already held),
		// so bind the payload: reinterpret it as the variant's payload struct and
		// recurse into each field's sub-pattern.
		dt, ok := l.resolveDataType(valType)
		if !ok {
			return fmt.Errorf("llvm: data pattern on non-data value of type %s", valType)
		}
		ctor, _, ok := findConstructor(dt, p.Name)
		if !ok {
			return fmt.Errorf("llvm: %q is not a constructor of %s", p.Name, dt.Name)
		}
		fieldPatterns, err := payloadFieldPatterns(p, ctor)
		if err != nil {
			return err
		}
		if len(fieldPatterns) == 0 {
			return nil // nullary variant
		}
		payload, err := l.extractDataPayload(block, val, ctor)
		if err != nil {
			return err
		}
		fieldTypes := ctor.FieldTypes()
		for i, fp := range fieldPatterns {
			if err := l.aggPatternBind(block, block.NewExtractValue(payload, uint64(i)), fp, fieldTypes[i]); err != nil {
				return err
			}
		}
		return nil
	default:
		return fmt.Errorf("llvm: match sub-pattern %T binding not implemented yet", pat)
	}
}

// bindValue stores val into a fresh entry-block alloca and records it under name
// in l.locals, so the arm body reads the binding like any local.
func (l *lowerer) bindValue(block *ir.Block, name string, val value.Value) {
	slot := block.Parent.Blocks[0].NewAlloca(val.Type())
	block.NewStore(val, slot)
	l.locals[name] = slot
}

// andConds combines two optional i1 conditions (nil = "always true"), returning
// nil when both are nil, the other when one is nil, or their `and`.
func andConds(block *ir.Block, a, b value.Value) value.Value {
	switch {
	case a == nil:
		return b
	case b == nil:
		return a
	default:
		return block.NewAnd(a, b)
	}
}

// isBindingLeaf reports whether a sub-pattern only binds (or ignores) and imposes
// no test: a shorthand field (nil), a wildcard, or an identifier.
func isBindingLeaf(pat ast.Pattern) bool {
	switch pat.(type) {
	case nil, *ast.WildcardPattern, *ast.IdentifierPattern:
		return true
	}
	return false
}

// structFieldIndexAndType returns a struct field's position and declared type by
// name (the type may be an UnresolvedType for a nested named type — resolved by
// the recursive caller).
func structFieldIndexAndType(st types.NamedStructType, name string) (int, types.Type, bool) {
	for i, f := range st.Fields {
		if f.Name == name {
			return i, f.Type, true
		}
	}
	return 0, nil, false
}

// resolveTupleType resolves a scrutinee type to its TupleType — directly (named
// or anonymous), or via the symbol table for an UnresolvedType naming one.
func (l *lowerer) resolveTupleType(t types.Type) (types.TupleType, bool) {
	switch v := t.(type) {
	case types.TupleType:
		return v, true
	case types.UnresolvedType:
		if decl, ok := l.res.SymbolTable.Types[v.Name]; ok {
			if tt, ok := decl.Type.(types.TupleType); ok {
				return tt, true
			}
		}
	}
	return types.TupleType{}, false
}

// lowerTupleMatch lowers a `match` on a tuple value via the shared aggregate
// ladder — the positional counterpart to lowerStructMatch: a tuple pattern
// `(a, b)` binds elements by position, and a literal element (`(0, b)`) makes the
// arm conditional on that position.
func (l *lowerer) lowerTupleMatch(block *ir.Block, e *ast.MatchExpr, tt types.TupleType) (value.Value, *ir.Block, error) {
	scrut, block, err := l.lowerExpr(block, e.Scrutinee)
	if err != nil {
		return nil, nil, err
	}
	structTy, ok := scrut.Type().(*lltypes.StructType)
	if !ok {
		return nil, nil, fmt.Errorf("llvm: tuple match scrutinee did not lower to a struct (%s)", scrut.Type())
	}
	return l.lowerAggregateMatch(block, e, scrut, structTy,
		func(b *ir.Block, pat ast.Pattern) (value.Value, error) {
			return l.aggPatternTest(b, scrut, pat, tt)
		},
		func(b *ir.Block, pat ast.Pattern) error {
			return l.aggPatternBind(b, scrut, pat, tt)
		},
	)
}

// dataMatchHasPayloadTest reports whether any arm of a `data` match imposes a
// value test on a payload sub-pattern (`Some(0)`, `Some(Wrapped(0))`) — i.e. a
// test beyond the variant tag. Such a match can't lower to a single tag `switch`
// (two same-tag arms need distinct payload tests) and instead uses the if-else
// ladder. A non-data or unknown-constructor arm contributes no payload test here.
func dataMatchHasPayloadTest(e *ast.MatchExpr, dt types.DataType) bool {
	for _, arm := range e.MatchArms {
		dp, ok := arm.Pattern.(*ast.DataPattern)
		if !ok {
			continue
		}
		ctor, _, ok := findConstructor(dt, dp.Name)
		if !ok {
			continue
		}
		fps, err := payloadFieldPatterns(dp, ctor)
		if err != nil {
			continue
		}
		if slices.ContainsFunc(fps, patternHasTest) {
			return true
		}
	}
	return false
}

// lowerDataMatch lowers a `match` on a `data` value: store the scrutinee, load
// its tag, and `switch` on it to one block per arm (DATA_LAYOUT.md). Each data
// pattern's arm reinterprets the payload blob as its variant's payload struct and
// binds the fields; a wildcard/identifier arm is the switch default. The arms
// feed a merge phi, so the match is a value (like `if`). The front-end guarantees
// exhaustiveness (lyra-E009), so a match with no catch-all gets an `unreachable`
// default.
func (l *lowerer) lowerDataMatch(block *ir.Block, e *ast.MatchExpr, dt types.DataType) (value.Value, *ir.Block, error) {
	scrut, block, err := l.lowerExpr(block, e.Scrutinee)
	if err != nil {
		return nil, nil, err
	}
	unionTy, ok := scrut.Type().(*lltypes.StructType)
	if !ok {
		return nil, nil, fmt.Errorf("llvm: data match scrutinee did not lower to a struct (%s)", scrut.Type())
	}

	// The compact tag `switch` below routes each tag to exactly one block, so it
	// can't express a match where two arms share a tag but differ — a value-testing
	// payload sub-pattern (`Some(0)` vs `Some(x)`), or a guard that may fail and
	// fall through to a following same-tag arm. Either case falls back to the
	// if-else ladder shared with struct/tuple matches, where each arm's condition is
	// the tag check ANDed with its payload tests (aggPatternTest) and then its
	// guard, first-match-wins preserved.
	if dataMatchHasPayloadTest(e, dt) || matchHasGuard(e) {
		return l.lowerAggregateMatch(block, e, scrut, unionTy,
			func(b *ir.Block, pat ast.Pattern) (value.Value, error) {
				return l.aggPatternTest(b, scrut, pat, dt)
			},
			func(b *ir.Block, pat ast.Pattern) error {
				return l.aggPatternBind(b, scrut, pat, dt)
			},
		)
	}

	fn := block.Parent

	// Store the scrutinee so a variant's payload can be reinterpreted out of the
	// blob (the mirror of construction), and load its tag.
	slot := fn.Blocks[0].NewAlloca(unionTy)
	block.NewStore(scrut, slot)
	tagTy := unionTy.Fields[0].(*lltypes.IntType)
	tagPtr := block.NewGetElementPtr(unionTy, slot, constant.NewInt(lltypes.I32, 0), constant.NewInt(lltypes.I32, 0))
	tag := block.NewLoad(tagTy, tagPtr)

	mergeBlock := fn.NewBlock("")
	type incoming struct {
		val value.Value
		end *ir.Block
	}
	var incomings []incoming
	var cases []*ir.Case
	var defaultBlock *ir.Block

	for _, arm := range e.MatchArms {
		armBlock := fn.NewBlock("")
		switch p := arm.Pattern.(type) {
		case *ast.DataPattern:
			idx := -1
			for i, c := range dt.Constructors {
				if c.Name == p.Name {
					idx = i
					break
				}
			}
			if idx < 0 {
				return nil, nil, fmt.Errorf("llvm: %q is not a constructor of %s", p.Name, dt.Name)
			}
			if err := l.bindDataPayload(armBlock, p, dt.Constructors[idx], slot, unionTy); err != nil {
				return nil, nil, err
			}
			cases = append(cases, ir.NewCase(constant.NewInt(tagTy, int64(idx)), armBlock))
		case *ast.WildcardPattern:
			defaultBlock = armBlock
		case *ast.IdentifierPattern:
			if p.Name != "_" {
				// Bind the whole scrutinee value; slot already holds it.
				l.locals[p.Name] = slot
			}
			defaultBlock = armBlock
		default:
			return nil, nil, fmt.Errorf("llvm: match pattern %T not implemented for a data scrutinee", arm.Pattern)
		}

		val, end, err := l.lowerExpr(armBlock, arm.Body)
		if err != nil {
			return nil, nil, err
		}
		if end.Term == nil {
			end.NewBr(mergeBlock)
			incomings = append(incomings, incoming{val, end})
		}
	}

	if defaultBlock == nil {
		// Exhaustive over the constructors, so the default is unreachable.
		defaultBlock = fn.NewBlock("")
		defaultBlock.NewUnreachable()
	}
	block.NewSwitch(tag, defaultBlock, cases...)

	if len(incomings) == 0 {
		return nil, mergeBlock, nil // every arm diverged (e.g. all `return`)
	}
	incs := make([]*ir.Incoming, len(incomings))
	for i, in := range incomings {
		incs[i] = ir.NewIncoming(in.val, in.end)
	}
	return mergeBlock.NewPhi(incs...), mergeBlock, nil
}

// bindDataPayload binds a data pattern's payload sub-patterns into l.locals for
// the arm body. It reinterprets the union's payload blob as the variant's payload
// struct (bitcast + load) and binds each field's sub-pattern via aggPatternBind,
// so a payload that is (or contains) a struct/tuple is destructured recursively
// (`W((a, b))`, `Some({ x, y })`). A *value-testing* payload sub-pattern (a
// literal, or a nested data pattern) is deferred: this arm was already selected by
// the tag switch, which can't also test the payload and fall through to another
// arm (see patternHasTest).
func (l *lowerer) bindDataPayload(armBlock *ir.Block, p *ast.DataPattern, ctor types.DataTypeConstructor, slot value.Value, unionTy *lltypes.StructType) error {
	fieldPatterns, err := payloadFieldPatterns(p, ctor)
	if err != nil {
		return err
	}
	if len(fieldPatterns) == 0 {
		return nil // nullary variant — nothing to bind
	}
	for _, fp := range fieldPatterns {
		if patternHasTest(fp) {
			return fmt.Errorf("llvm: a value-testing payload sub-pattern (%T) in a `data` match arm is not implemented yet", fp)
		}
	}
	payloadStructTy, err := l.dataPayloadStructType(ctor)
	if err != nil {
		return err
	}
	blobPtr := armBlock.NewGetElementPtr(unionTy, slot, constant.NewInt(lltypes.I32, 0), constant.NewInt(lltypes.I32, 1))
	typedPtr := armBlock.NewBitCast(blobPtr, lltypes.NewPointer(payloadStructTy))
	payload := armBlock.NewLoad(payloadStructTy, typedPtr)

	fieldTypes := ctor.FieldTypes()
	for i, fp := range fieldPatterns {
		if err := l.aggPatternBind(armBlock, armBlock.NewExtractValue(payload, uint64(i)), fp, fieldTypes[i]); err != nil {
			return err
		}
	}
	return nil
}

// extractDataPayload reinterprets a first-class data value's payload blob as the
// given variant's payload struct, returning the loaded payload struct value. It
// goes through memory (alloca + store + bitcast + load), the mirror of
// construction — the same move as lowerDataMatch, but on an arbitrary value rather
// than the match scrutinee's pre-stored slot, so it works for a data value nested
// inside another pattern.
func (l *lowerer) extractDataPayload(block *ir.Block, val value.Value, ctor types.DataTypeConstructor) (value.Value, error) {
	payloadStructTy, err := l.dataPayloadStructType(ctor)
	if err != nil {
		return nil, err
	}
	unionSt, ok := val.Type().(*lltypes.StructType)
	if !ok {
		return nil, fmt.Errorf("llvm: data value did not lower to a struct (%s)", val.Type())
	}
	slot := block.Parent.Blocks[0].NewAlloca(unionSt)
	block.NewStore(val, slot)
	blobPtr := block.NewGetElementPtr(unionSt, slot, constant.NewInt(lltypes.I32, 0), constant.NewInt(lltypes.I32, 1))
	typedPtr := block.NewBitCast(blobPtr, lltypes.NewPointer(payloadStructTy))
	return block.NewLoad(payloadStructTy, typedPtr), nil
}

// findConstructor returns a data type's constructor by name, plus its
// declaration-order index (the variant tag).
func findConstructor(dt types.DataType, name string) (types.DataTypeConstructor, int, bool) {
	for i, c := range dt.Constructors {
		if c.Name == name {
			return c, i, true
		}
	}
	return types.DataTypeConstructor{}, 0, false
}

// patternHasTest reports whether a pattern imposes a runtime value test (rather
// than only binding/ignoring): a literal or range, a data pattern (a tag check),
// or any aggregate containing one. Used to defer a payload sub-pattern that a
// tag-switch `data` arm can't test-and-fall-through on.
func patternHasTest(pat ast.Pattern) bool {
	switch p := pat.(type) {
	case *ast.LiteralPattern, *ast.RangePattern, *ast.DataPattern:
		return true
	case *ast.StructPattern:
		return slices.ContainsFunc(p.Fields, func(f ast.StructPatternField) bool {
			return patternHasTest(f.Pattern)
		})
	case *ast.TuplePattern:
		return slices.ContainsFunc(p.Elements, patternHasTest)
	}
	return false
}

// payloadFieldPatterns returns the flat list of sub-patterns, one per payload
// field, for a data pattern — or an error for a form the backend doesn't bind
// yet. Flat positional (`Rect(w, h)` against `[i64, i64]`, `Circle(r)` against
// `[i64]`) and bare single (`Some x`) are supported; tuple-payload destructuring
// (`MkPair((x, y))`) is deferred.
func payloadFieldPatterns(p *ast.DataPattern, ctor types.DataTypeConstructor) ([]ast.Pattern, error) {
	flat := ctor.FieldTypes()
	if p.Pattern == nil {
		if len(flat) != 0 {
			return nil, fmt.Errorf("llvm: constructor %q has a payload but the pattern binds none", p.Name)
		}
		return nil, nil
	}
	if tp, ok := p.Pattern.(*ast.TuplePattern); ok {
		if len(tp.Elements) == len(flat) {
			return tp.Elements, nil
		}
		return nil, fmt.Errorf("llvm: tuple-payload destructuring for %q not implemented yet", p.Name)
	}
	if len(flat) == 1 {
		return []ast.Pattern{p.Pattern}, nil
	}
	return nil, fmt.Errorf("llvm: payload pattern for %q not implemented yet", p.Name)
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

// literalFloatType returns the LLVM float type a float literal should lower at:
// the concrete width the typechecker recorded for it (via context-directed
// literal-width inference), or double (f64) when the literal has no resolved
// context — matching the language's untyped-float default. The integer analogue
// is literalIntType.
func (l *lowerer) literalFloatType(e ast.Expression) *lltypes.FloatType {
	if t, ok := l.res.TypeTable.Get(e); ok {
		if p, ok := t.(types.PrimitiveType); ok {
			if ll, ok := LLVMPrimitive(p.Name); ok {
				if ft, ok := ll.(*lltypes.FloatType); ok {
					return ft
				}
			}
		}
	}
	return lltypes.Double
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
	if _, isFloat := left.Type().(*lltypes.FloatType); isFloat {
		v, err := l.lowerFloatComparison(block, e.Operator, left, right)
		return v, block, err
	}
	// Integer comparisons (bool `==`/`!=` included — i1 is an integer type).
	// `icmp` requires both operands to have the same integer type, so a width
	// mismatch is reported explicitly rather than emitting invalid IR clang would
	// reject. With context-directed literal-width inference a literal sibling takes
	// the concrete operand's width (`i8(x) < 3` → both i8), so the width guard is
	// defensive: it fires only when a literal is too large for that width and was
	// left untyped (`i8(x) < 300`), lowering to the i64 default — loud, not
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

// lowerFloatComparison lowers a comparison of two already-lowered same-type float
// values to an `fcmp`. Relational ops and `==` use the *ordered* predicates
// (false when either operand is NaN — the C `<`/`==` semantics); `!=` uses the
// *unordered* `une` (true when either is NaN), so `x != x` is true for a NaN, as
// IEEE requires. (The typechecker already warns that float `==`/`!=` is precision-
// sensitive.)
func (l *lowerer) lowerFloatComparison(block *ir.Block, op ast.BooleanBinaryOp, left, right value.Value) (value.Value, error) {
	var pred enum.FPred
	switch op {
	case ast.BooleanBinaryOpEq:
		pred = enum.FPredOEQ
	case ast.BooleanBinaryOpNEq:
		pred = enum.FPredUNE
	case ast.BooleanBinaryOpLT:
		pred = enum.FPredOLT
	case ast.BooleanBinaryOpLTE:
		pred = enum.FPredOLE
	case ast.BooleanBinaryOpGT:
		pred = enum.FPredOGT
	case ast.BooleanBinaryOpGTE:
		pred = enum.FPredOGE
	default:
		return nil, fmt.Errorf("llvm: float comparison operator %v not implemented", op)
	}
	return block.NewFCmp(pred, left, right), nil
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
	if _, isFloat := left.Type().(*lltypes.FloatType); isFloat {
		v, err := l.applyFloatMathOp(block, e.Operator, left, right)
		return v, block, err
	}
	signed, err := l.getIntSignedness(e.Left)
	if err != nil {
		return nil, nil, err
	}
	v, err := l.applyIntMathOp(block, e.Operator, left, right, signed)
	return v, block, err
}

// applyFloatMathOp emits a binary floating-point arithmetic op on two
// already-lowered same-type float values — the float counterpart to
// applyIntMathOp, shared by lowerMathBinaryOpExpr and lowerMathAssignOp. `%` (Mod)
// is `frem` (LLVM's frem matches C fmod — the result's sign follows the dividend,
// the truncated form, exactly mirroring the integer `%`); `%%` (Remainder,
// floored) applies the sign-of-divisor fixup on top, mirroring the integer `%%`.
func (l *lowerer) applyFloatMathOp(block *ir.Block, op ast.MathBinaryOp, left, right value.Value) (value.Value, error) {
	switch op {
	case ast.MathBinaryOpAdd:
		return block.NewFAdd(left, right), nil
	case ast.MathBinaryOpSub:
		return block.NewFSub(left, right), nil
	case ast.MathBinaryOpMul:
		return block.NewFMul(left, right), nil
	case ast.MathBinaryOpDiv:
		return block.NewFDiv(left, right), nil
	case ast.MathBinaryOpMod:
		return block.NewFRem(left, right), nil
	case ast.MathBinaryOpRemainder:
		return l.lowerFlooredFRem(block, left, right), nil
	default:
		return nil, fmt.Errorf("llvm: float math binary op lowering not implemented for %v", op)
	}
}

// lowerFlooredFRem computes the floored-division remainder of two floats — Odin's
// "remainder (floored)" (%%), sign following the divisor — the float analogue of
// lowerFlooredSRem. Take the truncated remainder (`frem`, sign of the dividend);
// if it's non-zero and its sign disagrees with the divisor's, add the divisor
// back. Built branchlessly with `select`, since it sits inside one expression.
func (l *lowerer) lowerFlooredFRem(block *ir.Block, left, right value.Value) value.Value {
	zero := constant.NewFloat(left.Type().(*lltypes.FloatType), 0)
	r := block.NewFRem(left, right)
	rNeg := block.NewFCmp(enum.FPredOLT, r, zero)
	divisorNeg := block.NewFCmp(enum.FPredOLT, right, zero)
	signsDiffer := block.NewXor(rNeg, divisorNeg)
	nonZero := block.NewFCmp(enum.FPredONE, r, zero)
	needsFixup := block.NewAnd(nonZero, signsDiffer)
	fixed := block.NewFAdd(r, right)
	return block.NewSelect(needsFixup, fixed, r)
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
	var result value.Value
	if _, isFloat := cur.Type().(*lltypes.FloatType); isFloat {
		result, err = l.applyFloatMathOp(block, binOp, cur, rhs)
	} else {
		var signed bool
		if signed, err = l.getIntSignedness(e.Right); err == nil {
			result, err = l.applyIntMathOp(block, binOp, cur, rhs, signed)
		}
	}
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
// Covers the conversions the typechecker admits (Pit-of-Success #5): int→int
// (trunc/sext/zext), int→float (sitofp/uitofp, from the source's signedness),
// and float→float *widening* (fpext — narrowing is a typecheck error directing
// to floor/ceil/round). float→int is not a language operation (same rejection),
// so its arm here is defensive only.
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
	srcSigned := IsSignedInt(srcP.Name)

	if dstFloat, ok := dstLL.(*lltypes.FloatType); ok {
		switch arg.Type().(type) {
		case *lltypes.FloatType:
			// float→float: only widening reaches here (narrowing is a typecheck error).
			return coerceFloatWidth(block, arg, dstFloat), block, nil
		case *lltypes.IntType:
			// int→float: signed vs unsigned source picks sitofp vs uitofp.
			if srcSigned {
				return block.NewSIToFP(arg, dstFloat), block, nil
			}
			return block.NewUIToFP(arg, dstFloat), block, nil
		}
		return nil, nil, fmt.Errorf("llvm: conversion from %s to %s not implemented", arg.Type(), targetName)
	}

	dst, ok := dstLL.(*lltypes.IntType)
	if !ok {
		return nil, nil, fmt.Errorf("llvm: no integer/float LLVM representation for %q", targetName)
	}
	if _, ok := arg.Type().(*lltypes.IntType); !ok {
		// A float→int target reaches here only if the typechecker's rejection
		// (use floor/ceil/round) were bypassed — defensive, not a real path.
		return nil, nil, fmt.Errorf("llvm: conversion from %s to %s not implemented (float→int is not a conversion; use floor/ceil/round)", arg.Type(), targetName)
	}
	return coerceIntWidth(block, arg, srcSigned, dst), block, nil
}

// coerceAggregateElem defensively reconciles a lowered aggregate element value
// with its destination field type before an insertvalue/store. A well-typed
// program already has matching widths — the typechecker's context-directed
// literal-width propagation narrows a tuple/struct/data-payload literal element
// to its declared field type — so this is normally the identity. It exists so a
// residual int-width mismatch is coerced (trunc/ext, widening signedness read
// from the source expr's Lyra type) rather than letting llir panic inside
// NewInsertValue (as `insertvalue elem type mismatch, expected i8, got i64`); a
// non-int mismatch it can't reconcile is a loud error, never a panic.
func (l *lowerer) coerceAggregateElem(block *ir.Block, v value.Value, dst lltypes.Type, src ast.Expression) (value.Value, error) {
	if v.Type().Equal(dst) {
		return v, nil
	}
	dstInt, dstOk := dst.(*lltypes.IntType)
	srcInt, srcOk := v.Type().(*lltypes.IntType)
	if dstOk && srcOk {
		signed := false
		if dstInt.BitSize > srcInt.BitSize {
			// Only a widening ext depends on the source signedness; a narrowing
			// trunc is width-only. Fall back to unsigned if the type is unknown.
			if s, err := l.getIntSignedness(src); err == nil {
				signed = s
			}
		}
		return coerceIntWidth(block, v, signed, dstInt), nil
	}
	return nil, fmt.Errorf("llvm: aggregate element type mismatch: cannot store %s into %s", v.Type(), dst)
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

// coerceFloatWidth adapts an already-lowered float value to the destination float
// type (fptrunc to narrow, fpext to widen, identity when equal) — the float
// analogue of coerceIntWidth. In a well-typed program the widths already match
// (the typechecker propagates the target width onto untyped literal leaves and
// rejects implicit float widening otherwise), so this is normally the identity;
// the trunc/ext arms keep a residual mismatch from panicking llir's NewRet.
func coerceFloatWidth(block *ir.Block, v value.Value, dst *lltypes.FloatType) value.Value {
	src := v.Type().(*lltypes.FloatType)
	switch {
	case src.Kind == dst.Kind:
		return v
	case floatKindBits(dst.Kind) < floatKindBits(src.Kind):
		return block.NewFPTrunc(v, dst)
	default:
		return block.NewFPExt(v, dst)
	}
}

// floatKindBits maps the float kinds Lyra lowers (f16/f32/f64) to their bit
// widths, for ordering fptrunc vs fpext. An unexpected kind returns 0.
func floatKindBits(k lltypes.FloatKind) int {
	switch k {
	case lltypes.FloatKindHalf:
		return 16
	case lltypes.FloatKindFloat:
		return 32
	case lltypes.FloatKindDouble:
		return 64
	}
	return 0
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
