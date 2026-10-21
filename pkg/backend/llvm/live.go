package llvm

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/llir/llvm/ir"
	"github.com/llir/llvm/ir/constant"
	"github.com/llir/llvm/ir/enum"
	lltypes "github.com/llir/llvm/ir/types"
)

// Live reload (`lyrac run --live`; the design is pkg/backend/LIVE.md).
//
// A live program is built in **generations**. Generation 0 is the executable: the program
// as always, except that every call **in `main`** to a Lyra function loads its callee from
// a slot (`@lyra.live.slot.<symbol>`), and every loop back-edge in `main` polls a flag a
// SIGUSR1 sets. Generation N ≥ 1 is the whole program again, built as a library whose
// mutable globals are *declarations* — the dynamic linker binds them to generation 0's, so
// the new code sees the state the old code made — and whose `lyra_live_patch` stores its
// functions' addresses in generation 0's slots. Generation 0's `lyra.live.apply`, reached
// from a poll, reads the library's path from a pipe, `dlopen`s it and calls that.
//
// **Only `main` polls and only `main` calls through slots**, because `main`'s is the one
// frame that stays on the stack: everything it calls returns every time round its loop, so
// at a poll no other function is mid-call, and the next call reaches the new code. The rest
// of the program keeps its direct calls and its inlining.
//
// What may be swapped is lyrac's to decide (cmd/lyrac/live.go), from two generations'
// LiveInfo — `main` unchanged, every type and global the same shape — before the program
// hears of it. Nothing here checks compatibility.

// LiveInfo is what one live build's program looks like to the compatibility check: the
// parts generation N must share with generation 0 for its code to run on generation 0's
// state.
type LiveInfo struct {
	// Main is `main`'s IR with every symbol the backend numbers program-wide — a string
	// literal's `.str.N`, a lambda's `lyra_closure_N` — replaced by a hash of what it
	// defines (normalizeLive), so a lambda added in another file does not read as a change
	// to `main`.
	Main string
	// Globals are the mutable globals generation N binds to generation 0's — every
	// top-level `let`/`var`, and the runtime's own state (the program's arguments, the
	// saved terminal) — by symbol, each with its LLVM type.
	Globals map[string]string
	// Types are the module's named LLVM types, by name, each with its definition: the
	// layout of a value already in memory.
	Types map[string]string
	// Slots are the functions `main` calls through a slot, by symbol.
	Slots []string
}

// Live makes the next Emit a live build's generation gen: 0 for the executable, N ≥ 1 for
// a library to be loaded into it. b, for chaining.
func (b *Backend) Live(gen int) *Backend {
	b.live = &liveBuild{gen: gen}
	return b
}

// LiveInfo is the last live Emit's, or nil.
func (b *Backend) LiveInfo() *LiveInfo { return b.liveInfo }

// LivePatchSymbol is the function a later generation exports for generation 0 to call.
const LivePatchSymbol = "lyra_live_patch"

// LiveEnv is the environment variable that tells a generation 0 it was started by
// `lyrac run --live`, and so may read and write the pipe's two descriptors: liveReadFD
// carries the path of each library to load, liveWriteFD the program's replies — `ready`
// once its handler is installed, then `applied` or `failed: <why>` per library.
const LiveEnv = "LYRA_LIVE"

const (
	liveReadFD  = 3 // os/exec's ExtraFiles[0]
	liveWriteFD = 4
	// liveGlobalsInit holds the top-level initializers in a live build, rather than
	// `main`: they are part of the program generation N replaces, and `main` is the part
	// it keeps, compared byte for byte.
	liveGlobalsInit = "lyra.live.init_globals"
	liveSlotPrefix  = "lyra.live.slot."
)

type liveBuild struct {
	gen int
}

// liveSignals is SIGUSR1, and the dlopen flags (RTLD_NOW | RTLD_LOCAL), per OS.
var liveSignals = map[string]struct{ usr1, dlopenFlags int64 }{
	"darwin": {30, 2 | 4},
	"linux":  {10, 2},
}

// LiveSignal is the number of SIGUSR1 on os, which lyrac sends a live program — the same
// table the program's handler is installed from.
func LiveSignal(os string) (int, bool) {
	sig, ok := liveSignals[os]
	return int(sig.usr1), ok
}

// finishLive is the live half of emitModule, run on the finished module: generation 0
// gets its slots, polls and loader; a later generation loses `main` and binds to
// generation 0's state instead.
func (l *lowerer) finishLive(m *ir.Module) (*LiveInfo, error) {
	sig, ok := liveSignals[l.os]
	if !ok {
		return nil, fmt.Errorf("llvm: live reload is not available for %q", l.os)
	}
	mainFn := findFunc(m, l.entrySymbol())
	if mainFn == nil {
		return nil, fmt.Errorf("llvm: live build has no %s", l.entrySymbol())
	}
	info := &LiveInfo{
		Main:    normalizeLive(m, mainFn.Name()),
		Globals: map[string]string{},
		Types:   map[string]string{},
	}
	for _, t := range m.TypeDefs {
		info.Types[t.Name()] = t.LLString()
	}
	for _, g := range m.Globals {
		if isLiveState(g) {
			info.Globals[g.Name()] = g.ContentType.LLString()
		}
	}
	callees := l.mainCallees(mainFn)
	for _, fn := range callees {
		info.Slots = append(info.Slots, fn.Name())
	}

	if l.live.gen == 0 {
		l.rewriteMainCalls(mainFn, callees)
		poll := l.emitLivePoll(sig.dlopenFlags)
		insertPolls(mainFn, poll)
		init := l.emitLiveInit(sig.usr1)
		entry := mainFn.Blocks[0]
		entry.Insts = slices.Insert(entry.Insts, 0, ir.Instruction(ir.NewCall(init)))
		return info, nil
	}

	// A later generation: generation 0's `main` stays, so this one's goes, with the
	// initializers it ran — the state they made is generation 0's, already made.
	m.Funcs = slices.DeleteFunc(m.Funcs, func(f *ir.Func) bool {
		return f == mainFn || f.Name() == liveGlobalsInit
	})
	for _, g := range m.Globals {
		if isLiveState(g) {
			g.Init = nil
			g.Linkage = enum.LinkageExternal
		}
	}
	l.emitLivePatch(callees)
	return info, nil
}

// entrySymbol is the C name the entry is emitted under.
func (l *lowerer) entrySymbol() string {
	if l.entry != "" {
		return l.entry
	}
	return "main"
}

func findFunc(m *ir.Module, name string) *ir.Func {
	for _, f := range m.Funcs {
		if f.Name() == name {
			return f
		}
	}
	return nil
}

// isLiveState reports whether g is state a later generation must share rather than
// define: a mutable global another image can see. A constant is the same in every
// generation and a private one is no other image's, so each generation keeps its own.
func isLiveState(g *ir.Global) bool {
	if g.Immutable || g.Init == nil {
		return false
	}
	switch g.Linkage {
	case enum.LinkagePrivate, enum.LinkageInternal:
		return false
	}
	return !strings.HasPrefix(g.Name(), liveSlotPrefix)
}

// mainCallees are the Lyra functions `main` calls directly, in the order it first calls
// them: a function, method or specialization written in Lyra, never an `extern` or the
// runtime's own glue (drop, retain, equality), which depend only on types — checked
// unchanged — and so are the same in every generation.
func (l *lowerer) mainCallees(mainFn *ir.Func) []*ir.Func {
	var out []*ir.Func
	seen := map[*ir.Func]bool{}
	for _, b := range mainFn.Blocks {
		for _, inst := range b.Insts {
			call, ok := inst.(*ir.InstCall)
			if !ok {
				continue
			}
			fn, ok := call.Callee.(*ir.Func)
			if !ok || seen[fn] || !l.sourceFuncs[fn] || len(fn.Blocks) == 0 {
				continue
			}
			if _, isExtern := l.externs[fn.Name()]; isExtern {
				continue
			}
			seen[fn] = true
			out = append(out, fn)
		}
	}
	return out
}

// rewriteMainCalls points each of main's calls to callees at a load of the callee's slot,
// a global initialized to the function. The load is also what keeps LLVM from inlining
// the callee into `main`, which would leave nothing to patch.
func (l *lowerer) rewriteMainCalls(mainFn *ir.Func, callees []*ir.Func) {
	slots := map[*ir.Func]*ir.Global{}
	for _, fn := range callees {
		g := l.module.NewGlobalDef(liveSlotPrefix+fn.Name(), fn)
		slots[fn] = g
	}
	n := 0
	for _, b := range mainFn.Blocks {
		insts := make([]ir.Instruction, 0, len(b.Insts))
		for _, inst := range b.Insts {
			if call, ok := inst.(*ir.InstCall); ok {
				if fn, ok := call.Callee.(*ir.Func); ok {
					if slot, ok := slots[fn]; ok {
						load := ir.NewLoad(fn.Type(), slot)
						// Named, so the IDs `main`'s unnamed values were given when its
						// text was taken (normalizeLive) still count up without a gap.
						load.SetName(fmt.Sprintf("live.callee.%d", n))
						n++
						insts = append(insts, load)
						call.Callee = load
					}
				}
			}
			insts = append(insts, inst)
		}
		b.Insts = insts
	}
}

// insertPolls puts a call to poll before the terminator of every block that jumps back to
// a block dominating it — each loop back-edge in `main`, the points between one turn of a
// loop and the next.
func insertPolls(mainFn *ir.Func, poll *ir.Func) {
	dom := newDomTree(mainFn)
	for _, b := range mainFn.Blocks {
		if b.Term == nil {
			continue
		}
		for _, succ := range b.Term.Succs() {
			if dom.dominates(succ, b) {
				call := ir.NewCall(poll)
				b.Insts = append(b.Insts, call)
				break
			}
		}
	}
}

// emitLivePoll emits generation 0's loader: `lyra.live.pending`, set by the signal
// handler; `lyra.live.poll`, which `main` calls at each back-edge and which costs a
// volatile load while nothing is pending; and `lyra.live.apply`, which loads one library
// and runs its patch.
//
// **The handler only sets the flag.** The work happens at a back-edge, on `main`'s own
// stack, at a point the compiler chose, so nothing but a store runs inside the signal.
func (l *lowerer) emitLivePoll(dlopenFlags int64) *ir.Func {
	i8ptr := lltypes.NewPointer(lltypes.I8)
	pending := l.module.NewGlobalDef("lyra.live.pending", i32c(0))
	pending.Linkage = enum.LinkageInternal
	l.livePending = pending

	readFn, _ := l.declareLibc("read", lltypes.I64, lltypes.I32, i8ptr, lltypes.I64)
	dlopen, _ := l.declareLibc("dlopen", i8ptr, i8ptr, lltypes.I32)
	dlsym, _ := l.declareLibc("dlsym", i8ptr, i8ptr, i8ptr)
	dlerror, _ := l.declareLibc("dlerror", i8ptr)
	cstrLen := l.ensureCStrLenRuntime()

	apply := l.module.NewFunc("lyra.live.apply", lltypes.Void)
	apply.Linkage = enum.LinkageInternal
	apply.FuncAttrs = append(apply.FuncAttrs, enum.FuncAttrNoInline)
	entry := apply.NewBlock("entry")
	got := apply.NewBlock("got")
	lookup := apply.NewBlock("lookup")
	patch := apply.NewBlock("patch")
	failed := apply.NewBlock("failed")
	done := apply.NewBlock("done")

	clear := entry.NewStore(i32c(0), pending)
	clear.Volatile = true
	const pathMax = 4096
	buf := entry.NewAlloca(lltypes.NewArray(pathMax, lltypes.I8))
	path := entry.NewGetElementPtr(buf.ElemType, buf, i64c(0), i64c(0))
	n := entry.NewCall(readFn, i32c(liveReadFD), path, i64c(pathMax-1))
	entry.NewCondBr(entry.NewICmp(enum.IPredSGT, n, i64c(0)), got, done)

	// lyrac ends each path with a newline, written whole (a pipe write that small is
	// atomic), and waits for the reply before sending another.
	last := got.NewGetElementPtr(lltypes.I8, path, got.NewSub(n, i64c(1)))
	got.NewStore(constant.NewInt(lltypes.I8, 0), last)
	handle := got.NewCall(dlopen, path, i32c(dlopenFlags))
	got.NewCondBr(got.NewICmp(enum.IPredEQ, handle, constant.NewNull(i8ptr)), failed, lookup)

	sym := lookup.NewCall(dlsym, handle, l.cString(LivePatchSymbol))
	lookup.NewCondBr(lookup.NewICmp(enum.IPredEQ, sym, constant.NewNull(i8ptr)), failed, patch)

	patchFn := patch.NewBitCast(sym, lltypes.NewPointer(lltypes.NewFunc(lltypes.Void)))
	patch.NewCall(patchFn)
	l.liveReply(patch, "applied\n")
	patch.NewBr(done)

	why := failed.NewCall(dlerror)
	l.liveReply(failed, "failed: ")
	failed.NewCall(l.writeFunc(), i32c(liveWriteFD), why, failed.NewCall(cstrLen, why))
	l.liveReply(failed, "\n")
	failed.NewBr(done)
	done.NewRet(nil)

	poll := l.module.NewFunc("lyra.live.poll", lltypes.Void)
	poll.Linkage = enum.LinkageInternal
	pe := poll.NewBlock("entry")
	run := poll.NewBlock("apply")
	back := poll.NewBlock("done")
	flag := pe.NewLoad(lltypes.I32, pending)
	flag.Volatile = true
	pe.NewCondBr(pe.NewICmp(enum.IPredNE, flag, i32c(0)), run, back)
	run.NewCall(apply)
	run.NewBr(back)
	back.NewRet(nil)
	return poll
}

// emitLiveInit emits what `main` runs first in generation 0: under `lyrac run --live`
// (LiveEnv set), install the SIGUSR1 handler and say `ready`; otherwise nothing, and the
// live pieces stay inert.
//
// The variable is unset and both descriptors closed on exec, so a program this one starts
// — Vega runs `lyrac` — does not take them for its own.
func (l *lowerer) emitLiveInit(usr1 int64) *ir.Func {
	i8ptr := lltypes.NewPointer(lltypes.I8)
	getenv, _ := l.declareLibc("getenv", i8ptr, i8ptr)
	unsetenv, _ := l.declareLibc("unsetenv", lltypes.I32, i8ptr)
	signal, _ := l.declareLibc("signal", i8ptr, lltypes.I32, i8ptr)
	fcntl := l.declareLibcVariadic("fcntl", lltypes.I32, lltypes.I32, lltypes.I32)

	handler := l.module.NewFunc("lyra.live.signal", lltypes.Void, ir.NewParam("sig", lltypes.I32))
	handler.Linkage = enum.LinkageInternal
	hb := handler.NewBlock("entry")
	set := hb.NewStore(i32c(1), l.livePending)
	set.Volatile = true
	hb.NewRet(nil)

	init := l.module.NewFunc("lyra.live.init", lltypes.Void)
	init.Linkage = enum.LinkageInternal
	entry := init.NewBlock("entry")
	install := init.NewBlock("install")
	done := init.NewBlock("done")
	name := l.cString(LiveEnv)
	on := entry.NewCall(getenv, name)
	entry.NewCondBr(entry.NewICmp(enum.IPredNE, on, constant.NewNull(i8ptr)), install, done)

	install.NewCall(unsetenv, name)
	const fSetFD, fdCloexec = 2, 1 // the same on macOS and Linux
	install.NewCall(fcntl, i32c(liveReadFD), i32c(fSetFD), i32c(fdCloexec))
	install.NewCall(fcntl, i32c(liveWriteFD), i32c(fSetFD), i32c(fdCloexec))
	install.NewCall(signal, i32c(usr1), install.NewBitCast(handler, i8ptr))
	l.liveReply(install, "ready\n")
	install.NewBr(done)
	done.NewRet(nil)
	return init
}

// liveReply writes text to lyrac's end of the pipe.
func (l *lowerer) liveReply(block *ir.Block, text string) {
	block.NewCall(l.writeFunc(), i32c(liveWriteFD), l.cString(text), i64c(int64(len(text))))
}

// emitLivePatch emits a later generation's `lyra_live_patch`: a store of each of its
// functions `main` calls into generation 0's slot for it. The library carries its own
// patch list as code, so the loader has no table to read.
func (l *lowerer) emitLivePatch(callees []*ir.Func) {
	fn := l.module.NewFunc(LivePatchSymbol, lltypes.Void)
	b := fn.NewBlock("entry")
	for _, callee := range callees {
		slot := l.module.NewGlobal(liveSlotPrefix+callee.Name(), callee.Type())
		slot.Linkage = enum.LinkageExternal
		b.NewStore(callee, slot)
	}
	b.NewRet(nil)
}

// emitLiveGlobalsInit lowers the top-level initializers into a function of their own,
// which `main` calls where it would otherwise run them inline (lowerEntry).
func (l *lowerer) emitLiveGlobalsInit() (*ir.Func, error) {
	fn := l.module.NewFunc(liveGlobalsInit, lltypes.Void)
	l.beginFunction(lltypes.Void, nil, false, false)
	block, err := l.initGlobals(fn.NewBlock("entry"))
	if err != nil {
		return nil, err
	}
	if block.Term == nil {
		if err := l.emitReturn(fn.Blocks[0], block, nil); err != nil {
			return nil, err
		}
	}
	return fn, l.resolveExitReleases(fn)
}

// liveNumbered matches a symbol the backend numbers across the whole program, so that
// its number says where in the compile it was made rather than what it is.
var liveNumbered = regexp.MustCompile(`^(\.str\.|\.cstr\.|\.re\.(trans|nl|acc)\.|lyra_closure_|lyra_env_drop_|lyra_retain_|lyra\.seq\.)\d+`)

// liveSymbolRef matches a global's or function's name where IR text uses it.
var liveSymbolRef = regexp.MustCompile(`@("[^"]*"|[-a-zA-Z$._0-9]+)`)

// normalizeLive is the text of the function or global named name with each numbered
// symbol it refers to replaced by a hash of that symbol's own normalized definition: the
// same text for the same code, whichever order the rest of the program was lowered in.
func normalizeLive(m *ir.Module, name string) string {
	defs := map[string]func() string{}
	for _, f := range m.Funcs {
		defs[f.Name()] = f.LLString
	}
	for _, g := range m.Globals {
		defs[g.Name()] = g.LLString
	}
	memo := map[string]string{}
	var normalize func(name string) string
	normalize = func(name string) string {
		if done, ok := memo[name]; ok {
			return done
		}
		memo[name] = "@#cycle" // a lambda reaching itself
		def, ok := defs[name]
		if !ok {
			return "@" + name
		}
		text := liveSymbolRef.ReplaceAllStringFunc(def(), func(ref string) string {
			sym := strings.Trim(ref[1:], `"`)
			if sym == name {
				return "@#self"
			}
			if !liveNumbered.MatchString(sym) {
				return ref
			}
			sum := sha256.Sum256([]byte(normalize(sym)))
			return "@#" + hex.EncodeToString(sum[:8])
		})
		memo[name] = text
		return text
	}
	return normalize(name)
}

// SortedKeys lists a LiveInfo map's keys in order, for a report that reads the same each
// time.
func SortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
