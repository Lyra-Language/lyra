package llvm

import (
	"github.com/llir/llvm/ir"
	"github.com/llir/llvm/ir/constant"
	"github.com/llir/llvm/ir/enum"
	lltypes "github.com/llir/llvm/ir/types"
	"github.com/llir/llvm/ir/value"

	"github.com/Lyra-Language/lyra/pkg/ast"
)

// writeFunc lazily declares libc's `i64 @write(i32 fd, i8* buf, i64 count)`
// (POSIX `ssize_t write(int, const void*, size_t)` — i64 ssize_t/size_t, i32 int
// on a 64-bit target), caching it so every print shares one declaration. clang
// links libc, so no runtime object is needed — the same self-contained story as
// memcmp/memcpy/malloc.
func (l *lowerer) writeFunc() *ir.Func {
	if l.write == nil {
		l.write = l.module.NewFunc("write", lltypes.I64,
			ir.NewParam("", lltypes.I32),
			ir.NewParam("", lltypes.NewPointer(lltypes.I8)),
			ir.NewParam("", lltypes.I64))
	}
	return l.write
}

// newlinePtr returns an i8* to a single interned "\n" byte, used for println's
// trailing newline. The byte lives in one private immutable global shared across
// every println in the module.
func (l *lowerer) newlinePtr() value.Value {
	if l.newlineByte == nil {
		l.newlineByte = l.module.NewGlobalDef(".nl", constant.NewCharArray([]byte("\n")))
		l.newlineByte.Immutable = true
		l.newlineByte.Linkage = enum.LinkagePrivate
	}
	arrTy := lltypes.NewArray(1, lltypes.I8)
	zero := constant.NewInt(lltypes.I32, 0)
	return constant.NewGetElementPtr(arrTy, l.newlineByte, zero, zero)
}

// lowerPrintCall lowers a `print(s)` / `println(s)` call: it writes the string
// argument's bytes to stdout (fd 1) via libc `write`, then — for println — a
// trailing newline. A string value is a fat pointer { i8* data, i64 len }
// (STRING_LAYOUT.md), so `write(1, data, len)` emits exactly the byte length,
// with no NUL-termination or formatting; a zero-length string is a valid no-op
// write. The typechecker guarantees exactly one `string` argument.
//
// `print` is a void statement — its value is discarded — so this returns the
// (last) `write` result; nothing reads it. stdout ordering across a `print`
// then `println` is preserved because each is its own `write` syscall in program
// order.
func (l *lowerer) lowerPrintCall(block *ir.Block, e *ast.FunctionCallExpr, newline bool) (value.Value, *ir.Block, error) {
	str, block, err := l.lowerExpr(block, e.Arguments[0])
	if err != nil {
		return nil, nil, err
	}
	data := block.NewExtractValue(str, 0) // i8* — first payload byte
	length := block.NewExtractValue(str, 1) // i64 byte length
	fd := constant.NewInt(lltypes.I32, 1)   // stdout
	result := block.NewCall(l.writeFunc(), fd, data, length)
	if newline {
		result = block.NewCall(l.writeFunc(), fd, l.newlinePtr(), constant.NewInt(lltypes.I64, 1))
	}
	return result, block, nil
}
