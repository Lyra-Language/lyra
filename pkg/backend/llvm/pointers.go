package llvm

import (
	"fmt"

	"github.com/llir/llvm/ir"
	"github.com/llir/llvm/ir/value"

	"github.com/Lyra-Language/lyra/pkg/ast"
)

// Raw pointers: `&x`, `&mut x`, `p^`, `p^ = v`, and the `unsafe { … }` block they sit
// inside.
//
// **Every one of them is something this package already does**, which is the whole reason
// the feature is small here after being large in the front end. A raw pointer is an LLVM
// pointer and nothing more: `&x` is the address of x's storage — the same address a
// `mut` parameter is passed by — `p^` is a load from it, `p^ = v` a store, and an
// `unsafe` block is its body. There is no ownership, no refcounting and no drop glue: a
// raw pointer does not own what it points at, which is exactly what makes it raw.
//
// The one thing that would be wrong is to reach for `lowerExpr` on the operand of `&`:
// that yields the *value*, and storing it into a fresh slot would hand out the address of
// a copy. `argumentAddress` is the existing answer — it takes the real storage for an
// lvalue path and only falls back to a temporary slot for something that has no storage,
// which the typechecker has already refused here (lyra-E059).

// lowerAddressOf lowers `&x` / `&mut x` to the address of x's storage.
//
// Mutability is a *type-level* distinction: `^T` and `^mut T` are the same machine value,
// and what separates them is what the front end permits (lyra-E061). Nothing here needs
// to know which it lowered.
func (l *lowerer) lowerAddressOf(block *ir.Block, e *ast.AddressOfExpr) (value.Value, *ir.Block, error) {
	if e.Operand == nil {
		return nil, nil, fmt.Errorf("llvm: `&` with no operand")
	}
	if !isLValuePath(e.Operand) {
		// The typechecker refuses this (lyra-E059), so reaching it means the two
		// disagree about what has an address — rule 5: say so rather than hand back the
		// address of a copy, which would dangle at the end of the statement.
		return nil, nil, fmt.Errorf("llvm: cannot take the address of a temporary")
	}
	return l.argumentAddress(block, e.Operand)
}

// lowerDeref lowers `p^` to a load through the pointer.
func (l *lowerer) lowerDeref(block *ir.Block, e *ast.DerefExpr) (value.Value, *ir.Block, error) {
	if e.Operand == nil {
		return nil, nil, fmt.Errorf("llvm: `^` with no operand")
	}
	ptr, block, err := l.lowerExpr(block, e.Operand)
	if err != nil {
		return nil, nil, err
	}
	// The pointee's type comes from the *recorded* type of the deref expression, not from
	// the LLVM pointer, which carries none: pointers are opaque, so the load has to be
	// told what it is loading. This is the same reason every other load in this package
	// reads recordedType first.
	pointee, ok := l.recordedType(e)
	if !ok {
		return nil, nil, fmt.Errorf("llvm: no type recorded for a pointer dereference")
	}
	llType, err := l.lowerType(pointee)
	if err != nil {
		return nil, nil, err
	}
	return block.NewLoad(llType, ptr), block, nil
}

// lowerDerefAssignment lowers `p^ = v` to a store through the pointer.
func (l *lowerer) lowerDerefAssignment(block *ir.Block, stmt *ast.DerefAssignmentStmt) (*ir.Block, error) {
	if stmt.Target.Operand == nil {
		return nil, fmt.Errorf("llvm: pointer write with no target")
	}
	ptr, block, err := l.lowerExpr(block, stmt.Target.Operand)
	if err != nil {
		return nil, err
	}
	v, block, err := l.lowerExpr(block, stmt.Value)
	if err != nil {
		return nil, err
	}
	// Coerced for the reason an aggregate element is: an untyped literal reaches here at
	// its default width, and storing an i64 through a pointer to i8 is a module clang
	// refuses.
	pointee, ok := l.recordedType(&stmt.Target)
	if ok {
		llType, err := l.lowerType(pointee)
		if err != nil {
			return nil, err
		}
		if v, err = l.coerceAggregateElem(block, v, llType, stmt.Value); err != nil {
			return nil, err
		}
	}
	block.NewStore(v, ptr)
	return block, nil
}

// lowerUnsafeBlock lowers `unsafe { … }` as its body.
//
// An `unsafe` block changes what the *front end permits* inside it and nothing else — it
// is not a scope with its own storage, not a barrier, and not a value of its own. Lowering
// it as anything but its body would give the keyword a runtime meaning it does not have.
func (l *lowerer) lowerUnsafeBlock(block *ir.Block, e *ast.UnsafeBlockExpr) (value.Value, *ir.Block, error) {
	if e.Body == nil {
		return nil, nil, fmt.Errorf("llvm: `unsafe` block with no body")
	}
	// lowerBlockStmts, not lowerBlock: the latter insists the block *has* a value, and an
	// `unsafe` block is as often a statement as an expression — `unsafe { p^ = v }` ends
	// in an assignment and produces nothing. A caller that needs a value gets nil here
	// and reports it, which is the same split lowerForEffect draws for a plain block.
	return l.lowerBlockStmts(block, e.Body, false)
}

// isExtern reports whether name is a foreign function declared in this program.
func (l *lowerer) isExtern(name string) bool {
	if l.res == nil || l.res.Program == nil {
		return false
	}
	for _, stmt := range l.res.Program.Statements {
		if ext, ok := stmt.(*ast.ExternDeclStmt); ok && ext.Name == name {
			return true
		}
	}
	return false
}

// lowerExternCall refuses a call to a foreign function, loudly.
//
// The front end is complete — the declaration is collected, its signature checked against
// each call, its effects taken from the bound it asserts, and its `@link` collected — but
// nothing emits a declaration for the foreign symbol or a call to it. Rule 5: a form that
// does not lower yet is a hard error, never a guess, and the alternative here is worse than
// usual — an `extern`'s body-less LambdaExpr would otherwise reach the ordinary function
// path and emit a `define` with no blocks, which clang rejects with a message about the IR
// rather than about the program.
func (l *lowerer) lowerExternCall(name string) error {
	return fmt.Errorf("llvm: cannot lower a call to the foreign function %q: `extern` is "+
		"checked but not yet emitted — see todo.md (Foreign functions)", name)
}
