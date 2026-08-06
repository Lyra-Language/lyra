package checker

// Effect is one bit in a function's inferred effect set — the generalization of
// the old single is-pure/is-impure bool into a row of independently tracked
// effect kinds (FP/Imperative todo #5). The row is internal: the programmer
// never names a bit. The user-facing surface is a small ladder of *named
// bounds*, each a union over this row — `pure` (no effect but alloc, via the
// PurityEffects mask), `det` (deterministic, via DetEffects), and `noalloc`
// (via EffectAlloc).
//
// EffectMut, the two IO bits, EffectAlloc, and (via the ambient `Random.global()`
// / `wallClock()` builtins) EffectRand and EffectTime are all detected today. IO
// is split into two bits because the halves threaten determinism
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
	// EffectRand: draws from a non-deterministic random source (the ambient
	// `Random.global()`). Non-deterministic, so it breaks `pure` and `det`. A
	// *threaded* RNG value reached through a local binding (`rng.next()`) is
	// ordinary data, not this effect.
	EffectRand
	// EffectTime: reads wall-clock/system time (the ambient `wallClock()`).
	// Non-deterministic, so it breaks `pure` and `det`. A *threaded* tick passed
	// in as a parameter is ordinary data, not this effect.
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

// AllEffects is every tracked effect bit set — the maximally conservative
// assumption for a callee whose body we cannot inspect (an imported/external
// function that resolves to no local lambda, builtin, or type conversion). It
// includes EffectAlloc, unlike PurityEffects: since we can't verify the callee
// doesn't allocate, a `noalloc` caller must be flagged for calling it, just as a
// `pure`/`det` caller is flagged by the PurityEffects bits.
const AllEffects = PurityEffects | EffectAlloc

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
	// `panic` is EffectNone — allowed in `pure`, `det` and `noalloc` alike.
	//
	// It writes to stderr and exits, so tagging it EffectOutput looks right at first.
	// The reason it is not: *every integer operation in this language can already
	// panic*. `a + b` traps on overflow, `xs[i]` traps out of bounds, a non-exhaustive
	// match traps on fallthrough — all inside `pure` functions, all writing the same
	// message to the same fd and exiting with the same code. Making the explicit form
	// impure while the implicit ones are free would say the effect is the *writing*,
	// when what is actually being classified is divergence, and would leave `pure`
	// meaning "cannot panic on purpose" rather than "cannot panic".
	//
	// So the rule is: purity is about what a function *returns* and what it mutates,
	// not about whether it terminates. A `pure` function that panics has produced no
	// value and mutated nothing; it has no observable effect on the program because
	// there is no longer a program. (Koka, which tracks `exn` and `div` as effects in
	// their own right, takes the other road — worth revisiting if a catchable panic or
	// a total-function guarantee is ever wanted, since both need this bit.)
	"panic": EffectNone,
	// Input — world → program. The returned value depends on external state, so
	// it is non-deterministic: forbidden in both `pure` and `det`.
	"read": EffectInput,
	// `read_line` consumes a line of stdin. Input for the obvious reason (the value
	// comes from outside), but note it is also *destructive* — the line is gone, so
	// two identical calls do not return the same thing. That is what Input already
	// means for `det` purposes, so it needs no bit of its own; recorded because
	// "reads external state" undersells it.
	//
	// It allocates too: the returned string owns heap bytes (`lyra_read_line`
	// mallocs them), so it is not `noalloc`. That is not expressible here — this
	// table is consulted for the purity ladder, while allocation is charged by
	// *form* in allocContext (todo.md's `noalloc` section) — and is the reason the
	// EffectAlloc bit is not set: setting it here would be the only entry doing so
	// and would not reach the pass that decides `noalloc`. Left for whenever a
	// builtin's allocation is charged at all; today no builtin's is.
	"read_line": EffectInput,
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
	// Ambient nondeterminism sources — the entries that give `det` its teeth.
	// Only the *ambient* (globally-reachable, un-threaded) sources are tagged:
	// `Random.global()` reaches a process-wide RNG and `wallClock()` reads the
	// system clock, so their results depend on external state the caller never
	// passed in — non-deterministic, forbidden in both `pure` and `det`.
	//
	// The ambient-vs-threaded split lives in the *call shape*, not here: a
	// threaded RNG value's `rng.next()` or a passed-in `tick` parameter is
	// ordinary `mut`/`own` data reached through a local binding (base `rng`,
	// `tick`), never one of these ambient entry points — so it carries no
	// Rand/Time bit and a `det` function may use seeded randomness and sim-time.
	// That is exactly what lets `det` be reproducible-from-its-inputs without
	// banning randomness or time outright.
	"Random.global": EffectRand,
	"wallClock":     EffectTime,
}
