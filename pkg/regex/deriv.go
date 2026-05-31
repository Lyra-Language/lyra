package regex

// Symbol space for the DFA. Real bytes are 0..255. Above that we have four
// virtual "boundary markers" the matching engine injects at line/text
// boundaries so that anchors can be expressed as ordinary DFA transitions:
//
//	symBOT   beginning of text         (also satisfies ^ — start of line)
//	symEOT   end of text               (also satisfies $ — end of line)
//	symBOL   beginning of line         (after an interior '\n')
//	symEOL   end of line               (before an interior '\n')
//
// The matching engine treats boundary transitions as transparent: if taking
// the boundary edge would land in the dead state, we keep the original
// state. This is what makes anchor-free patterns "ignore" injected markers.
const (
	symBOT  = 256
	symEOT  = 257
	symBOL  = 258
	symEOL  = 259
	numSyms = 260
)

// nullable reports whether the empty string ε is in the language of e.
//
// Anchors are intentionally treated as NOT nullable here: matching an anchor
// requires a boundary symbol to fire (see deriv), so an anchor never accepts
// "in place". The DFA's accept predicate uses this nullable(); after any
// trailing boundary markers have been processed by the matching engine, the
// remaining expression encodes only what real input must satisfy.
func nullable(e expr) bool {
	switch v := e.(type) {
	case exEmpty:
		return false
	case exEps:
		return true
	case exLit, exCls, exAny:
		return false
	case exBT, exET, exBL, exEL:
		return false
	case exLookAhead:
		// exLookAhead is always treated as non-accepting by the standard DFA
		// accept check. Acceptance at the lookahead position is handled
		// exclusively by lookaheadNullable() in the scanning loop, which
		// calls checkLAPass() to verify the remaining input without consuming.
		_ = v
		return false
	case exLookBehind:
		return false // temporary parse node; should never reach the DFA
	case exLeadingLB:
		return false // marker node; peeled before DFA construction
	case exStar:
		return true
	case exCompl:
		return !nullable(v.e)
	case exConcat:
		return nullable(v.l) && nullable(v.r)
	case exUnion:
		for _, p := range v.parts {
			if nullable(p) {
				return true
			}
		}
		return false
	case exInter:
		for _, p := range v.parts {
			if !nullable(p) {
				return false
			}
		}
		return true
	}
	return false
}

// deriv computes the Brzozowski derivative D_sym(e) — the set of suffixes w
// such that sym·w is in L(e):
//
//	D_sym(∅)   = ∅
//	D_sym(ε)   = ∅
//	D_sym(b)   = ε  if sym = b, else ∅
//	D_sym([S]) = ε  if sym ∈ S, else ∅
//	D_sym(_)   = ε  if sym is a real byte, else ∅
//	D_sym(R·S) = D_sym(R)·S       if R is not nullable
//	           = D_sym(R)·S ∪ D_sym(S)   if R is nullable
//	D_sym(R*)  = D_sym(R)·R*
//	D_sym(R|S) = D_sym(R) ∪ D_sym(S)
//	D_sym(R&S) = D_sym(R) ∩ D_sym(S)
//	D_sym(~R)  = ~D_sym(R)
//
// Anchors fire only on the boundary symbol they correspond to. \A matches
// only symBOT; ^ matches symBOT or symBOL (because the start of text is
// also the start of a line); \z matches only symEOT; $ matches symEOT or
// symEOL.
func deriv(sym int, e expr) expr {
	switch v := e.(type) {
	case exEmpty:
		return exprEmpty
	case exEps:
		return exprEmpty

	case exLit:
		if sym < 256 && v.b == byte(sym) {
			return exprEps
		}
		return exprEmpty

	case exCls:
		if sym < 256 && v.s.has(byte(sym)) {
			return exprEps
		}
		return exprEmpty

	case exAny:
		// _ matches any real byte but never a boundary marker.
		if sym < 256 {
			return exprEps
		}
		return exprEmpty

	case exBT:
		if sym == symBOT {
			return exprEps
		}
		return exprEmpty
	case exET:
		if sym == symEOT {
			return exprEps
		}
		return exprEmpty
	case exBL:
		if sym == symBOT || sym == symBOL {
			return exprEps
		}
		return exprEmpty
	case exEL:
		if sym == symEOT || sym == symEOL {
			return exprEps
		}
		return exprEmpty

	case exStar:
		return mkConcat(deriv(sym, v.e), v)

	case exCompl:
		// Boundary-transparency for anchor-free complements.
		//
		// `~(R)` should mean "byte strings not in L(R)". When sym is a
		// boundary marker and R contains no anchors, R cannot progress on
		// sym (every byte-only sub-expression returns ∅ on a boundary). A
		// naïve `~D_sym(R) = ~∅ = _*` would then make the complement accept
		// everything as soon as we cross a boundary, which is wrong.
		//
		// Treating boundaries as transparent through anchor-free complements
		// preserves the byte-level language: the complement neither makes nor
		// loses progress on virtual markers.
		if sym >= 256 && !containsAnchors(v.e) {
			return e
		}
		return mkCompl(deriv(sym, v.e))

	case exConcat:
		dl := mkConcat(deriv(sym, v.l), v.r)
		if nullable(v.l) {
			return mkUnion(dl, deriv(sym, v.r))
		}
		return dl

	case exLookAhead:
		// Lookaheads are zero-width: they don't consume characters.
		// D_c((?=R)) = ∅  —  the node stays as a sentinel in the concat until
		// lookaheadNullable() fires it at scan time.
		return exprEmpty
	case exLookBehind:
		return exprEmpty // temporary parse node
	case exLeadingLB:
		return exprEmpty // marker node; peeled before DFA construction

	case exUnion:
		parts := make([]expr, len(v.parts))
		for i, p := range v.parts {
			parts[i] = deriv(sym, p)
		}
		return mkUnion(parts...)

	case exInter:
		parts := make([]expr, len(v.parts))
		for i, p := range v.parts {
			parts[i] = deriv(sym, p)
		}
		return mkInter(parts...)
	}
	return exprEmpty
}
