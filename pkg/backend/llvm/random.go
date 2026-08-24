package llvm

import (
	"fmt"

	"github.com/llir/llvm/ir"
	"github.com/llir/llvm/ir/constant"
	lltypes "github.com/llir/llvm/ir/types"
	"github.com/llir/llvm/ir/value"

	"github.com/Lyra-Language/lyra/pkg/ast"
)

// Randomness — the `random_seed()` builtin, `() -> u64`.
//
// **Only the seed is here.** The generator is ordinary Lyra in
// `std/prelude.lyra` (`Rng`, `next_u64`, `below`, `random_below`), because a PRNG
// is arithmetic and arithmetic is expressible in the language; asking the
// operating system for entropy is not. Same division of labour as `read_line`
// (primitive, so a builtin) beside `parse_i64` (expressible, so the prelude).
//
// It is also what makes `det` usable with randomness. A seeded generator is pure
// arithmetic over its own state, so `rng.below(100)` carries only EffectMut and
// stays legal in `det`; the Rand bit is charged exactly here, at the point of
// asking for a seed nobody supplied. Had the builtin been `random_below`, every
// draw would be non-deterministic and `det` code could not draw at all.

// ShimRandomSeed returns one word of entropy from the operating system.
const ShimRandomSeed = "lyra_random_seed"

// ensureRandomSeedRuntime emits `lyra_random_seed` into the module the first time
// it is needed, caching the handle on the lowerer (idempotent).
//
// **`getentropy`, with a `time` fallback.** getentropy(2) is the portable spelling
// available on both targets (macOS 10.12+, glibc 2.25+) and, unlike `getrandom`,
// is not Linux-only; unlike `arc4random_buf` it does not need a glibc as recent as
// 2.36. It needs no `FILE*`, so it avoids the platform-dependent `stdin` symbol
// problem that shaped `read_line`'s use of `getchar`.
//
// The slot is seeded with `time(NULL)` *before* the getentropy call rather than
// after a failure test. POSIX leaves the buffer's contents unspecified when
// getentropy fails, so a program that checked the return value and then read an
// untouched buffer would be seeding from an uninitialized stack word — reading
// uninitialized memory to decide a seed is exactly the kind of thing a sanitizer
// should flag and a language like this should not do. Writing the fallback first
// means the buffer always holds a defined value, and success simply overwrites it.
//
// The fallback is deliberately weak and that is fine: `time(NULL)` has one-second
// resolution, so two processes started in the same second would share a seed. It
// is reached only where the OS has no entropy source at all, and a predictable
// seed is a better failure than a crash for the uses this serves. It is not, and
// must not become, a security primitive — nothing here is suitable for keys.
func (l *lowerer) ensureRandomSeedRuntime() *ir.Func {
	if l.randomSeed != nil {
		return l.randomSeed
	}
	i8ptr := lltypes.NewPointer(lltypes.I8)

	// time(time_t*) -> time_t and getentropy(void*, size_t) -> int. `time_t` is
	// 64-bit on both supported targets.
	timeFn := l.module.NewFunc("time", lltypes.I64, ir.NewParam("", i8ptr))
	getentropy := l.module.NewFunc("getentropy", lltypes.I32,
		ir.NewParam("", i8ptr), ir.NewParam("", lltypes.I64))

	fn := l.module.NewFunc(ShimRandomSeed, lltypes.I64)
	b := fn.NewBlock("entry")
	slot := b.NewAlloca(lltypes.I64)
	b.NewStore(b.NewCall(timeFn, constant.NewNull(i8ptr)), slot)
	b.NewCall(getentropy, b.NewBitCast(slot, i8ptr), i64c(8))
	b.NewRet(b.NewLoad(lltypes.I64, slot))

	l.randomSeed = fn
	return fn
}

// lowerRandomSeedCall lowers `random_seed()` to a single call.
//
// The result is a `u64` — a plain scalar, owning nothing — so unlike `read_line`
// there is no ownership question here and nothing for the temp machinery to do.
func (l *lowerer) lowerRandomSeedCall(block *ir.Block, e *ast.FunctionCallExpr) (value.Value, *ir.Block, error) {
	if len(e.Arguments) != 0 {
		return nil, nil, fmt.Errorf("llvm: random_seed expects 0 arguments, got %d", len(e.Arguments))
	}
	return block.NewCall(l.ensureRandomSeedRuntime()), block, nil
}
