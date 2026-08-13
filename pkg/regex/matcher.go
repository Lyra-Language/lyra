package regex

import (
	"errors"
	"fmt"
)

// A **compiled match table**: the DFA flattened into plain arrays a code
// generator can emit as constants, so a pattern known at compile time can be
// matched at run time without a regex engine in the runtime (08/13).
//
// Lyra's runtime is hand-written shims and libc; it has no FFI and nothing that
// could compile a pattern. But a pattern never needs compiling at run time —
// `r"…"` is a *literal* and a `where pattern(…)` constraint is part of a type — so
// the engine runs here, at compile time, and only its answer ships. What ships is
// two arrays and a loop.
//
// **The table must agree with IsMatch exactly**, since the compile-time and runtime
// rungs of the same check must not disagree about one pattern (a value passing one
// and failing the other is worse than either). That is why the boundary handling
// below mirrors IsMatch's structure step for step rather than being re-derived, and
// why matcher_test.go checks the two against each other over a corpus of patterns
// and inputs rather than against hand-written expectations.

// ErrUncompilablePattern is returned for a pattern this cannot flatten. The caller
// is expected to report it and fall back to compile-time-only checking, rather than
// silently matching by some other rule.
var ErrUncompilablePattern = errors.New("regex: pattern cannot be compiled to a match table")

// Matcher is a pattern compiled to flat tables.
//
// Trans is the byte transition table, `Trans[state*256+b]`, with the `'\n'` column
// carrying the *mid-input* newline behavior (see NewlineLast for the other case).
// Dead states are represented by DeadState, and a run that reaches one can stop.
type Matcher struct {
	Pattern     string
	NumStates   int
	Start       int32   // the state after the beginning-of-text boundary
	Trans       []int32 // NumStates*256, byte transitions
	NewlineLast []int32 // NumStates, the transition for a '\n' that is the final byte
	// AcceptFinal[s] reports whether s accepts once the end-of-text boundaries have
	// been applied — so the run's last act is one array read rather than more
	// boundary logic.
	AcceptFinal []bool
}

// DeadState is the absorbing state; it is state 0 in the underlying DFA, and a
// transition into it can never lead to a match.
const DeadState = int32(deadID)

// MaxTableStates caps a compiled pattern's DFA for a *code generator*: the
// transition table is states×256 entries, so 256 states is a 256 KB constant —
// already generous for a constraint pattern. It lives here so the compiler's two
// users of it cannot disagree: the typechecker asks whether a pattern will compile
// (and reports lyra-E054 if not), and the backend then compiles it. A different cap
// on each side would mean accepting a pattern the backend could not emit.
const MaxTableStates = 256

// CompileMatcher compiles pattern and flattens it into tables.
//
// maxStates caps the table's size: the transition table is NumStates*256 entries,
// so a pattern with a large DFA would emit a large constant into the program. A
// pattern exceeding it is refused rather than silently truncated.
func CompileMatcher(pattern string, maxStates int) (*Matcher, error) {
	re, err := Compile(pattern)
	if err != nil {
		return nil, err
	}
	return re.Matcher(maxStates)
}

// Matcher flattens an already-compiled Regex.
//
// Two shapes are refused rather than approximated:
//
//   - **A leading lookbehind.** IsMatch runs a separate gate DFA per lookbehind
//     before scanning, and whether the gate accepts depends on context *preceding*
//     the input — which a flat byte table has no way to represent. There are none
//     in an ordinary constraint pattern.
//   - **A DFA larger than maxStates**, which would emit a table larger than the
//     program that uses it.
//
// Both are `ErrUncompilablePattern`, so the caller reports one thing.
func (r *Regex) Matcher(maxStates int) (*Matcher, error) {
	if len(r.lbGates) > 0 {
		return nil, fmt.Errorf("%w: %s uses a lookbehind, whose gate depends on text before the input",
			ErrUncompilablePattern, r.pattern)
	}

	m := &Matcher{Pattern: r.pattern}

	// The beginning-of-text boundary fires once, before any byte, exactly as it does
	// in IsMatch — so the emitted loop starts from the state *after* it and never
	// has to know the boundary exists.
	start, err := r.dfa.boundaryTrans(r.dfa.initial, symBOT)
	if err != nil {
		return nil, err
	}

	// Reachability closure. The DFA is lazy — a state's transitions exist only once
	// asked for — so every state reachable from the start must be visited and every
	// byte asked, which is what turns "compute on demand" into a table.
	seen := map[int]bool{start: true, deadID: true}
	order := []int{deadID, start}
	if start == deadID {
		order = []int{deadID}
	}
	for i := 0; i < len(order); i++ {
		s := order[i]
		if len(order) > maxStates {
			return nil, fmt.Errorf("%w: %s needs more than %d states",
				ErrUncompilablePattern, r.pattern, maxStates)
		}
		next, err := r.successors(s)
		if err != nil {
			return nil, err
		}
		for _, t := range next {
			if !seen[t] {
				seen[t] = true
				order = append(order, t)
			}
		}
	}
	if len(order) > maxStates {
		return nil, fmt.Errorf("%w: %s needs more than %d states",
			ErrUncompilablePattern, r.pattern, maxStates)
	}

	// Renumber to a dense 0..n-1 so the emitted table has no gaps, keeping dead at 0
	// so DeadState is a constant the generated code can compare against.
	id := make(map[int]int32, len(order))
	for i, s := range order {
		id[s] = int32(i)
	}
	m.NumStates = len(order)
	m.Start = id[start]
	m.Trans = make([]int32, m.NumStates*256)
	m.NewlineLast = make([]int32, m.NumStates)
	m.AcceptFinal = make([]bool, m.NumStates)

	for _, s := range order {
		row := int(id[s]) * 256
		for b := 0; b < 256; b++ {
			t, err := r.stepByte(s, byte(b), false)
			if err != nil {
				return nil, err
			}
			m.Trans[row+b] = id[t]
		}
		last, err := r.stepByte(s, '\n', true)
		if err != nil {
			return nil, err
		}
		m.NewlineLast[id[s]] = id[last]

		acc, err := r.acceptsAtEnd(s)
		if err != nil {
			return nil, err
		}
		m.AcceptFinal[id[s]] = acc
	}
	return m, nil
}

// stepByte is one byte of IsMatch's loop, boundaries included: in multiline mode a
// '\n' fires end-of-line *before* it is consumed and beginning-of-line after —
// except when it is the input's final byte, where IsMatch deliberately omits the
// trailing beginning-of-line (`i < len(input)-1`).
//
// Mirroring that structure rather than reasoning about it fresh is deliberate: the
// two must agree, and the cheapest way to guarantee agreement is to be the same
// sequence of calls.
func (r *Regex) stepByte(state int, b byte, isLastByte bool) (int, error) {
	var err error
	if b == '\n' && r.opts.MultiLine {
		if state, err = r.dfa.boundaryTrans(state, symEOL); err != nil {
			return 0, err
		}
	}
	if state, err = r.dfa.trans(state, int(b)); err != nil {
		return 0, err
	}
	if state == deadID {
		return deadID, nil
	}
	if b == '\n' && r.opts.MultiLine && !isLastByte {
		if state, err = r.dfa.boundaryTrans(state, symBOL); err != nil {
			return 0, err
		}
	}
	return state, nil
}

// acceptsAtEnd applies the end-of-input boundaries and reports whether the state
// accepts — the tail of IsMatch, precomputed so the emitted loop ends in a lookup.
func (r *Regex) acceptsAtEnd(state int) (bool, error) {
	var err error
	if r.opts.MultiLine {
		if state, err = r.dfa.boundaryTrans(state, symEOL); err != nil {
			return false, err
		}
	}
	if state, err = r.dfa.boundaryTrans(state, symEOT); err != nil {
		return false, err
	}
	return r.dfa.accept(state), nil
}

// successors lists every state reachable from s in one step, over all bytes and the
// final-byte newline — the edges the closure above must follow.
func (r *Regex) successors(s int) ([]int, error) {
	out := make([]int, 0, 8)
	seen := map[int]bool{}
	add := func(t int) {
		if !seen[t] {
			seen[t] = true
			out = append(out, t)
		}
	}
	for b := 0; b < 256; b++ {
		t, err := r.stepByte(s, byte(b), false)
		if err != nil {
			return nil, err
		}
		add(t)
	}
	t, err := r.stepByte(s, '\n', true)
	if err != nil {
		return nil, err
	}
	add(t)
	return out, nil
}

// Match runs the flattened tables over input. It exists so the table can be tested
// directly against IsMatch, and it is the exact algorithm the backend emits — if
// this is right, the generated loop is right.
func (m *Matcher) Match(input []byte) bool {
	state := m.Start
	for i, b := range input {
		if b == '\n' && i == len(input)-1 {
			state = m.NewlineLast[state]
		} else {
			state = m.Trans[int(state)*256+int(b)]
		}
		if state == DeadState {
			return false
		}
	}
	return m.AcceptFinal[state]
}
