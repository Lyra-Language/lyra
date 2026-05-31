// Package regex implements Lyra's regular expression engine.
//
// The engine follows the resharp design (https://github.com/ieviev/resharp):
// it is automata-based and non-backtracking, runs in linear time, and adds
// first-class intersection (&), complement (~(...)), and wildcard (_) on
// top of standard regex syntax. Matches are leftmost-longest: branch order
// in an alternation is irrelevant, and at every starting position the
// longest viable match is preferred.
//
// # Quick start
//
//	re, err := regex.Compile(`[A-Za-z0-9]{8,}&_*[0-9]_*&_*[A-Z]_*`)
//	if err != nil { ... }
//	ok, _ := re.IsMatch([]byte("Hunter2024"))
//
// # Operators
//
//   - |       alternation (leftmost-longest, order-independent)
//   - &       intersection ("must satisfy both sides")
//   - ~(R)    complement   ("must NOT match R")
//   - _       any single byte (including '\n')
//   - .       any single byte except '\n' (use (?s) or _ to include '\n')
//
// # Anchors
//
// Multiline is on by default; '^' / '$' match at line boundaries; '\A' /
// '\z' match only at the start / end of the entire input. Disable multiline
// with (?-m) or Options.MultiLine=false.
//
// # Lookarounds
//
// All four PCRE-style lookaround assertions are supported:
//
//   - (?=R)  positive lookahead  — the position must be followed by R
//   - (?!R)  negative lookahead  — the position must NOT be followed by R
//   - (?<=R) positive lookbehind — the position must be preceded by R
//   - (?<!R) negative lookbehind — the position must NOT be preceded by R
//
// Lookarounds are compiled into the automaton via intersection and complement.
// Leading lookbehinds (at the very start of a pattern) use a pre-computed
// gate DFA that is run once across the whole input before scanning.
//
// # Differences from PCRE / Go's regexp
//
//   - Leftmost-longest semantics (POSIX-style), not leftmost-first.
//   - No capture groups; (...) is always non-capturing.
//   - No lazy quantifiers (*?, +?, ...); use complement instead.
//   - No backreferences.
package regex

import "fmt"

// Options controls compilation and matching behavior.
type Options struct {
	// CaseInsensitive enables global (?i) — ASCII letters in literals and
	// character classes match either case.
	CaseInsensitive bool

	// DotAll enables (?s) — '.' matches '\n'. (The wildcard '_' always
	// matches '\n'.)
	DotAll bool

	// MultiLine controls (?m) — '^' / '$' match at every '\n', not just at
	// the boundaries of the input. Default true (resharp default).
	MultiLine bool

	// IgnoreWhitespace enables (?x) — whitespace and '#'-comments inside
	// the pattern are ignored.
	IgnoreWhitespace bool

	// MaxDFAStates caps the lazily constructed DFA. Patterns combining
	// heavy intersection or complement can in principle blow up; we fail
	// loudly rather than build forever. Zero or negative selects the
	// default (65535, matching resharp).
	MaxDFAStates int
}

// DefaultOptions returns the recommended defaults (resharp-compatible).
func DefaultOptions() Options {
	return Options{
		MultiLine:    true,
		MaxDFAStates: 1 << 16,
	}
}

// lbGate is a compiled leading-lookbehind gate DFA.
// It is run once across the whole input before scanning so that scanFrom can
// quickly check whether the lookbehind is satisfied at each start position.
type lbGate struct {
	pos bool // true = positive (?<=R), false = negative (?<!R)
	dfa *dfa
}

// laCache holds compiled sub-DFAs used by lookaheadNullable to verify
// trailing lookahead assertions without modifying the main DFA.
type laCache struct {
	dfas map[string]*dfa
	opts Options
}

// Regex is a compiled regular expression. Safe for concurrent IsMatch /
// FindAll calls: lazily computed DFA states are added under a single
// builder lock per call.
type Regex struct {
	pattern string
	opts    Options
	dfa     *dfa
	lbGates []lbGate // leading lookbehind gate DFAs (often nil)
	laCache *laCache // sub-DFA cache for trailing lookahead checks
}

// Compile parses pattern with DefaultOptions and returns a Regex.
func Compile(pattern string) (*Regex, error) {
	return CompileWithOptions(pattern, DefaultOptions())
}

// MustCompile is like Compile but panics on error. Useful for package-level
// variables holding patterns known at build time.
func MustCompile(pattern string) *Regex {
	re, err := Compile(pattern)
	if err != nil {
		panic(err)
	}
	return re
}

// CompileWithOptions parses pattern with the given options.
func CompileWithOptions(pattern string, opts Options) (*Regex, error) {
	if opts.MaxDFAStates <= 0 {
		opts.MaxDFAStates = 1 << 16
	}
	p := newParser(pattern, opts)
	rawExpr, err := p.parse()
	if err != nil {
		return nil, err
	}

	// Peel any leading exLeadingLB markers off the top-level expression.
	// Each one becomes a gate DFA (_*·R) checked before each scan position.
	var gates []lbGate
	mainExpr := rawExpr
	for {
		c, ok := mainExpr.(exConcat)
		if !ok {
			break
		}
		lb, ok := c.l.(exLeadingLB)
		if !ok {
			break
		}
		// Gate DFA: _*·R  (positive = require acceptance; negative = require non-acceptance)
		gateExpr := mkConcat(exprAnyStr, lb.e)
		gates = append(gates, lbGate{pos: lb.pos, dfa: newDFA(gateExpr, opts.MaxDFAStates)})
		mainExpr = c.r
	}
	// Handle a bare exLeadingLB with nothing after it.
	if lb, ok := mainExpr.(exLeadingLB); ok {
		gateExpr := mkConcat(exprAnyStr, lb.e)
		gates = append(gates, lbGate{pos: lb.pos, dfa: newDFA(gateExpr, opts.MaxDFAStates)})
		mainExpr = exprEps
	}

	return &Regex{
		pattern: pattern,
		opts:    opts,
		dfa:     newDFA(mainExpr, opts.MaxDFAStates),
		lbGates: gates,
		laCache: &laCache{dfas: make(map[string]*dfa), opts: opts},
	}, nil
}

// Pattern returns the original pattern string this Regex was compiled from.
func (r *Regex) Pattern() string { return r.pattern }

// String returns a debug representation of the regex.
func (r *Regex) String() string { return fmt.Sprintf("regex(%q)", r.pattern) }

// IsMatch reports whether the entire input is in the language of the
// pattern. An error is returned only if the DFA state cap is exceeded.
func (r *Regex) IsMatch(input []byte) (bool, error) {
	state := r.dfa.initial
	var err error

	// Beginning of text (= start of first line).
	state, err = r.dfa.boundaryTrans(state, symBOT)
	if err != nil {
		return false, err
	}

	// For leading lookbehinds, check each gate at position 0 (nothing precedes).
	// Gate DFA for _*·R: at position 0, only BOT has fired.  Accepts iff R
	// is nullable (since no real bytes preceded the start).
	for i := range r.lbGates {
		gate := &r.lbGates[i]
		lbState := gate.dfa.initial
		lbState, err = gate.dfa.boundaryTrans(lbState, symBOT)
		if err != nil {
			return false, err
		}
		accepted := gate.dfa.accept(lbState)
		if gate.pos && !accepted {
			return false, nil // positive lookbehind requires preceding context
		}
		if !gate.pos && accepted {
			return false, nil // negative lookbehind requires absence of preceding context
		}
	}

	for i, b := range input {
		// Before '\n' in multiline mode, end-of-line fires.
		if b == '\n' && r.opts.MultiLine {
			state, err = r.dfa.boundaryTrans(state, symEOL)
			if err != nil {
				return false, err
			}
		}

		// Consume the byte.
		state, err = r.dfa.trans(state, int(b))
		if err != nil {
			return false, err
		}
		if state == deadID {
			return false, nil
		}

		if b == '\n' && r.opts.MultiLine && i < len(input)-1 {
			state, err = r.dfa.boundaryTrans(state, symBOL)
			if err != nil {
				return false, err
			}
		}
	}

	// End of text (= end of last line).
	if r.opts.MultiLine {
		state, err = r.dfa.boundaryTrans(state, symEOL)
		if err != nil {
			return false, err
		}
	}
	state, err = r.dfa.boundaryTrans(state, symEOT)
	if err != nil {
		return false, err
	}

	return r.dfa.accept(state), nil
}

// MatchString is a convenience wrapper around IsMatch.
func (r *Regex) MatchString(s string) (bool, error) { return r.IsMatch([]byte(s)) }

// FindAll returns all non-overlapping leftmost-longest matches of the
// pattern in input. Returns an empty slice when there are no matches; an
// error only if the DFA capacity is exceeded.
func (r *Regex) FindAll(input []byte) ([][]byte, error) {
	matches, err := r.FindAllIndex(input)
	if err != nil {
		return nil, err
	}
	out := make([][]byte, len(matches))
	for i, m := range matches {
		out[i] = input[m[0]:m[1]]
	}
	return out, nil
}

// FindAllIndex returns the [start, end) byte offsets of all non-overlapping
// leftmost-longest matches.
func (r *Regex) FindAllIndex(input []byte) ([][2]int, error) {
	n := len(input)

	// Pre-run all leading-lookbehind gate DFAs once across the full input.
	// gateStates[gi][k] is the gate DFA state after consuming input[0:k].
	gateStates := r.preRunGates(input)

	var out [][2]int
	pos := 0
	for pos <= n {
		// Skip positions where a leading-lookbehind gate is not satisfied.
		if !r.gateOK(gateStates, pos) {
			pos++
			continue
		}
		end, ok, err := r.scanFrom(input, pos)
		if err != nil {
			return nil, err
		}
		if !ok {
			pos++
			continue
		}
		out = append(out, [2]int{pos, end})
		if end > pos {
			pos = end
		} else {
			// empty match; advance to avoid an infinite loop
			pos++
		}
	}
	return out, nil
}

// preRunGates pre-computes the gate DFA state at every position in input.
// Returns nil when there are no gates (the common case).
func (r *Regex) preRunGates(input []byte) [][]int {
	if len(r.lbGates) == 0 {
		return nil
	}
	n := len(input)
	out := make([][]int, len(r.lbGates))
	for gi := range r.lbGates {
		gate := &r.lbGates[gi]
		states := make([]int, n+1)
		state := gate.dfa.initial
		var err error
		state, err = gate.dfa.boundaryTrans(state, symBOT)
		if err != nil {
			state = deadID
		}
		states[0] = state
		for i, b := range input {
			if b == '\n' && r.opts.MultiLine {
				state, err = gate.dfa.boundaryTrans(state, symEOL)
				if err != nil {
					state = deadID
				}
			}
			state, err = gate.dfa.trans(state, int(b))
			if err != nil {
				state = deadID
			}
			if b == '\n' && r.opts.MultiLine {
				state, err = gate.dfa.boundaryTrans(state, symBOL)
				if err != nil {
					state = deadID
				}
			}
			states[i+1] = state
		}
		out[gi] = states
	}
	return out
}

// gateOK reports whether all leading-lookbehind gates pass at position pos.
// gateStates is nil when there are no gates, in which case it always returns true.
func (r *Regex) gateOK(gateStates [][]int, pos int) bool {
	for gi := range r.lbGates {
		gate := &r.lbGates[gi]
		accepted := gate.dfa.accept(gateStates[gi][pos])
		if gate.pos && !accepted {
			return false // positive (?<=R): preceding context must end with R
		}
		if !gate.pos && accepted {
			return false // negative (?<!R): preceding context must NOT end with R
		}
	}
	return true
}

// scanFrom attempts to find the longest match starting exactly at startPos
// in input. Returns (matchEnd, true, nil) if a match exists, or
// (_, false, nil) if no match starts here. Errors propagate from the lazy
// DFA construction.
func (r *Regex) scanFrom(input []byte, startPos int) (int, bool, error) {
	n := len(input)

	// Initial boundary at startPos.
	state := r.dfa.initial
	var err error
	switch {
	case startPos == 0:
		state, err = r.dfa.boundaryTrans(state, symBOT)
	case startPos > 0 && input[startPos-1] == '\n' && r.opts.MultiLine:
		state, err = r.dfa.boundaryTrans(state, symBOL)
	}
	if err != nil {
		return 0, false, err
	}

	bestEnd := -1
	if r.dfa.accept(state) {
		bestEnd = startPos
	}
	// Check trailing lookaheads on the initial state (before any bytes consumed).
	if lookaheadNullable(r.dfa.states[state].e, input, startPos, r.laCache) {
		bestEnd = startPos
	}

	// Consume bytes from startPos until the DFA is dead or input ends.
	for j := startPos; j < n; j++ {
		b := input[j]

		// Check trailing lookaheads BEFORE consuming input[j].
		// A trailing lookahead fires at position j (zero-width: the match
		// endpoint is j, not j+1).
		if lookaheadNullable(r.dfa.states[state].e, input, j, r.laCache) {
			bestEnd = j
		}

		// EOL fires *before* '\n' so that '$' can match the position
		// preceding the newline.
		if b == '\n' && r.opts.MultiLine {
			state, err = r.dfa.boundaryTrans(state, symEOL)
			if err != nil {
				return 0, false, err
			}
			if r.dfa.accept(state) {
				bestEnd = j
			}
		}

		// Consume the byte.
		state, err = r.dfa.trans(state, int(b))
		if err != nil {
			return 0, false, err
		}
		if state == deadID {
			break
		}
		if r.dfa.accept(state) {
			bestEnd = j + 1
		}

		// BOL after '\n' (zero-width): may enable matches that begin at
		// start-of-line within the same scan.
		if b == '\n' && r.opts.MultiLine {
			state, err = r.dfa.boundaryTrans(state, symBOL)
			if err != nil {
				return 0, false, err
			}
			if r.dfa.accept(state) {
				bestEnd = j + 1
			}
		}
	}

	// End-of-input boundaries.
	if state != deadID {
		// Check trailing lookaheads at the end of input.
		if lookaheadNullable(r.dfa.states[state].e, input, n, r.laCache) {
			bestEnd = n
		}
		if r.opts.MultiLine {
			state, err = r.dfa.boundaryTrans(state, symEOL)
			if err != nil {
				return 0, false, err
			}
			if r.dfa.accept(state) {
				bestEnd = n
			}
		}
		state, err = r.dfa.boundaryTrans(state, symEOT)
		if err != nil {
			return 0, false, err
		}
		if r.dfa.accept(state) {
			bestEnd = n
		}
	}

	if bestEnd < 0 {
		return 0, false, nil
	}
	return bestEnd, true, nil
}

// ── lookahead helpers ────────────────────────────────────────────────────────

// lookaheadNullable reports whether expression e is "accepting" at position pos
// in input, taking trailing exLookAhead assertions into account.
// It is called before consuming input[pos] to detect zero-width match endpoints.
func lookaheadNullable(e expr, input []byte, pos int, cache *laCache) bool {
	switch v := e.(type) {
	case exLookAhead:
		return checkLAPass(v.pos, v.e, input, pos, cache)
	case exConcat:
		// The left side must be nullable for the right side to matter.
		if !nullable(v.l) {
			return false
		}
		return lookaheadNullable(v.r, input, pos, cache)
	case exUnion:
		for _, p := range v.parts {
			if lookaheadNullable(p, input, pos, cache) {
				return true
			}
		}
		return false
	case exInter:
		for _, p := range v.parts {
			if !lookaheadNullable(p, input, pos, cache) {
				return false
			}
		}
		return true
	case exCompl:
		return !lookaheadNullable(v.e, input, pos, cache)
	case exStar:
		return true
	default:
		return nullable(e)
	}
}

// checkLAPass checks whether input[startPos:] satisfies a lookahead for laExpr.
//
//   - pos=true  (positive (?=R)):  returns true iff input[startPos:] ∈ L(R·_*)
//   - pos=false (negative (?!R)):  returns true iff input[startPos:] ∉ L(R·_*)
func checkLAPass(pos bool, laExpr expr, input []byte, startPos int, cache *laCache) bool {
	// Fast path: R matches ε — the lookahead trivially passes at any position.
	if nullable(laExpr) {
		return pos
	}
	// At end of input with non-nullable R: no characters remain to match R.
	if startPos >= len(input) {
		return !pos
	}

	// Get or compile the DFA for laExpr·_* (accepts if a prefix of input[startPos:]
	// is in L(laExpr), i.e. the lookahead is satisfied).
	key := laExpr.key()
	laDFA, ok := cache.dfas[key]
	if !ok {
		laDFA = newDFA(mkConcat(laExpr, exprAnyStr), cache.opts.MaxDFAStates)
		cache.dfas[key] = laDFA
	}

	// Run the sub-DFA on input[startPos:] until it accepts or dies.
	st := laDFA.initial
	for i := startPos; i < len(input); i++ {
		if laDFA.accept(st) {
			return pos // found a matching prefix
		}
		var e2 error
		st, e2 = laDFA.trans(st, int(input[i]))
		if e2 != nil || st == deadID {
			return !pos // can't match
		}
	}
	if laDFA.accept(st) {
		return pos
	}
	return !pos
}
