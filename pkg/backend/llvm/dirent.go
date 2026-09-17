package llvm

import (
	"fmt"
	"runtime"

	"github.com/llir/llvm/ir"
	"github.com/llir/llvm/ir/constant"
	"github.com/llir/llvm/ir/enum"
	lltypes "github.com/llir/llvm/ir/types"
	"github.com/llir/llvm/ir/value"

	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/types"
)

// A directory's entries — the `dir_names(path)` builtin, `(string) -> Maybe<[]string>`.
//
// Same division of labour as `read_line`/`parse_i64` and `program_arg`/`program_args`:
// this is the crossing the language cannot express, and everything above it — dropping
// `.` and `..`, sorting, recursing, filtering by extension — is ordinary Lyra in
// `std.io` (`read_dir`). What it answers is exactly what libc reported, in the order
// libc reported it.
//
// # Why this one is the compiler's, when `open`/`read`/`close` are not
//
// `std/io.lyra` reads and writes files over three `extern`s and says, in its own module
// doc, that a file needs no builtin. A **directory** does, and the reason is `struct
// dirent`: `readdir` answers a pointer to it, the name lives at a platform-dependent
// offset inside it, and there is no struct-free directory interface in libc to fall back
// on — `scandir` and `glob` hand back the same struct, and `fts`/`ftw` take function
// pointers, which Lyra cannot yet pass to C. Meanwhile Lyra source has no way to ask what
// platform it is being compiled for (todo.md's target predicates are unbuilt), so the
// offset cannot be written in `std.io` at all. The knowledge has to live where the host is
// known, and that is here.
//
// # Platform
//
// This file is the **second place the backend is not platform-neutral** (tui.go's
// `TIOCGWINSZ` is the first), and it goes one step further than that one: tui.go carries
// `struct termios` as an oversized opaque buffer precisely so it never has to know a field
// offset, and this file cannot, because the field *is* the answer. Both constants below
// are chosen from the compiling host, which is sound for the same reason tui.go's is —
// `lyrac` compiles for its host and has no notion of a target.
//
// Both numbers were measured rather than remembered (09/17), with `offsetof` through each
// platform's own `dirent.h`:
//
//   - macOS arm64: `d_name` at 21, `sizeof(struct dirent)` 1048.
//   - Linux glibc aarch64: `d_name` at 19, `sizeof(struct dirent)` 280. musl agrees; the
//     LP64 assumption the rest of the compiler states (`CLong` is the grep target) is what
//     makes one number answer for the platform rather than for a word size.
//
// **The symbol is platform-dependent too, and that is the sharper hazard.** macOS declares
// `readdir` as `__DARWIN_INODE64(readdir)`, which on x86_64 renames it to
// `readdir$INODE64`; on arm64 the suffix is empty because that ABI only ever had 64-bit
// inodes. A bare `readdir` on x86_64 macOS therefore **links successfully** against the
// legacy 32-bit-inode entry point, whose `d_name` sits at offset 8 — a wrong answer that
// does not trap, on data rather than on a coding mistake. That is the failure
// `std/io.lyra`'s `creat` comment was written about, and it is why the symbol is chosen
// here rather than assumed.
//
// An unknown host is refused rather than guessed, on the backend's own rule: emitting a
// plausible offset for a platform nobody measured is exactly the silent wrongness the
// loud error exists to prevent.

// ShimReadDir reads a directory and returns the canonical `Maybe<[]string>` directly —
// `Some` of the names, or `None` when the directory cannot be opened.
const ShimReadDir = "lyra_read_dir"

// direntNameOffset is the byte offset of `d_name` within `struct dirent` on the compiling
// host. See this file's Platform note for the measurements and for why a host constant is
// sound here.
func direntNameOffset() (int64, error) {
	switch runtime.GOOS {
	case "darwin":
		return 21, nil
	case "linux":
		return 19, nil
	default:
		return 0, fmt.Errorf("llvm: dir_names is unsupported on %s: "+
			"the offset of d_name within struct dirent has not been measured there", runtime.GOOS)
	}
}

// readdirSymbol is the C symbol `readdir` resolves to on the compiling host. See this
// file's Platform note: on x86_64 macOS the bare name is the legacy 32-bit-inode entry
// point, whose struct has a different layout and which links without complaint.
func readdirSymbol() string {
	if runtime.GOOS == "darwin" && runtime.GOARCH == "amd64" {
		return "readdir$INODE64"
	}
	return "readdir"
}

// ensureReadDirRuntime emits `lyra_read_dir` into the module the first time it is needed,
// caching the handle on the lowerer, in the same emit-into-the-module style as the other
// shims — so `lyrac build`'s single `clang out.ll` stays self-contained with no runtime
// object to link.
//
// The names are **copied** into fresh ref-counted strings, as `program_arg` copies argv:
// a Lyra string is a box whose header precedes its payload, so it cannot point into the
// buffer `readdir` owns and reuses on the next call.
//
// A null from `readdir` ends the walk, whether it means end-of-directory or an error
// mid-read. Distinguishing them needs `errno`, which nothing in this language reads yet —
// the same answer `read_file` gives for why it returns `None` rather than a reason.
func (l *lowerer) ensureReadDirRuntime(dt types.DataType, elemLyra types.Type,
	someC types.DataTypeConstructor, someTag int, noneC types.DataTypeConstructor, noneTag int) (*ir.Func, error) {
	if l.readDir != nil {
		return l.readDir, nil
	}
	nameOffset, err := direntNameOffset()
	if err != nil {
		return nil, err
	}
	dyn, ok := elemLyra.(types.DynamicArrayType)
	if !ok {
		return nil, fmt.Errorf("llvm: dir_names must answer a []string, got %s", elemLyra)
	}
	l.ensureRCRuntime()

	unionTy, err := l.lowerType(dt)
	if err != nil {
		return nil, err
	}
	i8ptr := lltypes.NewPointer(lltypes.I8)
	// `DIR *` is opaque and only ever carried, never indexed — the one thing tui.go's
	// termios buffer and this have in common.
	opendir, _ := l.declareLibc("opendir", i8ptr, i8ptr)
	readdir, _ := l.declareLibc(readdirSymbol(), i8ptr, i8ptr)
	closedir, _ := l.declareLibc("closedir", lltypes.I32, i8ptr)

	path := ir.NewParam("path", i8ptr)
	fn := l.module.NewFunc(ShimReadDir, unionTy, path)
	entry := fn.NewBlock("entry")
	noDir := fn.NewBlock("no_dir")
	opened := fn.NewBlock("opened")
	loop := fn.NewBlock("loop")
	entryFound := fn.NewBlock("entry_found")
	done := fn.NewBlock("done")

	dir := entry.NewCall(opendir, path)
	entry.NewCondBr(entry.NewICmp(enum.IPredEQ, dir, constant.NewNull(i8ptr)), noDir, opened)

	noneVal, err := l.buildDataValue(noDir, dt, noneTag, noneC, nil)
	if err != nil {
		return nil, err
	}
	noDir.NewRet(noneVal)

	// **The box is allocated past the null test, not before it.** Building it in the entry
	// block would allocate one on the `None` path too, which returns without a box to hand
	// back and so without anything to free it — a leak on exactly the path a caller takes
	// when a directory is missing, which is the common one.
	//
	// It starts empty and grows by push: the entry count is not knowable without walking
	// the directory twice, and a directory is walked once.
	box, boxTy, _, err := l.dynArrayBox(opened, dyn.ElementType, 0)
	if err != nil {
		return nil, err
	}
	elemSize, elemAlign, ok := SizeAndAlign(l.resolveForLayout(dyn.ElementType))
	if !ok {
		return nil, fmt.Errorf("llvm: cannot size dir_names element type %s", dyn.ElementType)
	}
	stride := int64(alignUp(elemSize, elemAlign))
	opened.NewBr(loop)

	ent := loop.NewCall(readdir, dir)
	loop.NewCondBr(loop.NewICmp(enum.IPredEQ, ent, constant.NewNull(i8ptr)), done, entryFound)

	namePtr := entryFound.NewGetElementPtr(lltypes.I8, ent, i64c(nameOffset))
	byteLen := entryFound.NewCall(l.ensureCStrLenRuntime(), namePtr)
	_, dst := l.rcAllocStringPayload(entryFound, byteLen)
	entryFound.NewCall(l.memcpyFunc(), dst, namePtr, byteLen)
	count := entryFound.NewCall(l.utf8CountFunc(), dst, byteLen)
	str := makeString(entryFound, dst, byteLen, count)
	pushed := l.emitDynArrayPush(entryFound, boxTy, box, str, stride)
	pushed.NewBr(loop)

	done.NewCall(closedir, dir)
	someVal, err := l.buildDataValue(done, dt, someTag, someC, []value.Value{box})
	if err != nil {
		return nil, err
	}
	done.NewRet(someVal)

	l.readDir = fn
	return fn, nil
}

// lowerDirNamesCall lowers `dir_names(path)` to a single call returning the canonical
// `Maybe<[]string>`.
//
// **The call site emits no branches**, for the reason input.go records at length: an owned
// builtin result that behaves exactly like an ordinary function call is the cheaper thing
// to reason about than one that is merely handled correctly. Every test and both union
// constructions live inside the shim.
func (l *lowerer) lowerDirNamesCall(block *ir.Block, e *ast.FunctionCallExpr) (value.Value, *ir.Block, error) {
	if len(e.Arguments) != 1 {
		return nil, nil, fmt.Errorf("llvm: dir_names expects 1 argument, got %d", len(e.Arguments))
	}
	recorded, ok := l.recordedType(e)
	if !ok {
		return nil, nil, fmt.Errorf("llvm: dir_names call has no recorded type")
	}
	dt, ok := recorded.(types.DataType)
	if !ok {
		return nil, nil, fmt.Errorf("llvm: dir_names must return a Maybe, got %s", recorded)
	}
	someC, someTag, hasSome := findConstructor(dt, "Some")
	noneC, noneTag, hasNone := findConstructor(dt, "None")
	if !hasSome || !hasNone {
		return nil, nil, fmt.Errorf("llvm: dir_names's return type %q is not a canonical Maybe", dt.Name)
	}
	if len(someC.Params) != 1 {
		return nil, nil, fmt.Errorf("llvm: dir_names's Some must carry one payload, got %d", len(someC.Params))
	}
	pathV, block, err := l.lowerExpr(block, e.Arguments[0])
	if err != nil {
		return nil, nil, err
	}
	if diverged(pathV, block) {
		return nil, block, nil
	}
	if !isStringLLVMType(pathV.Type()) {
		return nil, nil, fmt.Errorf("llvm: dir_names's path did not lower to a string (%s)", pathV.Type())
	}
	// The bytes go to C as they are: a string's payload is already NUL-terminated past its
	// length, which is what `s.cstring_ptr()` relies on. Its interior-NUL trap comes along
	// too, and for the same reason — a path holding a zero byte would arrive at `opendir`
	// truncated, and would open *a different directory* rather than fail.
	cpath := block.NewExtractValue(pathV, 0)
	i8ptr := lltypes.NewPointer(lltypes.I8)
	memchr, _ := l.declareLibc("memchr", i8ptr, i8ptr, lltypes.I32, lltypes.I64)
	found := block.NewCall(memchr, cpath, i32c(0), block.NewExtractValue(pathV, 1))
	block = l.emitTrapIf(block,
		block.NewICmp(enum.IPredNE, found, constant.NewNull(i8ptr)),
		l.panicInteriorNULFunc())
	fn, err := l.ensureReadDirRuntime(dt, someC.Params[0], someC, someTag, noneC, noneTag)
	if err != nil {
		return nil, nil, err
	}
	return block.NewCall(fn, cpath), block, nil
}
