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
// # Differences from PCRE / Go's regexp
//
//   - Leftmost-longest semantics (POSIX-style), not leftmost-first.
//   - No capture groups; (...) is always non-capturing.
//   - No lazy quantifiers (*?, +?, ...); use complement instead.
//   - No backreferences.
//   - Phase 1 of the implementation does not yet include lookarounds,
//     Unicode property classes, or word boundaries (\b).
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

// Regex is a compiled regular expression. Safe for concurrent IsMatch /
// FindAll calls: lazily computed DFA states are added under a single
// builder lock per call.
type Regex struct {
	pattern string
	opts    Options
	dfa     *dfa
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
	e, err := p.parse()
	if err != nil {
		return nil, err
	}
	return &Regex{
		pattern: pattern,
		opts:    opts,
		dfa:     newDFA(e, opts.MaxDFAStates),
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

		// After '\n' in multiline mode, beginning-of-line fires for the
		// next line. We skip BOL on the last byte to avoid double-firing
		// against EOT below, but it's harmless either way: transparency
		// keeps the state put if nothing consumes BOL.
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
	var out [][2]int

	pos := 0
	for pos <= n {
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

// scanFrom attempts to find the longest match starting exactly at startPos
// in input. Returns (matchEnd, true, nil) if a match exists, or
// (_, false, nil) if no match starts here. Errors propagate from the lazy
// DFA construction.
func (r *Regex) scanFrom(input []byte, startPos int) (int, bool, error) {
	n := len(input)

	// Initial boundary at startPos:
	//   - position 0           → BOT (which also satisfies ^)
	//   - after a '\n'          → BOL (satisfies ^)
	//   - otherwise             → no leading boundary
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

	// Consume bytes from startPos until the DFA is dead or input ends.
	for j := startPos; j < n; j++ {
		b := input[j]

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
