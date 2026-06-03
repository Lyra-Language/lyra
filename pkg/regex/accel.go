package regex

import (
	"bytes"
	"math/bits"
)

// ─── Byte rarity scores ───────────────────────────────────────────────────────

// byteRarityScore[b] is an estimate of how rarely byte b appears in typical
// UTF-8 text and source code.  Higher = rarer = better as a SIMD scan pivot.
// Scale: 0 = extremely common, 255 = extremely rare.
var byteRarityScore [256]uint8

func init() {
	// Control characters (0x00–0x08, 0x0B–0x0C, 0x0E–0x1F, 0x7F): very rare.
	for b := 0; b <= 0x08; b++ {
		byteRarityScore[b] = 200
	}
	byteRarityScore[0x09] = 20  // HT '\t'
	byteRarityScore[0x0A] = 5   // LF '\n'
	byteRarityScore[0x0B] = 200 // VT
	byteRarityScore[0x0C] = 200 // FF
	byteRarityScore[0x0D] = 30  // CR '\r'
	for b := 0x0E; b <= 0x1F; b++ {
		byteRarityScore[b] = 200
	}

	byteRarityScore[' '] = 5 // space: very common
	byteRarityScore['!'] = 90
	byteRarityScore['"'] = 55
	byteRarityScore['#'] = 100
	byteRarityScore['$'] = 100
	byteRarityScore['%'] = 110
	byteRarityScore['&'] = 100
	byteRarityScore['\''] = 55
	byteRarityScore['('] = 60
	byteRarityScore[')'] = 60
	byteRarityScore['*'] = 80
	byteRarityScore['+'] = 75
	byteRarityScore[','] = 35
	byteRarityScore['-'] = 45
	byteRarityScore['.'] = 30
	byteRarityScore['/'] = 60
	for b := byte('0'); b <= '9'; b++ {
		byteRarityScore[b] = 50
	}
	byteRarityScore[':'] = 65
	byteRarityScore[';'] = 65
	byteRarityScore['<'] = 70
	byteRarityScore['='] = 60
	byteRarityScore['>'] = 70
	byteRarityScore['?'] = 85
	byteRarityScore['@'] = 120 // rare in most corpora

	// Uppercase letters: medium frequency.
	for b := byte('A'); b <= 'Z'; b++ {
		byteRarityScore[b] = 48
	}
	byteRarityScore['['] = 80
	byteRarityScore['\\'] = 80
	byteRarityScore[']'] = 80
	byteRarityScore['^'] = 110
	byteRarityScore['_'] = 45
	byteRarityScore['`'] = 90

	// Lowercase letters: common.
	for b := byte('a'); b <= 'z'; b++ {
		byteRarityScore[b] = 20
	}
	// Most frequent English letters get lower scores.
	byteRarityScore['e'] = 8
	byteRarityScore['t'] = 9
	byteRarityScore['a'] = 10
	byteRarityScore['o'] = 10
	byteRarityScore['n'] = 12
	byteRarityScore['i'] = 12
	byteRarityScore['s'] = 13
	byteRarityScore['r'] = 12
	byteRarityScore['h'] = 16
	byteRarityScore['l'] = 14

	byteRarityScore['{'] = 80
	byteRarityScore['|'] = 90
	byteRarityScore['}'] = 80
	byteRarityScore['~'] = 105
	byteRarityScore[0x7F] = 200 // DEL

	// 0x80–0xBF: UTF-8 continuation bytes (present in Unicode text).
	for b := 0x80; b <= 0xBF; b++ {
		byteRarityScore[b] = 100
	}
	// 0xC0–0xFF: UTF-8 lead bytes (less frequent than continuations).
	for b := 0xC0; b <= 0xFF; b++ {
		byteRarityScore[b] = 110
	}
}

// ─── byteSet helpers ──────────────────────────────────────────────────────────

// popcount returns the number of set bits in s.
func (s byteSet) popcount() int {
	return bits.OnesCount64(s[0]) + bits.OnesCount64(s[1]) +
		bits.OnesCount64(s[2]) + bits.OnesCount64(s[3])
}

// ─── First-byte set ───────────────────────────────────────────────────────────

// isZeroWidthAtom reports whether e is a zero-width assertion (anchor or
// lookaround) that matches without consuming any real bytes.
func isZeroWidthAtom(e expr) bool {
	switch e.(type) {
	case exBT, exET, exBL, exEL, exLookAhead, exLeadingLB, exEps:
		return true
	}
	return false
}

// firstByteSet returns a conservative (over-approximate) set of bytes that can
// appear as the first real byte consumed by any non-empty match of e.
// The result is always a superset of the true first bytes; it is safe to skip
// positions where none of these bytes occur.
func firstByteSet(e expr) byteSet {
	switch v := e.(type) {
	case exLit:
		return singletonByteSet(v.b)
	case exCls:
		return v.s
	case exAny:
		return fullByteSet()
	case exBT, exET, exBL, exEL, exLookAhead, exLeadingLB, exEps, exEmpty:
		// Zero-width or empty: the first real byte comes from what follows.
		return byteSet{}
	case exConcat:
		s := firstByteSet(v.l)
		// If l is zero-width or nullable, r's first bytes are also candidates.
		if nullable(v.l) || isZeroWidthAtom(v.l) {
			s = s.union(firstByteSet(v.r))
		}
		return s
	case exStar:
		return firstByteSet(v.e)
	case exUnion:
		var s byteSet
		for _, p := range v.parts {
			s = s.union(firstByteSet(p))
		}
		return s
	case exInter:
		// A byte can start an intersection only if it can start ALL parts.
		s := fullByteSet()
		for _, p := range v.parts {
			s = s.intersect(firstByteSet(p))
		}
		return s
	case exCompl:
		// Complement of a restricted set is hard to bound; be conservative.
		return fullByteSet()
	}
	return byteSet{}
}

// ─── Literal prefix extraction ────────────────────────────────────────────────

// litPrefix returns the longest guaranteed literal byte prefix of e.
// A "guaranteed prefix" is a byte sequence that every non-empty match of e
// must begin with.  Returns nil when no such prefix can be determined.
func litPrefix(e expr) []byte {
	switch v := e.(type) {
	case exLit:
		return []byte{v.b}
	case exConcat:
		lp := litPrefix(v.l)
		if len(lp) == 0 {
			return nil
		}
		if nullable(v.l) {
			// l can match empty; r's first bytes are also candidates, so lp
			// is only a partial prefix — return what we have.
			return lp
		}
		// l is always non-empty in a match; we can extend the prefix with r.
		rp := litPrefix(v.r)
		return append(lp, rp...)
	}
	return nil
}

// ─── Acceleration hints ───────────────────────────────────────────────────────

// accelHints holds pre-computed acceleration data for a compiled Regex.
// They guide FindAllIndex to skip large regions of input that provably cannot
// contain the start of a match, using SIMD-accelerated byte searches.
type accelHints struct {
	// prefix is a literal byte sequence (len ≥ 2) that all matches start with.
	// When non-nil, FindAll uses bytes.Index for bulk skipping.
	prefix []byte

	// pivots is the set of bytes (up to 4) that can appear as the first byte
	// of any match.  FindAll uses bytes.IndexByte for each pivot and advances
	// to the earliest candidate position.
	// Empty when the first-byte set is too large or the pattern is nullable.
	pivots []byte
}

// buildAccelHints derives acceleration hints from the compiled main expression.
// Nullable patterns (which can match ε at any position) disable all skipping.
func buildAccelHints(mainExpr expr) accelHints {
	// Nullable patterns can match the empty string everywhere; no skipping.
	if nullable(mainExpr) {
		return accelHints{}
	}

	// Try to extract a multi-byte literal prefix first.
	lp := litPrefix(mainExpr)
	if len(lp) >= 2 {
		return accelHints{prefix: lp, pivots: lp[:1]}
	}
	if len(lp) == 1 {
		return accelHints{pivots: lp}
	}

	// No literal prefix: use the first-byte set as pivot candidates.
	fbs := firstByteSet(mainExpr)
	if fbs.isEmpty() || fbs.isFull() {
		return accelHints{}
	}
	pc := fbs.popcount()
	if pc > 4 {
		// Too many distinct first bytes; SIMD scans for each wouldn't pay off.
		return accelHints{}
	}

	// Collect candidates, sorted by rarity (rarest first).
	type candidate struct {
		b     byte
		score uint8
	}
	var cands []candidate
	for b := 0; b < 256; b++ {
		if fbs.has(byte(b)) {
			cands = append(cands, candidate{byte(b), byteRarityScore[b]})
		}
	}
	// Sort by descending rarity score so the rarest pivot is tried first.
	for i := 1; i < len(cands); i++ {
		for j := i; j > 0 && cands[j].score > cands[j-1].score; j-- {
			cands[j], cands[j-1] = cands[j-1], cands[j]
		}
	}
	pivots := make([]byte, len(cands))
	for i, c := range cands {
		pivots[i] = c.b
	}
	return accelHints{pivots: pivots}
}

// skip advances pos to the earliest position in input[pos:n] that could
// plausibly start a match, using SIMD-accelerated byte searches.
// Returns n+1 when no candidate position exists (caller should break).
func (h accelHints) skip(input []byte, pos, n int) int {
	if pos >= n {
		return pos // at end-of-input; let caller handle the ε-match case
	}

	// Multi-byte literal prefix: one SIMD call covers the whole skip.
	if len(h.prefix) >= 2 {
		idx := bytes.Index(input[pos:], h.prefix)
		if idx < 0 {
			return n + 1
		}
		return pos + idx
	}

	// Single-byte or multi-pivot scan: take the earliest hit among pivots.
	if len(h.pivots) > 0 {
		best := -1
		for _, pv := range h.pivots {
			idx := bytes.IndexByte(input[pos:], pv)
			if idx >= 0 && (best < 0 || idx < best) {
				best = idx
			}
		}
		if best < 0 {
			return n + 1
		}
		return pos + best
	}

	return pos // no acceleration available; caller tries every position
}
