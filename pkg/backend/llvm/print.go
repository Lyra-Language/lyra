package llvm

import (
	"fmt"

	"github.com/llir/llvm/ir"
	"github.com/llir/llvm/ir/constant"
	"github.com/llir/llvm/ir/enum"
	lltypes "github.com/llir/llvm/ir/types"
	"github.com/llir/llvm/ir/value"

	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/types"
)

// writeFunc lazily declares libc's `i64 @write(i32 fd, i8* buf, i64 count)`
// (POSIX `ssize_t write(int, const void*, size_t)` — i64 ssize_t/size_t, i32 int
// on a 64-bit target), caching it so every print shares one declaration. clang
// links libc, so no runtime object is needed — the same self-contained story as
// memcmp/memcpy/malloc.
func (l *lowerer) writeFunc() *ir.Func {
	fn, _ := l.declareLibc("write", lltypes.I64,
		lltypes.I32, lltypes.NewPointer(lltypes.I8), lltypes.I64)
	return fn
}

// snprintfFunc lazily declares libc's variadic
// `i32 @snprintf(i8* str, i64 size, i8* format, ...)`. Numeric formatting writes
// into a caller stack buffer (not stdio), so — unlike printf — it never buffers
// output and stays in program order with the raw `write`s that string/bool/rune
// print use.
func (l *lowerer) snprintfFunc() *ir.Func {
	i8ptr := lltypes.NewPointer(lltypes.I8)
	fn, fresh := l.declareLibc("snprintf", lltypes.I32, i8ptr, lltypes.I64, i8ptr)
	if fresh {
		fn.Sig.Variadic = true
	}
	return fn
}

// floatStrBufBytes sizes the stack buffer a formatted float is written into. The
// longest output is a 17-significant-digit f64 in exponent form with a sign on
// both the number and the exponent — `-1.2345678901234567e-308`, 24 bytes — so 32
// leaves room and keeps the alloca a round number. snprintf is given this size and
// truncates rather than overruns even if that reasoning is ever wrong.
const floatStrBufBytes = 32

// strtodFunc lazily declares libc's `double @strtod(i8* nptr, i8** endptr)`. It is
// what makes the formatter below a *round-trip* check rather than a guess: the only
// way to know a decimal string denotes the same float is to parse it back.
func (l *lowerer) strtodFunc() *ir.Func {
	i8ptr := lltypes.NewPointer(lltypes.I8)
	fn, _ := l.declareLibc("strtod", lltypes.Double, i8ptr, lltypes.NewPointer(i8ptr))
	return fn
}

// floatPrecisionLadder gives the significant-digit counts to try for a float
// width, lowest first, and the LLVM type to narrow back to when checking whether a
// candidate round-trips.
//
// The high end of each ladder is the width's round-trip guarantee (IEEE-754's
// "decimal digits needed to distinguish all values"): 17 for binary64, 9 for
// binary32, 5 for binary16. So the last rung *always* succeeds and the loop always
// terminates with a faithful answer.
//
// The low end is where a search would otherwise waste iterations. `%g` strips
// trailing zeros, so asking for 15 significant digits of 0.1 already yields `0.1`
// rather than `0.100000000000000` — the first rung is normally the answer, and a
// value that genuinely needs more digits costs at most two more libc round trips.
func floatPrecisionLadder(name types.PrimitiveTypeName) ([]int64, lltypes.Type, error) {
	switch name {
	// An untyped float literal defaults to f64 (the language's rule), and one can
	// reach print with its type still untyped — `println(0.1 + 0.2)` never meets a
	// context that pins a width.
	case types.Float64, types.UntypedFloat:
		return []int64{15, 16, 17}, lltypes.Double, nil
	case types.Float32:
		return []int64{6, 7, 8, 9}, lltypes.Float, nil
	case types.Float16:
		return []int64{3, 4, 5}, lltypes.Half, nil
	}
	return nil, nil, fmt.Errorf("llvm: no float formatter for %s", name)
}

// floatToStrFunc defines (once per printed float width) a formatter that writes the
// **shortest of its ladder's renderings that reads back as the same value**, and
// returns the byte length. One function per width rather than one taking the width
// as a parameter, so each bakes in its own precision ladder and narrowing with no
// runtime dispatch; only the widths a program actually prints are emitted.
//
// Until 08/13 printing a float was one `snprintf("%g")`, whose default is **six**
// significant digits — so `println(0.1 + 0.2)` printed `0.3`, `1.0 / 3.0` printed
// `0.333333`, and `1234567890.0` printed `1.23457e+09`. Every one of those is a
// different number from the one the program held, printed without a hint that
// anything was dropped, which is the silent-wrongness this language exists to
// refuse. Reading a printed value back is the ordinary way to move data between
// programs, and it was not safe to do.
//
// The loop is the classic printf-based construction: render at increasing
// precision, `strtod` the result, and stop at the first that comes back equal.
// **The comparison is at the value's own width**, which is why an f32's ladder
// narrows to `float` before comparing — 0.1f32 widened to double is
// 0.10000000149011612, so a check performed as double would reject `0.1` and print
// all of it. A NaN never compares equal, so it simply runs to the last rung and
// prints libc's `nan`; infinities round-trip immediately as `inf`.
//
// It is shortest *within the ladder*, not provably minimal — a value whose true
// minimum is 14 digits prints 15. The genuinely minimal algorithms (Ryu, Grisu)
// are a few hundred lines of hand-written IR here, and what actually mattered was
// that a printed float denote the value that was printed; that is now exact.
func (l *lowerer) floatToStrFunc(name types.PrimitiveTypeName) (*ir.Func, error) {
	if fn, ok := l.fmtFloat[string(name)]; ok {
		return fn, nil
	}
	ladder, narrowTy, err := floatPrecisionLadder(name)
	if err != nil {
		return nil, err
	}

	i8ptr := lltypes.NewPointer(lltypes.I8)
	val := ir.NewParam("val", lltypes.Double)
	out := ir.NewParam("out", i8ptr)
	fn := l.module.NewFunc("lyra_"+string(name)+"_to_str", lltypes.I64, val, out)

	entry := fn.NewBlock("entry")
	// endptr is required by strtod's signature; the parsed value is all we want.
	endptr := entry.NewAlloca(i8ptr)
	// The original, narrowed once to the value's own width — the thing each
	// candidate must match.
	want := narrowFromDouble(entry, val, narrowTy)

	// **A NaN is written by hand, because libc does not agree with itself about it.**
	// glibc renders a NaN whose sign bit is set as `-nan`; Apple's libc renders it
	// `nan`. So `(-1.0).log()` printed differently on Linux and macOS — a program's
	// *output* depending on the platform, which is the determinism this language gave up
	// platform-dependent integer widths to get.
	//
	// The sign of a NaN is the right thing to drop rather than to standardize on: IEEE
	// leaves it unspecified for almost every operation that produces one, so it carries
	// no meaning a program could act on. An infinity keeps its sign, which does.
	//
	// Written before the ladder rather than as another rung: a NaN never compares equal
	// to itself, so it would otherwise run every rung to reach the last one and print
	// whatever libc said there.
	nanBlock := fn.NewBlock("nan")
	ladderStart := fn.NewBlock("ladder")
	entry.NewCondBr(entry.NewFCmp(enum.FPredUNO, val, val), nanBlock, ladderStart)
	for i, ch := range []int64{'n', 'a', 'n'} {
		nanBlock.NewStore(constant.NewInt(lltypes.I8, ch),
			nanBlock.NewGetElementPtr(lltypes.I8, out, i64c(int64(i))))
	}
	nanBlock.NewRet(i64c(3))

	// One block per rung: render, parse back, and either stop or fall through to the
	// next. The final rung has nowhere to fall through to — its precision is the
	// width's round-trip guarantee, so it is correct by construction and taken
	// unconditionally.
	done := fn.NewBlock("done")
	lengths := make([]*ir.Incoming, 0, len(ladder))
	cur := ladderStart
	for i, prec := range ladder {
		n := cur.NewCall(l.snprintfFunc(), out, i64c(floatStrBufBytes),
			l.cString("%.*g"), i32c(prec), val)
		length := cur.NewSExt(n, lltypes.I64)
		lengths = append(lengths, ir.NewIncoming(length, cur))

		if i == len(ladder)-1 {
			cur.NewBr(done)
			break
		}
		parsed := cur.NewCall(l.strtodFunc(), out, endptr)
		got := narrowFromDouble(cur, parsed, narrowTy)
		next := fn.NewBlock("")
		cur.NewCondBr(cur.NewFCmp(enum.FPredOEQ, got, want), done, next)
		cur = next
	}

	done.NewRet(done.NewPhi(lengths...))
	l.fmtFloat[string(name)] = fn
	return fn, nil
}

// narrowFromDouble converts a double back to to, or returns it unchanged when to
// *is* double. Used to compare a candidate rendering against the original at the
// value's own width rather than at the width varargs promoted it to.
func narrowFromDouble(block *ir.Block, v value.Value, to lltypes.Type) value.Value {
	if to.Equal(lltypes.Double) {
		return v
	}
	return block.NewFPTrunc(v, to)
}

// cString interns a NUL-terminated byte string as a private immutable global and
// returns an i8* to its first byte, caching by content so repeated formats
// (`"%lld"`) share one global.
func (l *lowerer) cString(s string) value.Value {
	g, ok := l.cStrings[s]
	if !ok {
		bytes := append([]byte(s), 0) // NUL terminator for a C string
		g = l.privateConst(fmt.Sprintf(".cstr.%d", len(l.cStrings)), constant.NewCharArray(bytes))
		l.cStrings[s] = g
	}
	arrTy := g.ContentType
	zero := i32c(0)
	return constant.NewGetElementPtr(arrTy, g, zero, zero)
}

// newlinePtr returns an i8* to a single interned "\n" byte, used for println's
// trailing newline. The byte lives in one private immutable global shared across
// every println in the module.
func (l *lowerer) newlinePtr() value.Value {
	if l.newlineByte == nil {
		l.newlineByte = l.privateConst(".nl", constant.NewCharArray([]byte("\n")))
	}
	arrTy := lltypes.NewArray(1, lltypes.I8)
	zero := i32c(0)
	return constant.NewGetElementPtr(arrTy, l.newlineByte, zero, zero)
}

// runeToUTF8Func lazily defines `i64 @lyra_rune_to_utf8(i32 cp, i8* buf)`, which
// encodes a code point into buf (≥ 4 bytes) as UTF-8 and returns the byte count.
// A rune is an unvalidated code point, so this mirrors the standard 1–4 byte
// encoding by magnitude without a surrogate/range check (an out-of-range point
// just produces its 4-byte form) — matching the "unvalidated i32" contract the
// `rune` type documents.
func (l *lowerer) runeToUTF8Func() *ir.Func {
	if l.fmtRune != nil {
		return l.fmtRune
	}
	i8ptr := lltypes.NewPointer(lltypes.I8)
	cp := ir.NewParam("cp", lltypes.I32)
	buf := ir.NewParam("buf", i8ptr)
	fn := l.module.NewFunc("lyra_rune_to_utf8", lltypes.I64, cp, buf)

	entry := fn.NewBlock("entry")
	one := fn.NewBlock("one")      // cp < 0x80
	twoPlus := fn.NewBlock("twoP") // else
	two := fn.NewBlock("two")      // cp < 0x800
	threePlus := fn.NewBlock("triP")
	three := fn.NewBlock("three") // cp < 0x10000
	four := fn.NewBlock("four")   // else

	// storeByte writes the low 8 bits of v (an i32) into buf[idx].
	storeByte := func(b *ir.Block, idx int64, v value.Value) {
		ptr := b.NewGetElementPtr(lltypes.I8, buf, i64c(idx))
		b.NewStore(b.NewTrunc(v, lltypes.I8), ptr)
	}
	c := func(n int64) value.Value { return i32c(n) }

	entry.NewCondBr(entry.NewICmp(enum.IPredULT, cp, c(0x80)), one, twoPlus)

	storeByte(one, 0, cp)
	one.NewRet(i64c(1))

	twoPlus.NewCondBr(twoPlus.NewICmp(enum.IPredULT, cp, c(0x800)), two, threePlus)

	storeByte(two, 0, two.NewOr(c(0xC0), two.NewLShr(cp, c(6))))
	storeByte(two, 1, two.NewOr(c(0x80), two.NewAnd(cp, c(0x3F))))
	two.NewRet(i64c(2))

	threePlus.NewCondBr(threePlus.NewICmp(enum.IPredULT, cp, c(0x10000)), three, four)

	storeByte(three, 0, three.NewOr(c(0xE0), three.NewLShr(cp, c(12))))
	storeByte(three, 1, three.NewOr(c(0x80), three.NewAnd(three.NewLShr(cp, c(6)), c(0x3F))))
	storeByte(three, 2, three.NewOr(c(0x80), three.NewAnd(cp, c(0x3F))))
	three.NewRet(i64c(3))

	storeByte(four, 0, four.NewOr(c(0xF0), four.NewLShr(cp, c(18))))
	storeByte(four, 1, four.NewOr(c(0x80), four.NewAnd(four.NewLShr(cp, c(12)), c(0x3F))))
	storeByte(four, 2, four.NewOr(c(0x80), four.NewAnd(four.NewLShr(cp, c(6)), c(0x3F))))
	storeByte(four, 3, four.NewOr(c(0x80), four.NewAnd(cp, c(0x3F))))
	four.NewRet(i64c(4))

	l.fmtRune = fn
	return fn
}

// i128ToStrFunc lazily defines
// `i64 @lyra_i128_to_str(i128 val, i8* out, i1 isSigned)`, which writes the
// base-10 text of val into out (which must have room for ≥ 40 bytes: an optional
// '-' plus up to 39 digits) and returns the byte count. There is no printf length
// modifier for 128-bit integers, so this is the hand-written formatter the i128/
// u128 print path needs (todo.md's i128 change set, step 5).
//
// The algorithm is the textbook repeated-divmod-by-10: produce the digits of the
// magnitude least-significant first into a scratch buffer, then copy them out in
// reverse (most-significant first) behind an optional sign. The magnitude is taken
// as the *unsigned* bit pattern `select(isNeg, 0-val, val)`, which is correct even
// for INT_MIN — `0 - INT_MIN` wraps to INT_MIN's bit pattern, whose unsigned value
// is exactly 2^127, the true magnitude — so `udiv`/`urem` by 10 (128-bit division
// via compiler-rt, linked by clang) yield the right digits without an overflowing
// negation. Both signed (isSigned=1) and unsigned (isSigned=0) callers share it.
func (l *lowerer) i128ToStrFunc() *ir.Func {
	if l.fmtI128 != nil {
		return l.fmtI128
	}
	i8ptr := lltypes.NewPointer(lltypes.I8)
	val := ir.NewParam("val", lltypes.I128)
	out := ir.NewParam("out", i8ptr)
	isSigned := ir.NewParam("isSigned", lltypes.I1)
	fn := l.module.NewFunc("lyra_i128_to_str", lltypes.I64, val, out, isSigned)

	entry := fn.NewBlock("entry")
	loop := fn.NewBlock("loop")
	reverse := fn.NewBlock("reverse")
	writeMinus := fn.NewBlock("writeMinus")
	revloop := fn.NewBlock("revloop")
	done := fn.NewBlock("done")

	i64 := func(n int64) *constant.Int { return i64c(n) }
	i128 := func(n int64) *constant.Int { return constant.NewInt(lltypes.I128, n) }

	// Entry: allocas + sign/magnitude setup.
	scratch := entry.NewAlloca(lltypes.NewArray(40, lltypes.I8))
	scratchPtr := entry.NewGetElementPtr(lltypes.NewArray(40, lltypes.I8), scratch, i64(0), i64(0))
	magSlot := entry.NewAlloca(lltypes.I128)
	countSlot := entry.NewAlloca(lltypes.I64)
	srcSlot := entry.NewAlloca(lltypes.I64)
	dstSlot := entry.NewAlloca(lltypes.I64)

	isNeg := entry.NewAnd(isSigned, entry.NewICmp(enum.IPredSLT, val, i128(0)))
	mag0 := entry.NewSelect(isNeg, entry.NewSub(i128(0), val), val)
	entry.NewStore(mag0, magSlot)
	entry.NewStore(i64(0), countSlot)
	entry.NewBr(loop)

	// Loop: emit one decimal digit per iteration (do-while, so 0 emits "0").
	curMag := loop.NewLoad(lltypes.I128, magSlot)
	count := loop.NewLoad(lltypes.I64, countSlot)
	digit := loop.NewURem(curMag, i128(10))
	digitChar := loop.NewAdd(loop.NewTrunc(digit, lltypes.I8), constant.NewInt(lltypes.I8, '0'))
	loop.NewStore(digitChar, loop.NewGetElementPtr(lltypes.I8, scratchPtr, count))
	nextMag := loop.NewUDiv(curMag, i128(10))
	loop.NewStore(nextMag, magSlot)
	loop.NewStore(loop.NewAdd(count, i64(1)), countSlot)
	loop.NewCondBr(loop.NewICmp(enum.IPredNE, nextMag, i128(0)), loop, reverse)

	// Reverse: set up the output cursor (past a '-' if negative) and the scratch
	// read index (most-significant digit first), then write the sign.
	nDigits := reverse.NewLoad(lltypes.I64, countSlot)
	reverse.NewStore(reverse.NewSelect(isNeg, i64(1), i64(0)), dstSlot)
	reverse.NewStore(reverse.NewSub(nDigits, i64(1)), srcSlot)
	reverse.NewCondBr(isNeg, writeMinus, revloop)

	minusPtr := writeMinus.NewGetElementPtr(lltypes.I8, out, i64(0))
	writeMinus.NewStore(constant.NewInt(lltypes.I8, '-'), minusPtr)
	writeMinus.NewBr(revloop)

	// Revloop: copy scratch[src] → out[dst], walking src down to 0.
	src := revloop.NewLoad(lltypes.I64, srcSlot)
	dst := revloop.NewLoad(lltypes.I64, dstSlot)
	ch := revloop.NewLoad(lltypes.I8, revloop.NewGetElementPtr(lltypes.I8, scratchPtr, src))
	revloop.NewStore(ch, revloop.NewGetElementPtr(lltypes.I8, out, dst))
	revloop.NewStore(revloop.NewSub(src, i64(1)), srcSlot)
	revloop.NewStore(revloop.NewAdd(dst, i64(1)), dstSlot)
	revloop.NewCondBr(revloop.NewICmp(enum.IPredNE, src, i64(0)), revloop, done)

	done.NewRet(done.NewLoad(lltypes.I64, dstSlot))

	l.fmtI128 = fn
	return fn
}

// formatForPrint turns a lowered value of Lyra type argType into a
// (i8* data, i64 length) pair ready for `write(1, data, length)`:
//   - string: the fat pointer's own { data, len } fields (no copy).
//   - bool:   a pointer/length selected between interned "true"/"false".
//   - rune:   UTF-8 encoded into a stack buffer via lyra_rune_to_utf8.
//   - int:    snprintf "%lld"/"%llu" (by signedness) into a stack buffer.
//   - float:  snprintf "%g" into a stack buffer.
//
// Numeric/rune buffers are entry-block allocas (allocated once even inside a
// loop). The typechecker guarantees argType is one of these printable primitives.
func (l *lowerer) formatForPrint(block *ir.Block, val value.Value, argType types.Type) (value.Value, value.Value, error) {
	prim, ok := argType.(types.PrimitiveType)
	if !ok {
		return nil, nil, fmt.Errorf("llvm: print of non-printable type %s", argType)
	}
	entry := block.Parent.Blocks[0]
	_, isFloat := val.Type().(*lltypes.FloatType)

	switch {
	case prim.Name == types.String:
		return block.NewExtractValue(val, 0), block.NewExtractValue(val, 1), nil

	case prim.Name == types.Boolean:
		truePtr := l.cString("true")
		falsePtr := l.cString("false")
		dataPtr := block.NewSelect(val, truePtr, falsePtr)
		length := block.NewSelect(val, i64c(4), i64c(5))
		return dataPtr, length, nil

	case prim.Name == types.Rune:
		buf := entry.NewAlloca(lltypes.NewArray(4, lltypes.I8))
		dataPtr := block.NewGetElementPtr(lltypes.NewArray(4, lltypes.I8), buf,
			i64c(0), i64c(0))
		length := block.NewCall(l.runeToUTF8Func(), val, dataPtr)
		return dataPtr, length, nil

	case isFloat:
		buf := entry.NewAlloca(lltypes.NewArray(floatStrBufBytes, lltypes.I8))
		dataPtr := block.NewGetElementPtr(lltypes.NewArray(floatStrBufBytes, lltypes.I8), buf,
			i64c(0), i64c(0))
		// Widened to double because that is what a vararg float becomes anyway, and
		// because every f16/f32 is exactly representable as one — so the formatter
		// can do its round-trip check against the *original* width without loss.
		promoted := coerceFloatWidth(block, val, lltypes.Double)
		fn, err := l.floatToStrFunc(prim.Name)
		if err != nil {
			return nil, nil, err
		}
		return dataPtr, block.NewCall(fn, promoted, dataPtr), nil

	case prim.Name == types.Int128 || prim.Name == types.UInt128:
		// No printf length modifier reaches 128 bits, so a hand-written base-10
		// formatter renders it (40 bytes covers an optional '-' + up to 39 digits).
		buf := entry.NewAlloca(lltypes.NewArray(40, lltypes.I8))
		dataPtr := block.NewGetElementPtr(lltypes.NewArray(40, lltypes.I8), buf,
			i64c(0), i64c(0))
		signedBit := int64(0)
		if IsSignedInt(prim.Name) {
			signedBit = 1
		}
		length := block.NewCall(l.i128ToStrFunc(), val, dataPtr, constant.NewInt(lltypes.I1, signedBit))
		return dataPtr, length, nil

	default: // an integer type
		buf := entry.NewAlloca(lltypes.NewArray(24, lltypes.I8))
		dataPtr := block.NewGetElementPtr(lltypes.NewArray(24, lltypes.I8), buf,
			i64c(0), i64c(0))
		signed := IsSignedInt(prim.Name)
		promoted := coerceIntWidth(block, val, signed, lltypes.I64) // widen to long long
		format := "%llu"
		if signed {
			format = "%lld"
		}
		n := block.NewCall(l.snprintfFunc(), dataPtr, i64c(24), l.cString(format), promoted)
		return dataPtr, block.NewSExt(n, lltypes.I64), nil
	}
}

// lowerPrintCall lowers a `print(x)` / `println(x)` call: format the argument to
// bytes per its type (formatForPrint) and write them to stdout (fd 1) via libc
// `write`, then — for println — a trailing newline. `print` is a void statement,
// so its value is discarded; this returns the last `write` result. stdout
// ordering across calls is preserved because each print is its own `write` in
// program order (numeric formatting goes through snprintf-into-memory, not
// stdio, so it doesn't buffer ahead of a raw write).
func (l *lowerer) lowerPrintCall(block *ir.Block, e *ast.FunctionCallExpr, newline bool) (value.Value, *ir.Block, error) {
	arg := e.Arguments[0]
	argType, ok := l.recordedType(arg)
	if !ok {
		return nil, nil, fmt.Errorf("llvm: no type recorded for print argument")
	}
	val, block, err := l.lowerExpr(block, arg)
	if err != nil {
		return nil, nil, err
	}
	if diverged(val, block) {
		return nil, block, nil
	}
	dataPtr, length, err := l.formatForPrint(block, val, argType)
	if err != nil {
		return nil, nil, err
	}
	fd := i32c(1) // stdout
	result := block.NewCall(l.writeFunc(), fd, dataPtr, length)
	if newline {
		result = block.NewCall(l.writeFunc(), fd, l.newlinePtr(), i64c(1))
	}
	return result, block, nil
}
