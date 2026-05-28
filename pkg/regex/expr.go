package regex

import (
	"fmt"
	"slices"
	"sort"
	"strings"
)

// expr is the internal regex AST. Every distinct expression value represents
// a distinct regular language. Smart constructors (mkXxx) maintain a canonical
// form so structurally equal languages share keys, which makes the lazy DFA's
// memoization effective.
type expr interface {
	// key returns a unique, canonical string identifier for the language of
	// this expression. Two expressions with equal keys denote the same
	// language (modulo our simplification rules); they may safely share a
	// DFA state.
	key() string
}

// ── atoms ───────────────────────────────────────────────────────────────────

type exEmpty struct{} // ∅: the empty language (matches nothing)
type exEps struct{}   // ε: language containing only the empty string
type exAny struct{}   // `_`: any single byte (including '\n'); not boundaries
type exLit struct{ b byte }
type exCls struct{ s byteSet }

// ── anchors (zero-width, position-dependent) ────────────────────────────────

type exBT struct{} // \A
type exET struct{} // \z
type exBL struct{} // ^
type exEL struct{} // $

// ── combinators ─────────────────────────────────────────────────────────────

type exConcat struct{ l, r expr }
type exStar struct{ e expr }
type exCompl struct{ e expr }
type exUnion struct{ parts []expr } // sorted by key, deduplicated, ∅-free
type exInter struct{ parts []expr } // sorted by key, deduplicated, ∅-free

// ── key encoding ────────────────────────────────────────────────────────────
//
// Encoding rules (each token is self-delimiting):
//   atoms:   "0" (∅) | "1" (ε) | "2" (_) | "3" (\A) | "4" (\z) | "5" (^) | "6" ($)
//            "L<2 hex>"        | "C<64 hex>"
//   compounds (bracketed):
//     concat: "N" l r "n"   ─ no separator: children are self-delimiting
//     star:   "S" e "s"
//     compl:  "K" e "k"
//     union:  "U" p1 "," p2 "," ... "u"
//     inter:  "W" p1 "," p2 "," ... "w"
//
// Single-character markers (0-6, L, C, N, S, K, U, W) never collide with hex
// payloads (which are 0-9 / A-F only), and lowercase closers (n, s, k, u, w)
// never start a sub-expression. Thus the grammar is unambiguous without any
// length prefix.

func (exEmpty) key() string { return "0" }
func (exEps) key() string   { return "1" }
func (exAny) key() string   { return "2" }
func (exBT) key() string    { return "3" }
func (exET) key() string    { return "4" }
func (exBL) key() string    { return "5" }
func (exEL) key() string    { return "6" }
func (e exLit) key() string { return fmt.Sprintf("L%02X", e.b) }
func (e exCls) key() string {
	return fmt.Sprintf("C%016X%016X%016X%016X", e.s[0], e.s[1], e.s[2], e.s[3])
}
func (e exConcat) key() string { return "N" + e.l.key() + e.r.key() + "n" }
func (e exStar) key() string   { return "S" + e.e.key() + "s" }
func (e exCompl) key() string  { return "K" + e.e.key() + "k" }

func (e exUnion) key() string {
	parts := make([]string, len(e.parts))
	for i, p := range e.parts {
		parts[i] = p.key()
	}
	return "U" + strings.Join(parts, ",") + "u"
}

func (e exInter) key() string {
	parts := make([]string, len(e.parts))
	for i, p := range e.parts {
		parts[i] = p.key()
	}
	return "W" + strings.Join(parts, ",") + "w"
}

// ── well-known singletons ───────────────────────────────────────────────────

var (
	exprEmpty  expr = exEmpty{}
	exprEps    expr = exEps{}
	exprAny    expr = exAny{}
	exprAnyStr expr = exStar{exAny{}} // _*  ─ the all-strings language

	exprBT expr = exBT{}
	exprET expr = exET{}
	exprBL expr = exBL{}
	exprEL expr = exEL{}
)

func isEmpty(e expr) bool { _, ok := e.(exEmpty); return ok }
func isEps(e expr) bool   { _, ok := e.(exEps); return ok }
func isAnyStr(e expr) bool {
	s, ok := e.(exStar)
	if !ok {
		return false
	}
	_, ok = s.e.(exAny)
	return ok
}

// containsAnchors reports whether any \A, \z, ^, or $ appears anywhere in e.
// Used by deriv to decide when boundary markers should be transparent through
// a complement node — anchor-free complements operate purely on byte strings
// and must not react to virtual boundary symbols.
func containsAnchors(e expr) bool {
	switch v := e.(type) {
	case exBT, exET, exBL, exEL:
		return true
	case exConcat:
		return containsAnchors(v.l) || containsAnchors(v.r)
	case exStar:
		return containsAnchors(v.e)
	case exCompl:
		return containsAnchors(v.e)
	case exUnion:
		return slices.ContainsFunc(v.parts, containsAnchors)
	case exInter:
		return slices.ContainsFunc(v.parts, containsAnchors)
	}
	return false
}

// ── smart constructors ──────────────────────────────────────────────────────

func mkLit(b byte) expr { return exLit{b} }

func mkCls(s byteSet) expr {
	if s.isEmpty() {
		return exprEmpty
	}
	if s.isFull() {
		return exprAny
	}
	return exCls{s}
}

// mkConcat builds a concatenation node, applying:
//
//	∅·R = R·∅ = ∅,  ε·R = R,  R·ε = R.
func mkConcat(l, r expr) expr {
	if isEmpty(l) || isEmpty(r) {
		return exprEmpty
	}
	if isEps(l) {
		return r
	}
	if isEps(r) {
		return l
	}
	return exConcat{l, r}
}

// mkStar builds a Kleene-star node, applying:
//
//	∅* = ε,  ε* = ε,  (R*)* = R*.
func mkStar(e expr) expr {
	if isEmpty(e) || isEps(e) {
		return exprEps
	}
	if _, ok := e.(exStar); ok {
		return e
	}
	return exStar{e}
}

// mkCompl builds a complement node, applying:
//
//	~~R = R,  ~∅ = _*,  ~(_*) = ∅.
func mkCompl(e expr) expr {
	if c, ok := e.(exCompl); ok {
		return c.e
	}
	if isEmpty(e) {
		return exprAnyStr
	}
	if isAnyStr(e) {
		return exprEmpty
	}
	return exCompl{e}
}

// mkUnion folds the given expressions into a single union, applying:
//
//	∅ ∪ R = R,  R ∪ R = R,  _* ∪ R = _*,  associativity, commutativity.
//
// Empty input -> ∅. Single-element input is returned as-is.
func mkUnion(parts ...expr) expr {
	var flat []expr
	for _, p := range parts {
		switch v := p.(type) {
		case exEmpty:
			// drop neutral element
		case exUnion:
			flat = append(flat, v.parts...)
		default:
			if isAnyStr(p) {
				return exprAnyStr
			}
			flat = append(flat, p)
		}
	}
	flat = dedupAndSort(flat)
	switch len(flat) {
	case 0:
		return exprEmpty
	case 1:
		return flat[0]
	default:
		return exUnion{flat}
	}
}

// mkInter folds the given expressions into a single intersection, applying:
//
//	∅ ∩ R = ∅,  R ∩ R = R,  _* ∩ R = R,  associativity, commutativity.
//
// Empty input -> _* (identity for intersection).
func mkInter(parts ...expr) expr {
	var flat []expr
	for _, p := range parts {
		switch v := p.(type) {
		case exEmpty:
			return exprEmpty // ∅ absorbs
		case exInter:
			flat = append(flat, v.parts...)
		default:
			if isAnyStr(p) {
				continue // _* is the identity for intersection
			}
			flat = append(flat, p)
		}
	}
	flat = dedupAndSort(flat)
	switch len(flat) {
	case 0:
		return exprAnyStr
	case 1:
		return flat[0]
	default:
		return exInter{flat}
	}
}

// mkPlus and mkOpt are syntactic sugar.
func mkPlus(e expr) expr { return mkConcat(e, mkStar(e)) }
func mkOpt(e expr) expr  { return mkUnion(e, exprEps) }

// mkRepeat builds e{min,max}.  max < 0 means unbounded.
func mkRepeat(e expr, min, max int) expr {
	if min < 0 {
		min = 0
	}
	// e{0,0} = ε
	if max == 0 {
		return exprEps
	}
	// Build the required prefix: e · e · ... · e (min times)
	out := exprEps
	for i := 0; i < min; i++ {
		out = mkConcat(out, e)
	}
	if max < 0 {
		// {min,} = e{min} · e*
		return mkConcat(out, mkStar(e))
	}
	// {min, max} = e{min} · e? · e? · ... (max-min times)
	opt := mkOpt(e)
	for i := min; i < max; i++ {
		out = mkConcat(out, opt)
	}
	return out
}

func dedupAndSort(parts []expr) []expr {
	if len(parts) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(parts))
	out := parts[:0]
	for _, p := range parts {
		k := p.key()
		if _, ok := seen[k]; ok {
			continue
		}
		seen[k] = struct{}{}
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].key() < out[j].key() })
	return out
}
