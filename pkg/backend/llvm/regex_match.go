package llvm

import (
	"fmt"

	"github.com/llir/llvm/ir"
	"github.com/llir/llvm/ir/constant"
	"github.com/llir/llvm/ir/enum"
	lltypes "github.com/llir/llvm/ir/types"
	"github.com/llir/llvm/ir/value"

	"github.com/Lyra-Language/lyra/pkg/regex"
)

// Regex matching at run time, without a regex engine in the runtime (08/13).
//
// Lyra's runtime is hand-written shims and libc, with no FFI, so `lyra-E052` and
// `lyra-E054` both recorded the same absence: nothing could match a pattern against
// a value the compiler had not already read. The way out is that **a pattern never
// needs compiling at run time.** A `where pattern(r"…")` constraint is part of a
// type and `r"…"` is a literal, so the pattern is always known while compiling —
// which means the engine can run at compile time and only its *answer* need ship.
//
// What ships is two tables and a loop. `pkg/regex` flattens its derivative-based
// DFA into `Trans[state*256+byte]` with the beginning- and end-of-text boundaries
// folded in (regex.Matcher), the tables become private constant globals, and one
// shared driver walks them. Matching is O(n) in the input with no backtracking and
// no allocation, which is what a DFA buys.
//
// **The table is verified against the engine rather than against expectations**
// (pkg/regex/matcher_test.go runs both over a corpus, newline edge cases included),
// because the compile-time and run-time answers for one pattern must agree — a
// value passing one and failing the other is worse than either.

// regexTables is the emitted form of one compiled pattern: pointers to its three
// constant globals plus its start state.
type regexTables struct {
	trans       value.Value // i32*, [states*256]
	newlineLast value.Value // i32*, [states]
	acceptFinal value.Value // i8*,  [states]
	start       int32
}

// regexTablesFor compiles pattern and emits its tables as private constant globals,
// caching by pattern text so a newtype used in twenty places emits one table.
func (l *lowerer) regexTablesFor(pattern string) (*regexTables, error) {
	if t, ok := l.regexTables[pattern]; ok {
		return t, nil
	}
	m, err := regex.CompileMatcher(pattern, regex.MaxTableStates)
	if err != nil {
		return nil, err
	}

	idx := len(l.regexTables)
	trans := make([]constant.Constant, len(m.Trans))
	for i, v := range m.Trans {
		trans[i] = i32c(int64(v))
	}
	nl := make([]constant.Constant, len(m.NewlineLast))
	for i, v := range m.NewlineLast {
		nl[i] = i32c(int64(v))
	}
	acc := make([]byte, len(m.AcceptFinal))
	for i, v := range m.AcceptFinal {
		if v {
			acc[i] = 1
		}
	}

	t := &regexTables{
		trans:       l.constGlobal(fmt.Sprintf(".re.trans.%d", idx), constant.NewArray(lltypes.NewArray(uint64(len(trans)), lltypes.I32), trans...)),
		newlineLast: l.constGlobal(fmt.Sprintf(".re.nl.%d", idx), constant.NewArray(lltypes.NewArray(uint64(len(nl)), lltypes.I32), nl...)),
		acceptFinal: l.constGlobal(fmt.Sprintf(".re.acc.%d", idx), constant.NewCharArray(acc)),
		start:       m.Start,
	}
	l.regexTables[pattern] = t
	return t, nil
}

// constGlobal defines a private immutable global and returns a pointer to its first
// element, the same shape cString uses for an interned byte string.
func (l *lowerer) constGlobal(name string, init constant.Constant) value.Value {
	g := l.privateConst(name, init)
	zero := i32c(0)
	return constant.NewGetElementPtr(g.ContentType, g, zero, zero)
}

// regexMatchFunc lazily defines the one driver every pattern shares:
//
//	i1 @lyra_regex_match(i8* data, i64 len, i32* trans, i32* nlLast, i8* acceptFinal, i32 start)
//
// It is `Matcher.Match` instruction for instruction — walk the bytes, take the
// newline-final column for a trailing '\n', stop early at the dead state, and end
// in one lookup — so the Go table test covers the emitted code's algorithm too.
//
// State and index live in allocas rather than phis: mem2reg promotes them at any
// optimization level this compiler emits (-O2 by default), and the straight-line
// version is markedly easier to read against the Go original, which is the property
// that matters for something that must agree with another implementation.
func (l *lowerer) regexMatchFunc() *ir.Func {
	if l.regexMatch != nil {
		return l.regexMatch
	}
	i8ptr := lltypes.NewPointer(lltypes.I8)
	i32ptr := lltypes.NewPointer(lltypes.I32)

	data := ir.NewParam("data", i8ptr)
	length := ir.NewParam("len", lltypes.I64)
	trans := ir.NewParam("trans", i32ptr)
	nlLast := ir.NewParam("nlLast", i32ptr)
	acceptFinal := ir.NewParam("acceptFinal", i8ptr)
	start := ir.NewParam("start", lltypes.I32)
	fn := l.module.NewFunc("lyra_regex_match", lltypes.I1, data, length, trans, nlLast, acceptFinal, start)

	entry := fn.NewBlock("entry")
	cond := fn.NewBlock("cond")
	body := fn.NewBlock("body")
	nlPath := fn.NewBlock("nl")
	bytePath := fn.NewBlock("byte")
	after := fn.NewBlock("after")
	dead := fn.NewBlock("dead")
	done := fn.NewBlock("done")

	statePtr := entry.NewAlloca(lltypes.I32)
	iPtr := entry.NewAlloca(lltypes.I64)
	entry.NewStore(start, statePtr)
	entry.NewStore(i64c(0), iPtr)
	entry.NewBr(cond)

	i := cond.NewLoad(lltypes.I64, iPtr)
	cond.NewCondBr(cond.NewICmp(enum.IPredSLT, i, length), body, done)

	bi := body.NewLoad(lltypes.I64, iPtr)
	b := body.NewLoad(lltypes.I8, body.NewGetElementPtr(lltypes.I8, data, bi))
	isNL := body.NewICmp(enum.IPredEQ, b, constant.NewInt(lltypes.I8, '\n'))
	isLast := body.NewICmp(enum.IPredEQ, bi,
		body.NewSub(length, i64c(1)))
	body.NewCondBr(body.NewAnd(isNL, isLast), nlPath, bytePath)

	// A trailing newline takes its own column: IsMatch deliberately omits the
	// beginning-of-line boundary after the input's final byte, and the table
	// carries that difference rather than the loop re-deriving it.
	nlState := nlPath.NewLoad(lltypes.I32, statePtr)
	nlPath.NewStore(
		nlPath.NewLoad(lltypes.I32, nlPath.NewGetElementPtr(lltypes.I32, nlLast, nlState)),
		statePtr)
	nlPath.NewBr(after)

	byState := bytePath.NewLoad(lltypes.I32, statePtr)
	row := bytePath.NewMul(byState, i32c(256))
	off := bytePath.NewAdd(row, bytePath.NewZExt(b, lltypes.I32))
	bytePath.NewStore(
		bytePath.NewLoad(lltypes.I32, bytePath.NewGetElementPtr(lltypes.I32, trans, off)),
		statePtr)
	bytePath.NewBr(after)

	// The dead state is absorbing, so reaching it ends the run — the early exit a
	// DFA gets for free and the reason matching is linear with no backtracking.
	afterState := after.NewLoad(lltypes.I32, statePtr)
	afterI := after.NewLoad(lltypes.I64, iPtr)
	after.NewStore(after.NewAdd(afterI, i64c(1)), iPtr)
	after.NewCondBr(
		after.NewICmp(enum.IPredEQ, afterState, i32c(int64(regex.DeadState))),
		dead, cond)

	dead.NewRet(constant.NewInt(lltypes.I1, 0))

	doneState := done.NewLoad(lltypes.I32, statePtr)
	accByte := done.NewLoad(lltypes.I8, done.NewGetElementPtr(lltypes.I8, acceptFinal, doneState))
	done.NewRet(done.NewICmp(enum.IPredNE, accByte, constant.NewInt(lltypes.I8, 0)))

	l.regexMatch = fn
	return fn
}

// emitPatternCheck traps when the string val does not match pattern. val is a Lyra
// string — the `{data, byte_len, rune_count}` fat pointer — and the match runs over
// its bytes, which is exact rather than an approximation: the compile-time check
// matches the same bytes, and UTF-8 is self-synchronizing so a byte-level DFA over
// valid UTF-8 answers the same question a rune-level one would.
func (l *lowerer) emitPatternCheck(block *ir.Block, val value.Value, pattern string) (*ir.Block, error) {
	tables, err := l.regexTablesFor(pattern)
	if err != nil {
		return nil, err
	}
	data := block.NewExtractValue(val, 0)
	byteLen := block.NewExtractValue(val, 1)
	matched := block.NewCall(l.regexMatchFunc(), data, byteLen,
		tables.trans, tables.newlineLast, tables.acceptFinal,
		i32c(int64(tables.start)))
	return l.emitTrapIf(block, block.NewICmp(enum.IPredEQ, matched, constant.NewInt(lltypes.I1, 0)),
		l.panicConstraintFunc()), nil
}
