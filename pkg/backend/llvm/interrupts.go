package llvm

import (
	"fmt"

	"github.com/Lyra-Language/lyra/pkg/ast"
	lltypes "github.com/llir/llvm/ir/types"
	"github.com/llir/llvm/ir/value"
)

// emitInterruptEntries exports each `@interrupt(kind)` function as `__lyra_interrupt_kind`,
// the symbol a console runtime's interrupt entry calls (runtime/genesis/runtime.c) — a
// C-convention `void (void)` that calls the Lyra function, which the optimizer inlines.
//
// Not LLVM's M68k interrupt calling convention: it returns with `rte` but does not save the
// scratch registers it or its callees clobber, so the runtime's stub saves them and the
// handler stays an ordinary function. checker.CheckInterruptHandlers has already refused a
// handler that is not a top-level `() -> void`, and one on the host.
func (l *lowerer) emitInterruptEntries(program *ast.Program) error {
	l.interruptShared = map[string]bool{}
	for _, node := range program.Statements {
		decl, ok := node.(*ast.VarDeclStmt)
		if !ok {
			continue
		}
		lambda, ok := decl.Value.(*ast.LambdaExpr)
		if !ok || lambda.Interrupt == "" {
			continue
		}
		fn := l.funcs[l.funcKey(decl.Name, decl.GetLocation())]
		if fn == nil {
			return fmt.Errorf("llvm: @interrupt(%s) function %q was not declared", lambda.Interrupt, decl.Name)
		}
		l.noteInterruptWrites(lambda)
		entry := l.module.NewFunc("__lyra_interrupt_"+lambda.Interrupt, lltypes.Void)
		block := entry.NewBlock("entry")
		block.NewCall(fn)
		block.NewRet(nil)
	}
	return nil
}

// **A module-level `var` an interrupt handler writes is volatile everywhere** (09/30). The
// handler runs between any two instructions of the main program, which the optimizer does
// not know: a main loop waiting on `frames < 30` was compiled to read `frames` once, and
// the wait never ended however many times the handler ran. So every load and store of such
// a variable is volatile — in the handler and out — and `frames += 1` in the handler with
// `for frames < 30 {}` in `main` is simply correct.
//
// The set is what the handler's own body writes, by assignment, compound assignment or
// `&mut`: a variable written only in a function the handler calls is not seen, and needs
// `read_volatile` in the main program (LANGUAGE.md says so).
func (l *lowerer) noteInterruptWrites(handler *ast.LambdaExpr) {
	note := func(name string, loc ast.Location) { l.interruptShared[l.funcKey(name, loc)] = true }
	if handler.Body == nil {
		return
	}
	ast.WalkExpr(handler.Body, func(s ast.Statement) bool {
		if vrs, ok := s.(*ast.VarReassignmentStmt); ok {
			note(vrs.Name, vrs.GetLocation())
		}
		return true
	}, func(e ast.Expression) bool {
		switch x := e.(type) {
		case *ast.MathAssignOpExpr:
			if id, ok := x.Left.(*ast.IdentifierExpr); ok {
				note(id.Name, x.GetLocation())
			}
		case *ast.AddressOfExpr:
			if id, ok := x.Operand.(*ast.IdentifierExpr); ok && x.IsMut {
				note(id.Name, x.GetLocation())
			}
		}
		return true
	})
}

// sharedWithInterrupt reports whether slot — the storage name resolved to at loc — is a
// module-level variable an interrupt handler writes, so an access to it must be volatile.
func (l *lowerer) sharedWithInterrupt(name string, loc ast.Location, slot value.Value) bool {
	key := l.funcKey(name, loc)
	if !l.interruptShared[key] {
		return false
	}
	g, isGlobal := l.globals[key]
	return isGlobal && value.Value(g) == slot
}
