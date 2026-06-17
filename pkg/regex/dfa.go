package regex

import (
	"errors"
	"sync"
)

// errCapacityExceeded is returned when the lazy DFA would exceed
// Options.MaxDFAStates. Patterns combining heavy intersection / complement
// can in principle blow up to many states; we fail loudly rather than
// silently building forever, matching resharp's "fail-loud" philosophy.
var errCapacityExceeded = errors.New("regex: DFA state capacity exceeded")

// dfaState is one node of the lazy DFA. Each state is identified by an
// expression value (its "regex residual"); transitions out of it are computed
// on demand by taking derivatives and looked up by symbol id (0..numSyms-1).
//
// We use a map for trans rather than a 260-entry array because most states
// only use a small subset of symbols, especially boundaries.
type dfaState struct {
	e      expr
	accept bool
	trans  map[int]int // symbol → state id (lazily filled)
}

// deadID is the absorbing dead state — created at construction time.
const deadID = 0

// maxFrozenStates is the maximum number of DFA states for which we build a
// flat transition table.  A frozen DFA of N states uses N*256*2 bytes of
// memory (int16 per entry) and delivers O(1) byte-symbol lookups without any
// map / pointer overhead.
const maxFrozenStates = 512

// dfa is a lazily constructed DFA over the extended alphabet (bytes +
// boundary markers). States are added the first time a transition into them
// is requested.
//
// When frozen is true, all byte transitions (sym < 256) have been eagerly
// expanded and stored in flatTrans[stateID*256+sym].  Boundary-symbol
// transitions (sym ≥ 256) are always served by the map-based path.
type dfa struct {
	// mu guards the lazily-mutated fields (states, stateMap, and each
	// dfaState.trans map) so a compiled Regex is safe for concurrent matching,
	// as advertised. The frozen fast path (flatTrans, byte symbols) is lock-free:
	// frozen and flatTrans are written only by tryFreeze before the owning Regex
	// is published, and never mutated afterwards.
	mu       sync.Mutex
	states   []*dfaState
	stateMap map[string]int // expr.key() → state id
	initial  int
	maxCap   int

	// Frozen flat table: flatTrans[stateID*256 + sym] = nextStateID (int16).
	// Populated by tryFreeze; nil when the DFA is still lazy.
	flatTrans []int16
	frozen    bool
}

func newDFA(start expr, maxCap int) *dfa {
	if maxCap <= 0 {
		maxCap = 1 << 16
	}
	d := &dfa{
		states:   make([]*dfaState, 0, 64),
		stateMap: make(map[string]int, 64),
		maxCap:   maxCap,
	}
	// State 0 is the absorbing dead state. trans is empty; lookups will
	// dispatch through (*dfa).trans which always returns deadID for it.
	d.states = append(d.states, &dfaState{
		e:      exprEmpty,
		accept: false,
		trans:  map[int]int{},
	})
	d.stateMap[exprEmpty.key()] = deadID

	id, _ := d.intern(start)
	d.initial = id
	return d
}

// intern returns the state id for an expression, creating it on first sight.
// Returns (deadID, errCapacityExceeded) if maxCap would be exceeded.
//
// Callers must hold d.mu (or run during single-threaded construction, before
// the owning Regex is shared). It mutates d.states and d.stateMap.
func (d *dfa) intern(e expr) (int, error) {
	k := e.key()
	if id, ok := d.stateMap[k]; ok {
		return id, nil
	}
	if len(d.states) >= d.maxCap {
		return deadID, errCapacityExceeded
	}
	id := len(d.states)
	d.states = append(d.states, &dfaState{
		e:      e,
		accept: nullable(e),
		trans:  make(map[int]int, 4),
	})
	d.stateMap[k] = id
	return id, nil
}

// trans returns the next state id when reading sym from stateID. The dead
// state is absorbing.
func (d *dfa) trans(stateID, sym int) (int, error) {
	if stateID == deadID {
		return deadID, nil
	}
	// Fast path: frozen flat table for byte symbols. Lock-free: a frozen DFA's
	// flatTrans is immutable after construction.
	if d.frozen && sym < 256 {
		return int(d.flatTrans[stateID*256+sym]), nil
	}
	// Lazy path mutates shared maps/slices; serialize it.
	d.mu.Lock()
	defer d.mu.Unlock()
	s := d.states[stateID]
	if next, ok := s.trans[sym]; ok {
		return next, nil
	}
	next, err := d.intern(deriv(sym, s.e))
	if err != nil {
		return deadID, err
	}
	// d.states[stateID] is the same *dfaState even if d.states has been
	// re-sliced by intern, because the slice holds pointers.
	d.states[stateID].trans[sym] = next
	return next, nil
}

// tryFreeze attempts to eagerly build all reachable DFA states (via BFS over
// the full extended alphabet of 260 symbols) and compile byte transitions into
// a flat table.  If expansion would exceed maxFrozenStates the DFA stays lazy.
//
// After tryFreeze succeeds (frozen == true):
//   - Byte transitions (sym < 256) are served by the flat array.
//   - Boundary-symbol transitions (sym ≥ 256) were also expanded during BFS
//     and are cached in each dfaState.trans map; the lazy path finds them
//     instantly without allocating new states.
//
// maxFreezeWork is an upper bound on the number of derivative computations
// performed by tryFreeze.  It provides a hard time budget so that patterns
// whose DFA turns out to be large fail fast rather than blocking for seconds.
// At ~260 symbols per state, this allows roughly 31 states to be fully
// expanded before giving up.
const maxFreezeWork = 8192

func (d *dfa) tryFreeze() {
	// BFS over the FULL extended alphabet (bytes 0-255 + boundary symbols
	// 256-259).  We must expand boundary symbols too: they can create new
	// states (e.g., \A fires on symBOT and transitions to the real pattern).
	// After this loop every reachable state has ALL transitions cached, so
	// frozen = true guarantees no new states will ever be created.
	work := 0
	for id := 0; id < len(d.states); id++ {
		if len(d.states) > maxFrozenStates {
			return // too many states; stay lazy
		}
		for sym := 0; sym < numSyms; sym++ { // numSyms = 260 (bytes + boundaries)
			work++
			if work > maxFreezeWork {
				return // budget exhausted; stay lazy
			}
			if _, err := d.trans(id, sym); err != nil {
				return // capacity error; stay lazy
			}
		}
	}

	// All states and their byte transitions are now cached in the per-state
	// maps.  Build the flat array for O(1) indexed lookup.
	n := len(d.states)
	flat := make([]int16, n*256)
	for id, s := range d.states {
		base := id * 256
		for sym := 0; sym < 256; sym++ {
			next := deadID
			if v, ok := s.trans[sym]; ok {
				next = v
			}
			flat[base+sym] = int16(next)
		}
	}
	d.flatTrans = flat
	d.frozen = true
}

// boundaryTrans is like trans, but applies the transparency rule: if taking
// the boundary edge would land in the dead state, we stay where we are.
// This lets anchor-free patterns ignore injected boundary markers.
func (d *dfa) boundaryTrans(stateID, sym int) (int, error) {
	next, err := d.trans(stateID, sym)
	if err != nil {
		return deadID, err
	}
	if next == deadID {
		return stateID, nil
	}
	return next, nil
}

func (d *dfa) accept(stateID int) bool {
	// A frozen DFA's states slice never grows, so reading it is lock-free.
	// A lazy DFA may have d.states appended concurrently by trans, which
	// reallocates the backing array — guard the slice-header read.
	if d.frozen {
		return d.states[stateID].accept
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.states[stateID].accept
}

// exprAt returns the residual expression of a state. Like accept, it guards the
// states slice-header read for lazy DFAs that may be growing concurrently.
// A dfaState's .e field is immutable after creation, so only the slice access
// needs synchronization.
func (d *dfa) exprAt(stateID int) expr {
	if d.frozen {
		return d.states[stateID].e
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.states[stateID].e
}
