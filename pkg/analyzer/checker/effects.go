package checker

// Effect is one bit in a function's inferred effect set — the generalization
// of the old single is-pure/is-impure bool into a row of independently
// tracked effect kinds (FP/Imperative todo #5). `pure` corresponds to
// EffectNone, the empty set; CheckPurity's enforcement is unchanged by this
// (a `pure` function must still have no effects at all), but inferImpurity
// now records *which* effect(s) it found instead of collapsing them into one
// bool, so a caller (or a future relaxed annotation like "deterministic but
// allocates") can ask about a specific effect rather than just "pure or not".
//
// Only EffectMut and EffectIO are actually detected today, matching what
// inferImpurity/CheckPurity already looked for before this type existed.
// EffectAlloc, EffectRand, and EffectTime are reserved bits: declaring them
// now means a later pass can start setting them (e.g. flagging a heap
// allocation, a random-number builtin, or a wall-clock read) without
// changing this type, inferImpurity's signature, or any caller that already
// matches on the bits it cares about — only the detection logic and
// builtinEffects table need new cases.
type Effect uint8

const (
	// EffectMut: mutates state observable outside the call — reassigning or
	// interior-mutating a captured/borrowed binding, a pointer write, or
	// reading a captured mutable binding (non-deterministic between calls).
	EffectMut Effect = 1 << iota
	// EffectIO: observable I/O — a call to an effectful builtin (print,
	// read, write, …) or `await` (suspends on an external operation).
	EffectIO
	// EffectAlloc: heap allocation. Not yet detected by any pass — Lyra has
	// no settled notion yet of which constructs allocate (see todo #5); the
	// bit exists so that detection can be added later without a type change.
	EffectAlloc
	// EffectRand: draws from a non-deterministic random source. Not yet
	// detected — reserved for when a `rand`-style builtin exists.
	EffectRand
	// EffectTime: reads wall-clock/system time. Not yet detected — reserved
	// for when a time-reading builtin exists.
	EffectTime
)

// EffectNone is the empty effect set — purity, in row-of-effects terms.
const EffectNone Effect = 0

// PurityEffects is the subset of effects that violate `pure` — the
// *correctness* effects that break determinism / referential transparency.
// `pure` enforcement (CheckPurity) is defined against this mask, NOT the full
// row: a function is a purity violator iff it has any effect in PurityEffects.
//
// EffectAlloc is deliberately excluded — allocation is a *resource/budget*
// concern (does this fit a frame budget / avoid GC hitches?), orthogonal to
// purity: a `pure` function may allocate (e.g. build and return a list) and
// stay deterministic. EffectAlloc is still inferred and reported by
// InferredEffects, it just never makes a function impure.
//
// EffectRand and EffectTime are not detected yet, but when they are they
// belong here (both break determinism) — add them to this mask at that point.
const PurityEffects = EffectMut | EffectIO

// Has reports whether flag is set in e.
func (e Effect) Has(flag Effect) bool { return e&flag != 0 }

// IsPure reports whether e is the empty set — no observable effect at all.
func (e Effect) IsPure() bool { return e == EffectNone }

// builtinEffects maps a builtin's dotted call name to the effect(s) it
// performs. This is knownImpureBuiltins generalized: previously every entry
// here implied a single "impure" bool, now each is tagged with which kind of
// effect it actually is. All current entries are EffectIO; a future
// allocating or time/rand-reading builtin is added the same way, tagged with
// EffectAlloc/EffectRand/EffectTime instead.
var builtinEffects = map[string]Effect{
	"print":       EffectIO,
	"println":     EffectIO,
	"fmt.print":   EffectIO,
	"fmt.println": EffectIO,
	"read":        EffectIO,
	"write":       EffectIO,
}
