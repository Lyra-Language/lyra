package regex

import (
	"fmt"
	"sync"
	"unicode"
	"unicode/utf8"
)

// ─── Unicode property resolution ─────────────────────────────────────────────

var (
	unicodeExprCacheMu sync.Mutex
	unicodeExprCache   = make(map[string]expr)
)

// unicodePropertyToExpr returns a byte-level expr that matches the UTF-8
// encoding of any code point belonging to the named Unicode property,
// general category, or script. Results are cached after the first call.
//
// Supported names:
//   - General category abbreviations: L, Lu, Ll, Lt, Lm, Lo, M, Mn, Mc,
//     Me, N, Nd, Nl, No, P, Pc, Pd, Ps, Pe, Pi, Pf, Po, S, Sc, Sk, Sm,
//     So, Z, Zs, Zl, Zp, C, Cc, Cf, Co, Cs
//   - Long aliases: Letter, Uppercase_Letter, Lowercase_Letter, Mark,
//     Number, Decimal_Number, Digit, Punctuation, Punct, Symbol,
//     Separator, Other, ASCII
//   - Script names: Latin, Greek, Han, Arabic, Cyrillic, … (all scripts
//     listed in Go's unicode.Scripts map)
//   - Binary properties: White_Space, Hex_Digit, … (unicode.Properties)
func unicodePropertyToExpr(name string) (expr, error) {
	unicodeExprCacheMu.Lock()
	if e, ok := unicodeExprCache[name]; ok {
		unicodeExprCacheMu.Unlock()
		return e, nil
	}
	unicodeExprCacheMu.Unlock()

	tab, err := lookupUnicodeTable(name)
	if err != nil {
		return nil, err
	}
	e := unicodeTableToExpr(tab)

	unicodeExprCacheMu.Lock()
	unicodeExprCache[name] = e
	unicodeExprCacheMu.Unlock()
	return e, nil
}

// lookupUnicodeTable maps a property name to its *unicode.RangeTable.
// Lookup order: general categories → script names → binary properties →
// long-name aliases.
func lookupUnicodeTable(name string) (*unicode.RangeTable, error) {
	// General category abbreviations (L, Lu, Nd, Z, C, …)
	if tab, ok := unicode.Categories[name]; ok {
		return tab, nil
	}
	// Script names (Latin, Greek, Han, Arabic, …)
	if tab, ok := unicode.Scripts[name]; ok {
		return tab, nil
	}
	// Binary properties (White_Space, Hex_Digit, …)
	if tab, ok := unicode.Properties[name]; ok {
		return tab, nil
	}
	// Long-name aliases recognised by PCRE / Unicode standard.
	switch name {
	case "Letter":
		return unicode.Letter, nil
	case "Uppercase_Letter":
		return unicode.Upper, nil
	case "Lowercase_Letter":
		return unicode.Lower, nil
	case "Titlecase_Letter":
		return unicode.Title, nil
	case "Mark":
		return unicode.Mark, nil
	case "Number":
		return unicode.Number, nil
	case "Decimal_Number", "Digit":
		return unicode.Digit, nil
	case "Punctuation", "Punct":
		return unicode.Punct, nil
	case "Symbol":
		return unicode.Symbol, nil
	case "Separator":
		return unicode.Z, nil
	case "Other":
		return unicode.Other, nil
	case "ASCII":
		return asciiRangeTable, nil
	}
	return nil, fmt.Errorf(
		"unknown Unicode property %q — supported: category abbreviations "+
			"(L, Nd, Z, …), long names (Letter, Number, …), "+
			"script names (Latin, Han, …), binary properties (White_Space, …)",
		name)
}

// asciiRangeTable is a synthetic table covering U+0000–U+007F.
var asciiRangeTable = &unicode.RangeTable{
	R16: []unicode.Range16{{Lo: 0x00, Hi: 0x7F, Stride: 1}},
}

// ─── RangeTable → byte-level expr ────────────────────────────────────────────

// unicodeTableToExpr converts every code-point range in tab to a byte-level
// expr and unions them together.
func unicodeTableToExpr(tab *unicode.RangeTable) expr {
	var parts []expr
	for _, r := range tab.R16 {
		lo, hi, s := rune(r.Lo), rune(r.Hi), rune(r.Stride)
		if s == 1 {
			parts = append(parts, runeRangeToExpr(lo, hi))
		} else {
			for cp := lo; cp <= hi; cp += s {
				parts = append(parts, runeToExpr(cp))
			}
		}
	}
	for _, r := range tab.R32 {
		lo, hi, s := rune(r.Lo), rune(r.Hi), rune(r.Stride)
		if s == 1 {
			parts = append(parts, runeRangeToExpr(lo, hi))
		} else {
			for cp := lo; cp <= hi; cp += s {
				parts = append(parts, runeToExpr(cp))
			}
		}
	}
	return mkUnion(parts...)
}

// runeToExpr returns an expr matching the UTF-8 byte sequence of r.
func runeToExpr(r rune) expr {
	var buf [utf8.UTFMax]byte
	n := utf8.EncodeRune(buf[:], r)
	e := exprEps
	for i := 0; i < n; i++ {
		e = mkConcat(e, mkLit(buf[i]))
	}
	return e
}

// runeRangeToExpr returns an expr matching the UTF-8 encoding of any rune
// in [lo, hi]. It partitions the range by encoding length so each sub-range
// has a uniform byte width, then calls runeRangeFixedWidthToExpr.
func runeRangeToExpr(lo, hi rune) expr {
	if lo > hi {
		return exprEmpty
	}
	// Length bands: [0x0000,0x007F] 1-byte, [0x0080,0x07FF] 2-byte,
	//               [0x0800,0xFFFF] 3-byte, [0x10000,MaxRune] 4-byte.
	bands := [4][2]rune{
		{0x0000, 0x007F},
		{0x0080, 0x07FF},
		{0x0800, 0xFFFF},
		{0x10000, utf8.MaxRune},
	}
	var parts []expr
	for _, b := range bands {
		bLo, bHi := b[0], b[1]
		if hi < bLo || lo > bHi {
			continue
		}
		subLo, subHi := lo, hi
		if subLo < bLo {
			subLo = bLo
		}
		if subHi > bHi {
			subHi = bHi
		}
		parts = append(parts, runeRangeFixedWidthToExpr(subLo, subHi))
	}
	return mkUnion(parts...)
}

// runeRangeFixedWidthToExpr builds an expr for a rune range where both
// endpoints share the same UTF-8 encoding length.
func runeRangeFixedWidthToExpr(lo, hi rune) expr {
	if lo > hi {
		return exprEmpty
	}
	var loBuf, hiBuf [utf8.UTFMax]byte
	loN := utf8.EncodeRune(loBuf[:], lo)
	hiN := utf8.EncodeRune(hiBuf[:], hi)
	if loN != hiN {
		// Shouldn't happen when called from runeRangeToExpr; degrade gracefully.
		var parts []expr
		for cp := lo; cp <= hi; cp++ {
			parts = append(parts, runeToExpr(cp))
		}
		return mkUnion(parts...)
	}
	return byteSeqRangeToExpr(loBuf[:loN], hiBuf[:hiN])
}

// byteSeqRangeToExpr builds an expr for the lexicographic range [lo, hi]
// over equal-length byte sequences.
//
// At each byte position it either fixes a common lead byte (when lo[0]==hi[0])
// and recurses, or emits up to three sub-ranges:
//
//	first  = lo[0]         ·  [lo[1:]   …  maxTail]
//	middle = [lo[0]+1 … hi[0]-1] · [0x80…0xBF]^(n-1)   (if the gap ≥ 2)
//	last   = hi[0]         ·  [minTail  …  hi[1:]]
//
// This is the standard UTF-8 range-to-automaton algorithm.
func byteSeqRangeToExpr(lo, hi []byte) expr {
	n := len(lo)
	if n == 0 {
		return exprEps
	}
	if n == 1 {
		var s byteSet
		s.addRange(lo[0], hi[0])
		return mkCls(s)
	}
	if lo[0] == hi[0] {
		return mkConcat(mkLit(lo[0]), byteSeqRangeToExpr(lo[1:], hi[1:]))
	}

	// Build continuation-byte bounds.
	maxTail := make([]byte, n-1)
	for i := range maxTail {
		maxTail[i] = 0xBF
	}
	minTail := make([]byte, n-1)
	for i := range minTail {
		minTail[i] = 0x80
	}

	var parts []expr
	// First partial range.
	parts = append(parts, mkConcat(mkLit(lo[0]), byteSeqRangeToExpr(lo[1:], maxTail)))
	// Middle range (when lead-byte gap >= 2).
	if int(lo[0])+1 <= int(hi[0])-1 {
		var mid byteSet
		mid.addRange(lo[0]+1, hi[0]-1)
		parts = append(parts, mkConcat(mkCls(mid), fullContExpr(n-1)))
	}
	// Last partial range.
	parts = append(parts, mkConcat(mkLit(hi[0]), byteSeqRangeToExpr(minTail, hi[1:])))
	return mkUnion(parts...)
}

// fullContExpr returns an expr matching n continuation bytes ([0x80, 0xBF] each).
func fullContExpr(n int) expr {
	var s byteSet
	s.addRange(0x80, 0xBF)
	e := exprEps
	for range n {
		e = mkConcat(e, mkCls(s))
	}
	return e
}

// ─── "Any valid UTF-8 code point" expression ─────────────────────────────────

var (
	unicodeAnyCPOnce sync.Once
	unicodeAnyCPExpr expr
)

// unicodeAnyCodepointExpr returns an expr matching exactly one valid UTF-8
// encoded code point (U+0000..U+10FFFF, excluding surrogates U+D800..U+DFFF).
//
// This is used to implement negated Unicode character classes so that
// [^\p{L}] matches exactly one complete code point that is not a letter,
// rather than an arbitrary byte sequence outside the letter encodings.
//
// It is equivalent to (but more efficiently expressed than) runeRangeToExpr
// over the combined ranges [0,0xD7FF] and [0xE000,0x10FFFF].
func unicodeAnyCodepointExpr() expr {
	unicodeAnyCPOnce.Do(func() {
		var cont byteSet
		cont.addRange(0x80, 0xBF)

		// 1-byte: U+0000–U+007F
		var ascii byteSet
		ascii.addRange(0x00, 0x7F)

		// 2-byte: U+0080–U+07FF   C2..DF  80..BF
		// (C0,C1 are invalid overlong encodings and excluded.)
		var lead2 byteSet
		lead2.addRange(0xC2, 0xDF)

		// 3-byte, case 1: U+0800–U+0FFF   E0  A0..BF  80..BF
		var e0c1 byteSet
		e0c1.addRange(0xA0, 0xBF)

		// 3-byte, case 2: U+1000–U+CFFF   E1..EC  80..BF  80..BF
		var l3m byteSet
		l3m.addRange(0xE1, 0xEC)

		// 3-byte, case 3: U+D000–U+D7FF   ED  80..9F  80..BF
		// (Stops before surrogates U+D800–U+DFFF which use ED A0..BF.)
		var edc1 byteSet
		edc1.addRange(0x80, 0x9F)

		// 3-byte, case 4: U+E000–U+FFFF   EE..EF  80..BF  80..BF
		var l3h byteSet
		l3h.addRange(0xEE, 0xEF)

		// 4-byte, case 1: U+10000–U+3FFFF   F0  90..BF  80..BF  80..BF
		// (F0 80..8F would be overlong; minimum valid second byte is 0x90.)
		var f0c1 byteSet
		f0c1.addRange(0x90, 0xBF)

		// 4-byte, case 2: U+40000–U+FFFFF   F1..F3  80..BF  80..BF  80..BF
		var l4m byteSet
		l4m.addRange(0xF1, 0xF3)

		// 4-byte, case 3: U+100000–U+10FFFF   F4  80..8F  80..BF  80..BF
		// (F4 90..BF would exceed U+10FFFF.)
		var f4c1 byteSet
		f4c1.addRange(0x80, 0x8F)

		unicodeAnyCPExpr = mkUnion(
			// 1-byte
			mkCls(ascii),
			// 2-byte
			mkConcat(mkCls(lead2), mkCls(cont)),
			// 3-byte (four cases)
			mkConcat(mkLit(0xE0), mkConcat(mkCls(e0c1), mkCls(cont))),
			mkConcat(mkCls(l3m), mkConcat(mkCls(cont), mkCls(cont))),
			mkConcat(mkLit(0xED), mkConcat(mkCls(edc1), mkCls(cont))),
			mkConcat(mkCls(l3h), mkConcat(mkCls(cont), mkCls(cont))),
			// 4-byte (three cases)
			mkConcat(mkLit(0xF0), mkConcat(mkCls(f0c1), mkConcat(mkCls(cont), mkCls(cont)))),
			mkConcat(mkCls(l4m), mkConcat(mkCls(cont), mkConcat(mkCls(cont), mkCls(cont)))),
			mkConcat(mkLit(0xF4), mkConcat(mkCls(f4c1), mkConcat(mkCls(cont), mkCls(cont)))),
		)
	})
	return unicodeAnyCPExpr
}
