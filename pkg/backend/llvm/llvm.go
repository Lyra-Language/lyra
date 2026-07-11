// Package llvm is the (in-progress) LLVM IR backend for Lyra. It lowers a typed
// program from pkg/driver to LLVM IR (built with github.com/llir/llvm), which
// llc/clang then compiles.
//
// # Status: early
//
// Emit defines `@main` and lowers its body via lowerExpr, which so far handles
// integer literals — so `let main = () -> i64 => 42` compiles to a binary that
// exits 42. Any other body form errors (the build fails loudly rather than
// emitting wrong code). Grow lowerExpr and lowerType from here.
//
// # Where to build
//
// The lowering grows out from lowerEntry in roughly this order (see
// lyra/todo.md's backend section):
//
//  1. lowerType(t types.Type) — Lyra type → an llir `types.Type` (i8..i64/u* → iN,
//     f16/32/64 → half/float/double, bool → i1, struct → a struct type, data/sum
//     → a tagged union { tag, payload } per DATA_LAYOUT.md). `stack` values lower
//     by value, `shared` values to a pointer to a ref-counted box — see
//     ALLOCATION.md. The two docs compose: the sum-type layout is the payload; the
//     flavor decides inline vs boxed. layout.go/runtime.go provide the building
//     blocks — LLVMPrimitive, SharedBoxType, TagType, DataUnionType, SizeAndAlign,
//     and declareRuntime (wired into Emit) — for lowerType to dispatch over.
//  2. Grow lowerExpr past integer literals: arithmetic/calls, then let/if/blocks.
//     Model mutable locals as `alloca` + load/store (let mem2reg build SSA) rather
//     than hand-writing phi nodes.
//  3. Runtime shims: print, and the overflow trap for todo #2 (via
//     llvm.sadd.with.overflow); the builtin overflow-arithmetic methods
//     (typechecker/builtins.go) lower to two's-complement +/-/* and
//     llvm.{s,u}{add,sub}.sat.
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
	declareRuntime(m)
	l := &lowerer{module: m, res: res}
	if err := l.lowerEntry(entry); err != nil {
		return nil, err
	}
	return []byte(m.String()), nil
}

type lowerer struct {
	module *ir.Module
	res    *driver.Result // gives you TypeTable, SymbolTable, MethodTable, …
	// grows over time: locals map[string]value.Value, funcs map[string]*ir.Func, etc.
}

// lowerEntry defines `@main` and returns the entry function's value as the
// process exit code. An i64 entry returns its body's value; a void entry runs
// the body for effect (none expressible yet) and returns 0.
func (l *lowerer) lowerEntry(entry *driver.EntryPoint) error {
	fn := l.module.NewFunc("main", lltypes.I64)
	block := fn.NewBlock("entry")

	switch entry.Returns {
	case driver.EntryReturnExitCode:
		v, err := l.lowerExpr(block, entry.Lambda.Body)
		if err != nil {
			return err
		}
		block.NewRet(v)
	default: // EntryReturnVoid — nothing observable to run yet; exit 0.
		block.NewRet(constant.NewInt(lltypes.I64, 0))
	}
	return nil
}

// lowerExpr lowers a Lyra expression to an LLVM value, appending any instructions
// it needs to block. This is the seed of expression lowering — grow the switch as
// you add forms. It returns an error (rather than emitting wrong code) for a form
// that isn't handled yet, so `lyrac build` fails loudly.
//
// Integer literals lower to an i64 constant for now: the only caller is the i64
// entry point, whose body is i64. As more callers appear, the target width
// should come from res.TypeTable rather than being hardcoded.
func (l *lowerer) lowerExpr(block *ir.Block, expr ast.Expression) (value.Value, error) {
	switch e := expr.(type) {
	case *ast.IntegerLiteralExpr:
		return constant.NewInt(lltypes.I64, e.Value), nil
	case *ast.MathBinaryOpExpr:
		left, err := l.lowerExpr(block, e.Left)
		if err != nil {
			return nil, err
		}
		right, err := l.lowerExpr(block, e.Right)
		if err != nil {
			return nil, err
		}
		t, ok := l.res.TypeTable.Get(e.Left)
		if !ok {
			return nil, fmt.Errorf("llvm: type not found for %T", e.Left)
		}
		pt, ok := t.(types.PrimitiveType) // assert it's a primitive
		if !ok {
			return nil, fmt.Errorf("llvm: type not found for %T", e.Left)
		}
		signed := IsSignedInt(pt.Name)
		switch e.Operator {
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
			if signed {
				return block.NewSRem(left, right), nil
			}
			return block.NewURem(left, right), nil
		case ast.MathBinaryOpRemainder:
			if signed {
				return block.NewSRem(left, right), nil
			}
			return block.NewURem(left, right), nil
		default:
			return nil, fmt.Errorf("llvm: math binary op lowering not implemented for %v", e.Operator)
		}
	}
	return nil, fmt.Errorf("llvm: expression lowering not implemented for %T", expr)
}
