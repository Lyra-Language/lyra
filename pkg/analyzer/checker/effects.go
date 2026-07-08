package checker

// Effect is one bit in a function's inferred effect set — the generalization of
// the old single is-pure/is-impure bool into a row of independently tracked
// effect kinds (FP/Imperative todo #5). The row is internal: the programmer
// never names a bit. The user-facing surface is a small ladder of *named
// bounds*, each a union over this row — `pure` (no effect but alloc, via the
// PurityEffects mask), `det` (deterministic, via DetEffects), and `noalloc`
// (via EffectAlloc).
//
// EffectMut, the two IO bits, and EffectAlloc are detected today; EffectRand and
// EffectTime are reserved (declared so a later pass can set them without a type
// change). IO is split into two bits because the halves threaten determinism
// asymmetrically — that asymmetry is exactly what lets `det` be more permissive
// than `pure`:
//
//   - EffectInput (world → program) is non-deterministic: the value read depends
//     on external state, so it breaks `det` (and `pure`).
//   - EffectOutput (program → world, void result) is observable but
//     determinism-safe: printing doesn't feed external state back into the
//     computation, so a `det` function may log/emit telemetry. `pure` still
//     forbids it (memoizing/reordering a pure call must not drop or reorder
//     output).
type Effect uint8

const (
	// EffectMut: mutates state observable outside the call — reassigning or
	// interior-mutating a captured/borrowed binding, a pointer write, or
	// reading a captured mutable binding (non-deterministic between calls).
	EffectMut Effect = 1 << iota
	// EffectInput: reads external state — an effectful builtin whose result
	// depends on the outside world (read, recv, a future clock builtin) or
	// `await` (resumes with an external result). The value is non-deterministic,
	// so this breaks both `pure` and `det`.
	EffectInput
	// EffectOutput: writes to the outside world with a void result (print, log,
	// telemetry). Observable — so `pure` forbids it — but determinism-safe, so
	// `det` permits it: emitting output doesn't make the computation depend on
	// external state.
	EffectOutput
	// EffectAlloc: heap allocation — constructing a `shared`-declared value
	// outside a `with`-arena block. A resource/budget effect, orthogonal to
	// determinism: forbidden only by `noalloc`, never by `pure` or `det`.
	EffectAlloc
	// EffectRand: draws from a non-deterministic random source. Not yet detected
	// — reserved for when a `rand`-style ambient builtin exists.
	EffectRand
	// EffectTime: reads wall-clock/system time. Not yet detected — reserved for
	// when a time-reading ambient builtin exists.
	EffectTime
)

// EffectNone is the empty effect set — purity, in row-of-effects terms.
const EffectNone Effect = 0

// EffectIO is the union of the two IO halves — "performs any I/O". A convenience
// mask for callers that don't care about the input/output distinction;
// detection always sets the specific bit (EffectInput or EffectOutput).
const EffectIO = EffectInput | EffectOutput

// PurityEffects is the subset of effects that violate `pure` — every effect
// except EffectAlloc. `pure` is the strongest bound: no observable effect at all
// (mutation, input, output, randomness, time), so a pure call is
// memoizable/reorderable/parallelizable. EffectAlloc is deliberately excluded —
// allocation is a resource/budget concern, orthogonal to purity: a `pure`
// function may allocate (e.g. build and return a list) and stay deterministic.
const PurityEffects = EffectMut | EffectInput | EffectOutput | EffectRand | EffectTime

// DetEffects is the subset of effects that violate `det` (deterministic) — the
// non-determinism sources only. `det` is coarser than `pure`: it permits
// mutation (EffectMut), allocation (EffectAlloc), and output (EffectOutput), so
// a `det` sim step can mutate components, allocate, and log — but it forbids
// reading external input, randomness, and the clock, so it stays reproducible
// from its inputs. DetEffects ⊆ PurityEffects, so `pure` ⟹ `det` (every pure
// function is deterministic).
const DetEffects = EffectInput | EffectRand | EffectTime

// Has reports whether flag is set in e.
func (e Effect) Has(flag Effect) bool { return e&flag != 0 }

// IsPure reports whether e is the empty set — no observable effect at all.
func (e Effect) IsPure() bool { return e == EffectNone }

// builtinEffects maps a builtin's dotted call name to the effect(s) it performs.
// Each entry's classification is what drives the named bounds: an Output-only
// builtin is allowed in `det` (but not `pure`), an Input builtin is forbidden in
// both, an alloc-only builtin (none today) would be forbidden only in `noalloc`.
var builtinEffects = map[string]Effect{
	// Output — program → world, void result. Determinism-safe (allowed in `det`),
	// but still forbidden in `pure` (observable).
	"print":       EffectOutput,
	"println":     EffectOutput,
	"fmt.print":   EffectOutput,
	"fmt.println": EffectOutput,
	// Input — world → program. The returned value depends on external state, so
	// it is non-deterministic: forbidden in both `pure` and `det`.
	"read": EffectInput,
	// A write returns a status (bytes written / error) the program can branch on,
	// so external state can leak back into the computation — tagged Input
	// (non-deterministic, forbidden in `det`) as well as Output.
	"write": EffectInput | EffectOutput,
	// Arena helpers: pure (EffectNone). An arena is the *solution* to heap
	// allocation tracking — its creation is a one-time bounded setup, not a
	// per-frame GC-visible alloc; constructions built *inside* a `with`-arena
	// block that would ordinarily be EffectAlloc are discharged by the block.
	// Size helpers (megabytes, kilobytes, bytes) are pure arithmetic.
	"Arena.new":   EffectNone,
	"Arena.alloc": EffectNone,
	"megabytes":   EffectNone,
	"kilobytes":   EffectNone,
	"bytes":       EffectNone,
}
