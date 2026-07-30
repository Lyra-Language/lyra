package llvm

import (
	"fmt"

	"github.com/llir/llvm/ir"
	"github.com/llir/llvm/ir/constant"
	lltypes "github.com/llir/llvm/ir/types"
	"github.com/llir/llvm/ir/value"

	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/driver"
	"github.com/Lyra-Language/lyra/pkg/types"
)

// beginFunction resets the per-function lowering state before a body is lowered.
// managedFrames starts with one root frame (holding `own` string params, added
// by defineFunction); block lowering pushes nested frames on top.
func (l *lowerer) beginFunction(retType lltypes.Type, retSigned, entryABI bool) {
	l.locals = map[string]value.Value{}
	l.loops = nil
	l.retType = retType
	l.retSigned = retSigned
	l.entryABI = entryABI
	l.managedFrames = [][]managedSlot{nil}
	l.pendingReleases = nil
	l.pendingBase = 0
	l.reuseToken = nil
	l.byRefParams = map[value.Value]bool{}
}

// paramIsByRef reports whether a parameter is passed as a **pointer to the caller's
// storage** rather than by value: exactly the `mut` ones.
//
// `mut` is a *mutable borrow* — the typechecker's own diagnostic tells users it
// mutates "the caller's value" — so the callee must be able to write through to the
// caller. Passing it by value made the parameter a private copy whose interior
// mutation was silently discarded, and (worse) that loss depended on the type: a
// `mut []T` or `mut shared T` propagated because it is *already* a pointer, while a
// `mut` struct, tuple, or `[N]T` dropped every write with no diagnostic. Passing
// every `mut` by reference is the only rule with no such split.
//
// `ref` is by reference for the same reason at one remove: it is *also* a borrow, so
// copying the value to lend it out is pure waste — a `ref [8]i64` was passed as a
// 64-byte first-class aggregate at every call. It cannot write, so the callee's view
// is read-only either way; what by-reference does change is that the callee now sees
// the caller's *live* value rather than a snapshot, which is observable only when the
// same binding is also passed to a `mut` parameter of that call. That aliasing is
// rejected up front (checkExclusiveMutableBorrow), so the two readings can't diverge.
//
// `own` stays by value: it *transfers* the value into the callee's own storage, so a
// copy is the point. The ownership pass is untouched — `mut` and `ref` are both still
// borrows, retained and released by nobody in the callee.
//
// A `mut` on a **copied scalar** is excluded, and that is not a silent split: it is
// exactly the case `lyra-W010` already warns is inert, reading the same shared
// predicate (types.IsCopiedScalar) so the two cannot drift. A scalar has no interior
// to write through, and the one construct that could observe a by-reference scalar —
// reassigning the whole parameter (`n = n + 1`) — does not lower for integers at all
// (a pre-existing backend gap, unrelated to this convention). Passing those by
// reference would change their ABI and reject `f(5)` at call sites for no observable
// gain. If whole-parameter reassignment ever lowers, `mut` on a scalar becomes
// meaningful and both this predicate and W010 should be revisited together.
func paramIsByRef(param ast.Parameter) bool {
	if param.TypeModifier != types.Mut && param.TypeModifier != types.Ref {
		return false
	}
	return !types.IsCopiedScalar(param.Type)
}

// emitReturn lowers a `ret` for the current function, coercing val to the
// function's return type. main is the one special case: its Lyra u8 value goes
// through the C ABI's i32 slot (coerce to u8, then zero-extend). A nil val is a
// bare `return` (or a void function) → `ret void`.
//
// A return leaves every enclosing scope at once, so it flushes this statement's
// temporaries and then releases all live managed bindings. A value that escapes
// via the return was transferred by the ownership pass (its slot retired at the
// move), so it is no longer in any frame — dropping the remaining local references
// here can't free the returned box out from under the caller.
// start is the block the returned expression began evaluating in (the function
// entry for a tail return, the `return` statement's block for an explicit one); it
// lets flushStmtTemps release a temp used before a branch at `block` (after the
// whole expression) instead of prematurely in its production block.
func (l *lowerer) emitReturn(start, block *ir.Block, val value.Value) error {
	if err := l.flushStmtTemps(start, block); err != nil {
		return err
	}
	if err := l.releaseAllManagedFrames(block); err != nil {
		return err
	}
	if l.entryABI {
		if val == nil {
			block.NewRet(constant.NewInt(lltypes.I32, 0))
			return nil
		}
		u8 := coerceIntWidth(block, val, false, lltypes.I8)
		block.NewRet(block.NewZExt(u8, lltypes.I32))
		return nil
	}
	if _, ok := l.retType.(*lltypes.VoidType); ok {
		// A void function: the body may still produce a value (a `print` call, a
		// block's last expression) — it is discarded, and control returns void.
		block.NewRet(nil)
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
	if _, ok := l.retType.(*lltypes.StructType); ok {
		// An aggregate return (a string fat pointer, or a tuple/struct) is returned
		// by value; the typechecker already guaranteed the body matches the type.
		block.NewRet(val)
		return nil
	}
	if _, ok := l.retType.(*lltypes.ArrayType); ok {
		// A fixed-size array is likewise returned by value (a first-class `[N x T]`).
		block.NewRet(val)
		return nil
	}
	if _, ok := l.retType.(*lltypes.PointerType); ok {
		// A `shared` value is returned as its box pointer (an owned return transfers
		// the reference; the ownership pass retired it from the frame at the move).
		block.NewRet(val)
		return nil
	}
	intTy, ok := l.retType.(*lltypes.IntType)
	if !ok {
		return fmt.Errorf("llvm: return of non-integer type %s not implemented", l.retType)
	}
	block.NewRet(coerceIntWidth(block, val, l.retSigned, intTy))
	return nil
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
			// fn.Blocks[0] is the body's start (a bare-expression body evaluates from
			// the entry block); it lets a temp used before a branch survive to `block`.
			if err := l.emitReturn(fn.Blocks[0], block, v); err != nil {
				return err
			}
		}
	default: // EntryReturnVoid — run the body for its side effects, then exit 0.
		if entry.Lambda.Body != nil {
			// lowerForEffect (not lowerExpr): a void body needs no value, and may be
			// an empty block or one ending in a non-expression statement.
			var err error
			block, err = l.lowerForEffect(block, entry.Lambda.Body)
			if err != nil {
				return err
			}
			if block.Term != nil {
				return nil // the body sealed the block (e.g. an early return)
			}
		}
		// Route through emitReturn (val == nil): it flushes owned temporaries and
		// releases managed frames before emitting the entryABI `ret i32 0`, so a
		// heap temp built in the body (`println("a" ++ b)`) is freed, not leaked.
		// The body already flushed via lowerForEffect, so start/end coincide here.
		if err := l.emitReturn(block, block, nil); err != nil {
			return err
		}
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
		// A *generic* function has no non-generic form to emit: a type variable has no
		// representation, so there is nothing to declare or define under the bare name.
		// Its specializations are emitted instead, one per instantiation
		// (monomorphize.go). Skipping it here is also what makes an unused generic
		// function cost nothing.
		if isGenericLambda(lambda) {
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
	declared, err := l.declareFunctionAs(l.userSymbol(decl), fn)
	if err != nil {
		return err
	}
	key := l.funcKey(decl.Name, decl.GetLocation())
	l.funcs[key] = declared
	l.funcParams[key] = fn.Parameters
	return nil
}

// declareFunctionAs declares a function under an explicit symbol and returns it,
// without registering it in l.funcs — the by-name table is for functions a *source*
// name can reach, which an emitted trait method is not (its symbol is mangled, and it
// is only ever reached through dispatch).
func (l *lowerer) declareFunctionAs(name string, fn *ast.LambdaExpr) (*ir.Func, error) {
	if len(fn.LambdaClauses) > 0 {
		return nil, fmt.Errorf("llvm: multi-clause functions are not implemented yet (%q)", name)
	}
	if fn.ReturnType.Type == nil {
		return nil, fmt.Errorf("llvm: function %q needs a return type annotation", name)
	}
	retType, err := l.lowerType(fn.ReturnType.Type)
	if err != nil {
		return nil, err
	}
	irParams := make([]*ir.Param, 0, len(fn.Parameters))
	for _, param := range fn.Parameters {
		if param.DefaultValue != nil {
			return nil, fmt.Errorf("llvm: default parameter values are not implemented yet (%q)", name)
		}
		irParam, err := l.lowerParameter(param)
		if err != nil {
			return nil, err
		}
		irParams = append(irParams, irParam)
	}
	return l.module.NewFunc(name, retType, irParams...), nil
}

// defineFunction lowers a declared function's body: bind each parameter into a
// fresh alloca (so the body reads it like any local), lower the body, and emit
// the implicit tail return (unless the body already ended in an explicit one).
func (l *lowerer) defineFunction(decl *ast.VarDeclStmt, fn *ast.LambdaExpr) error {
	return l.defineFunctionInto(l.funcs[l.funcKey(decl.Name, decl.GetLocation())], fn, decl.Name)
}

// defineFunctionInto is defineFunction's body, parameterized by the ir.Func to fill
// in. A *specialization* of a generic function lowers the same lambda into a
// different ir.Func (one per instantiation, monomorphize.go), so the two share this
// rather than the specialization path re-deriving parameter binding, `own`-param
// framing, and the void/typed return split — three things that must not drift
// between a generic function and a plain one.
func (l *lowerer) defineFunctionInto(irFn *ir.Func, fn *ast.LambdaExpr, name string) error {
	decl := &ast.VarDeclStmt{Name: name}
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
		if paramIsByRef(param) {
			// A by-reference `mut` parameter: the incoming pointer *is* the caller's
			// storage, so bind it directly as the binding's slot. alloca+store would
			// make the private copy this fix exists to remove. Deliberately not framed
			// — `mut` is a borrow, so the callee releases nothing; the caller still
			// owns the value and holds its +1.
			l.locals[ident.Name] = p
			l.byRefParams[p] = true
			continue
		}
		slot := entry.NewAlloca(p.Type())
		entry.NewStore(p, slot)
		l.locals[ident.Name] = slot
		// An `own` parameter is consumed by the callee: the caller transferred its +1,
		// so the callee releases it at function exit. A bare/`ref`/`mut` param is a
		// borrow — the caller still owns it and did not retain, so it is not recorded
		// here. Deep, matching the pass: an `own Person` carries a +1 on its string
		// field, so the callee owes that release too.
		if param.TypeModifier == types.Own && l.needsDrop(param.Type) {
			l.addManagedBinding(slot, param.Type)
		}
	}

	if _, isVoid := fn.ReturnType.Type.(types.VoidType); isVoid {
		// A void function: lower the body for effect (it may be empty or end in a
		// non-expression statement) and return void.
		end, err := l.lowerForEffect(entry, fn.Body)
		if err != nil {
			return err
		}
		if end.Term == nil {
			// The void body already flushed via lowerForEffect, so start/end coincide.
			if err := l.emitReturn(end, end, nil); err != nil {
				return err
			}
		}
		return nil
	}

	v, end, err := l.lowerExpr(entry, fn.Body)
	if err != nil {
		return err
	}
	if end.Term == nil {
		// `entry` is the body's start block; a temp used before a branch is released
		// at `end` (after the whole body) rather than in its production block.
		if err := l.emitReturn(entry, end, v); err != nil {
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
	if paramIsByRef(param) {
		irType = lltypes.NewPointer(irType)
	}
	return ir.NewParam(param.GetName(), irType), nil
}

func (l *lowerer) lowerFunctionCallExpr(block *ir.Block, e *ast.FunctionCallExpr) (value.Value, *ir.Block, error) {
	if member, ok := e.Function.(*ast.MemberExpr); ok {
		// A struct field that *holds* a function is an indirect call through that
		// value, not a method dispatch: `h.run(5)` where `run: (i64) -> i64`. The
		// two are told apart by looking the field up on the receiver's struct —
		// *not* by the member node's recorded type, which is a LambdaType for a
		// builtin method too (that is the builtin's own signature, and lowering
		// `x.floor` as a value would try to read a field out of an f64).
		if l.memberIsFunctionField(member) {
			return l.lowerCallThroughValue(block, e)
		}
		// A MemberExpr callee the typechecker resolved to a builtin method
		// (`x.floor()`, builtins.go) rather than a struct field, trait method,
		// or user function — those aren't lowered yet (see lowerBuiltinMethodCall).
		return l.lowerBuiltinMethodCall(block, e, member)
	}
	ident, ok := e.Function.(*ast.IdentifierExpr)
	if !ok {
		// Not a name: the callee is an expression producing a function value (an
		// immediately-applied lambda, a call returning a closure). Its static
		// LambdaType is the signature to call through.
		return l.lowerCallThroughValue(block, e)
	}
	// A local binding or parameter holding a function value shadows any top-level
	// function of the same name, exactly as it does for a non-function binding —
	// and it is the only way `let add5 = makeAdder(5)` can be called at all.
	if _, isLocal := l.locals[ident.Name]; isLocal {
		return l.lowerCallThroughValue(block, e)
	}
	// A type-name callee is a numeric conversion (`i32(x)`), not a function call.
	if targetName := types.PrimitiveTypeName(ident.Name); IsNumericConversionTarget(targetName) {
		return l.lowerNumericConversion(block, e, targetName)
	}
	// A call to a *generic* function resolves to the specialization the typechecker
	// solved for this call site, not to the generic name (which has no emitted body —
	// a type variable has no representation). Checked before l.funcs so the two can
	// never be confused.
	if fn, params, ok := l.specializedFuncFor(e); ok {
		return l.lowerDirectCall(block, e, fn, params)
	}
	fn, ok := l.funcs[l.funcKey(ident.Name, e.GetLocation())]
	if !ok {
		// A compiler-provided free function (print/println) — checked only after
		// user functions, so a user binding of the same name shadows the builtin,
		// matching the typechecker's resolution order.
		switch ident.Name {
		case "print":
			return l.lowerPrintCall(block, e, false)
		case "println":
			return l.lowerPrintCall(block, e, true)
		}
		return nil, nil, fmt.Errorf("llvm: call to unknown function %q", ident.Name)
	}
	// Arguments match the parameters positionally. The typechecker validated
	// arity and assignability, and propagated each parameter's width onto its
	// argument (inferLambdaCall), so a literal arg already lowers at the param's
	// width — no coercion needed here.
	return l.lowerDirectCall(block, e, fn, l.funcParams[ident.Name])
}

// lowerDirectCall lowers the arguments of a call to a known function and emits it.
// Shared by an ordinary call by name and a call to a generic *specialization*, so
// the by-reference `mut`/`ref` argument handling below has one definition.
func (l *lowerer) lowerDirectCall(block *ir.Block, e *ast.FunctionCallExpr, fn *ir.Func, params []ast.Parameter) (value.Value, *ir.Block, error) {
	args := make([]value.Value, 0, len(e.Arguments))
	for i, argExpr := range e.Arguments {
		var (
			v   value.Value
			err error
		)
		// A `mut`/`ref` parameter is by reference (paramIsByRef), so pass the
		// argument's *address* rather than a copy of its value. lvalueAddress walks
		// the same identifier/field/index path an assignment target does, so `f(p)`,
		// `f(grid[i])`, and `f(p.inner)` all forward the right slot; forwarding a
		// by-ref parameter onward (`g(p)` inside the callee) resolves to the incoming
		// pointer, which is already the caller's address.
		//
		// A **temporary** has no such address, and unlike `mut` (where the typechecker
		// requires a mutable lvalue) it is perfectly legitimate to lend one out:
		// `f(Pt { x: 1 })`, `f(make())`, `f("a" ++ "b")`. Materialize it into an
		// entry-block alloca and pass that — the same shape arrayLValue uses for a
		// non-addressable array. The callee only reads during the call, so the slot's
		// function-long lifetime is more than enough, and an owned temporary is still
		// released after the statement by the ordinary pending-temp machinery.
		if i < len(params) && paramIsByRef(params[i]) {
			var ptr value.Value
			ptr, block, err = l.argumentAddress(block, argExpr)
			if err != nil {
				return nil, nil, err
			}
			args = append(args, ptr)
			continue
		}
		v, block, err = l.lowerExpr(block, argExpr)
		if err != nil {
			return nil, nil, err
		}
		args = append(args, v)
	}
	return block.NewCall(fn, args...), block, nil
}

// argumentAddress yields the address to pass for a by-reference parameter
// (paramIsByRef).
//
// An **lvalue path** — a binding, or a field/element of one — forwards the
// caller's own storage via lvalueAddress, which is what makes a `mut` write reach
// the caller and lets a `ref` observe the live value. Anything else is a
// **temporary** (a constructor result, a call result, a concatenation): it has no
// storage of its own, so it is materialized into an entry-block alloca and that
// address is passed. Only `ref` reaches the temporary case in a well-typed program
// — checkMutArgument requires a `mut` argument to be an lvalue rooted at a mutable
// binding, since writing to a temporary would be meaningless.
func (l *lowerer) argumentAddress(block *ir.Block, arg ast.Expression) (value.Value, *ir.Block, error) {
	if isLValuePath(arg) {
		loc, block, err := l.lvalueAddress(block, arg)
		if err != nil {
			return nil, nil, err
		}
		return loc.ptr, block, nil
	}
	v, block, err := l.lowerExpr(block, arg)
	if err != nil {
		return nil, nil, err
	}
	entry := block.Parent.Blocks[0]
	slot := entry.NewAlloca(v.Type())
	block.NewStore(v, slot)
	return slot, block, nil
}

// isLValuePath reports whether e names storage — an identifier, or a field/element
// path rooted at one — as opposed to a temporary. The backend counterpart of the
// typechecker's rootIdentifier walk.
func isLValuePath(e ast.Expression) bool {
	for {
		switch t := e.(type) {
		case *ast.IdentifierExpr:
			return true
		case *ast.MemberExpr:
			e = t.Object
		case *ast.IndexExpr:
			e = t.Object
		default:
			return false
		}
	}
}

// isGenericLambda reports whether a function's signature mentions a type variable,
// in which case only its specializations can be emitted.
func isGenericLambda(fn *ast.LambdaExpr) bool {
	for _, p := range fn.Parameters {
		if mentionsTypeVar(p.Type) {
			return true
		}
	}
	return mentionsTypeVar(fn.ReturnType.Type)
}

// mentionsTypeVar reports whether a type contains a GenericType leaf.
func mentionsTypeVar(t types.Type) bool {
	switch tt := t.(type) {
	case types.GenericType:
		return true
	case types.StaticArrayType:
		return mentionsTypeVar(tt.ElementType)
	case types.DynamicArrayType:
		return mentionsTypeVar(tt.ElementType)
	case types.TupleType:
		for _, e := range tt.Elements {
			if mentionsTypeVar(e) {
				return true
			}
		}
	case types.WeakType:
		return mentionsTypeVar(tt.Inner)
	}
	return false
}

// userSymbol is the emitted name for a top-level user function: its module path under a
// `lyra.` prefix, so `double` in `util.math` becomes `@lyra.util.math.double`.
//
// The prefix is not decoration. Emitted user functions previously took their source name
// verbatim, so a program with a function named `malloc`, `write` or `lyra_rc_alloc`
// produced a module clang rejected outright — "invalid redefinition" against libc or
// against Lyra's own emitted runtime. Reachable from ordinary source, and a name a
// program has every right to use.
//
// The module path is what makes the symbol *cross-module* unique, which is what separate
// compilation and per-module private names will both need. It cannot collide with the
// runtime's own `lyra_`-prefixed symbols either: everything here has a dot after `lyra`,
// and those have an underscore.
//
// `main` never reaches this — it is emitted by lowerEntry with the C ABI the platform
// expects, and forEachUserFunction excludes it.
func (l *lowerer) userSymbol(decl *ast.VarDeclStmt) string {
	module := ""
	if l.res != nil && l.res.SymbolTable != nil {
		module = l.res.SymbolTable.ModuleOfFile[decl.GetLocation().File]
	}
	if module == "" {
		return "lyra." + decl.Name
	}
	return "lyra." + module + "." + decl.Name
}

// funcKey is the key a function is stored and found under in l.funcs.
//
// A *private* function is keyed by its module, so two modules may each declare a
// `helper` without one overwriting the other — which is exactly what happened before:
// only the last module's was emitted, and the other module's calls pointed at a
// function that no longer existed, producing malformed IR. Exported functions keep the
// bare name, so a cross-module call finds them.
//
// The key is asked for from the *referencing* location, so a call inside a module finds
// that module's private function while a call from elsewhere finds the exported one.
// The symbol table decides, so the backend and the front end cannot disagree about
// which declaration a name means.
func (l *lowerer) funcKey(name string, loc ast.Location) string {
	if l.res == nil || l.res.SymbolTable == nil {
		return name
	}
	return l.res.SymbolTable.FunctionKey(name, loc)
}
