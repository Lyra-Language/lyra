package regex

import "errors"

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

// dfa is a lazily constructed DFA over the extended alphabet (bytes +
// boundary markers). States are added the first time a transition into them
// is requested.
type dfa struct {
	states   []*dfaState
	stateMap map[string]int // expr.key() → state id
	initial  int
	maxCap   int
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
	return d.states[stateID].accept
}
