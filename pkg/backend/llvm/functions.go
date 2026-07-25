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
	l.reuseToken = nil
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
func (l *lowerer) emitReturn(block *ir.Block, val value.Value) error {
	if err := l.flushTemps(); err != nil {
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
			if err := l.emitReturn(block, v); err != nil {
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
		if err := l.emitReturn(block, nil); err != nil {
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
		// An `own` managed parameter is consumed by the callee: the caller
		// transferred its +1, so the callee releases it at function exit. A
		// bare/`ref`/`mut` managed param is a borrow — the caller still owns it, so
		// it is not recorded here.
		if param.TypeModifier == types.Own && isManagedSlot(slot) {
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
			if err := l.emitReturn(end, nil); err != nil {
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

func (l *lowerer) lowerFunctionCallExpr(block *ir.Block, e *ast.FunctionCallExpr) (value.Value, *ir.Block, error) {
	if member, ok := e.Function.(*ast.MemberExpr); ok {
		// A MemberExpr callee the typechecker resolved to a builtin method
		// (`x.floor()`, builtins.go) rather than a struct field, trait method,
		// or user function — those aren't lowered yet (see lowerBuiltinMethodCall).
		return l.lowerBuiltinMethodCall(block, e, member)
	}
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
