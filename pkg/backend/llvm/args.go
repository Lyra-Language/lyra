package llvm

import (
	"fmt"

	"github.com/llir/llvm/ir"
	"github.com/llir/llvm/ir/constant"
	"github.com/llir/llvm/ir/enum"
	lltypes "github.com/llir/llvm/ir/types"
	"github.com/llir/llvm/ir/value"

	"github.com/Lyra-Language/lyra/pkg/ast"
)

// The program's arguments: `program_arg_count() -> i64` and `program_arg(i) -> string`.
//
// Two builtins, on the `random_seed` rule — argv exists only in the C runtime's `main`,
// so nothing in the language can reach it, while everything above these two (the
// `[]string` a program actually wants, flag parsing) is arithmetic and lives in the
// prelude (`program_args`). `main` is emitted as `main(i32 argc, i8** argv)` and stashes
// both in module globals on entry, before module-level data is initialized, so a builtin
// called from anywhere reads the same two words.
//
// `program_arg(i)` **copies** argv[i] into a fresh ref-counted string, the way
// `p.decode_utf8(n)` copies foreign bytes: a string is a box whose header precedes its
// payload, so it cannot point into memory the C runtime owns. An index outside
// `0..<count` traps with the array's own out-of-bounds message — it is an index. The
// C-string length is measured by a shim emitted here rather than a `declare strlen`,
// because the backend keys externs by C symbol and a program's own `extern strlen` must
// keep the one declaration it expects.

const (
	globalArgc  = "lyra_argc"
	globalArgv  = "lyra_argv"
	ShimCStrLen = "lyra_cstr_len"
)

// ensureArgsGlobals defines the two globals `main` fills, idempotent per module.
func (l *lowerer) ensureArgsGlobals() (argc, argv *ir.Global) {
	if l.argcGlobal == nil {
		l.argcGlobal = l.module.NewGlobalDef(globalArgc, constant.NewInt(lltypes.I64, 0))
		l.argvGlobal = l.module.NewGlobalDef(globalArgv,
			constant.NewNull(lltypes.NewPointer(lltypes.NewPointer(lltypes.I8))))
	}
	return l.argcGlobal, l.argvGlobal
}

// storeProgramArgs is what `main` runs first: argc widened to i64, argv as it came.
func (l *lowerer) storeProgramArgs(block *ir.Block, argc, argv value.Value) {
	argcG, argvG := l.ensureArgsGlobals()
	block.NewStore(block.NewSExt(argc, lltypes.I64), argcG)
	block.NewStore(argv, argvG)
}

// ensureCStrLenRuntime emits `lyra_cstr_len(i8*) -> i64`: the byte count before the NUL.
func (l *lowerer) ensureCStrLenRuntime() *ir.Func {
	if l.cstrLen != nil {
		return l.cstrLen
	}
	i8ptr := lltypes.NewPointer(lltypes.I8)
	p := ir.NewParam("p", i8ptr)
	fn := l.module.NewFunc(ShimCStrLen, lltypes.I64, p)
	entry := fn.NewBlock("entry")
	loop := fn.NewBlock("loop")
	done := fn.NewBlock("done")
	entry.NewBr(loop)
	n := loop.NewPhi(ir.NewIncoming(i64c(0), entry))
	byte := loop.NewLoad(lltypes.I8, loop.NewGetElementPtr(lltypes.I8, p, n))
	next := loop.NewAdd(n, i64c(1))
	n.Incs = append(n.Incs, ir.NewIncoming(next, loop))
	loop.NewCondBr(loop.NewICmp(enum.IPredEQ, byte, constant.NewInt(lltypes.I8, 0)), done, loop)
	done.NewRet(n)
	l.cstrLen = fn
	return fn
}

// lowerProgramArgCountCall lowers `program_arg_count()` to one load.
func (l *lowerer) lowerProgramArgCountCall(block *ir.Block, e *ast.FunctionCallExpr) (value.Value, *ir.Block, error) {
	if len(e.Arguments) != 0 {
		return nil, nil, fmt.Errorf("llvm: program_arg_count expects 0 arguments, got %d", len(e.Arguments))
	}
	argc, _ := l.ensureArgsGlobals()
	return block.NewLoad(lltypes.I64, argc), block, nil
}

// lowerProgramArgCall lowers `program_arg(i)`: bounds-check, measure, copy into a box.
func (l *lowerer) lowerProgramArgCall(block *ir.Block, e *ast.FunctionCallExpr) (value.Value, *ir.Block, error) {
	if len(e.Arguments) != 1 {
		return nil, nil, fmt.Errorf("llvm: program_arg expects 1 argument, got %d", len(e.Arguments))
	}
	idx, block, err := l.lowerExpr(block, e.Arguments[0])
	if err != nil {
		return nil, nil, err
	}
	if diverged(idx, block) {
		return nil, block, nil
	}
	signed, _ := l.getIntSignedness(e.Arguments[0])
	i := coerceIntWidth(block, idx, signed, lltypes.I64)
	argcG, argvG := l.ensureArgsGlobals()
	argc := block.NewLoad(lltypes.I64, argcG)
	// One unsigned compare covers both a negative index and one past the end.
	block = l.emitTrapIf(block, block.NewICmp(enum.IPredUGE, i, argc), l.panicIndexOOBFunc())

	i8ptr := lltypes.NewPointer(lltypes.I8)
	argv := block.NewLoad(lltypes.NewPointer(i8ptr), argvG)
	src := block.NewLoad(i8ptr, block.NewGetElementPtr(i8ptr, argv, i))
	byteLen := block.NewCall(l.ensureCStrLenRuntime(), src)
	_, dst := l.rcAllocStringPayload(block, byteLen)
	block.NewCall(l.memcpyFunc(), dst, src, byteLen)
	count := block.NewCall(l.utf8CountFunc(), dst, byteLen)
	return makeString(block, dst, byteLen, count), block, nil
}
