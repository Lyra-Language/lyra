package regex

import (
	"fmt"
	"strings"
	"unicode"
)

// parseError is a fixed-position parse failure.
type parseError struct {
	Pos int
	Msg string
}

func (e *parseError) Error() string {
	return fmt.Sprintf("regex parse error at offset %d: %s", e.Pos, e.Msg)
}

// parser is a recursive-descent parser for the regex surface syntax.
//
// Precedence (low to high):
//
//	alt    := inter ('|' inter)*
//	inter  := concat ('&' concat)*
//	concat := repeat*
//	repeat := atom postfix?      where postfix ∈ { *, +, ?, {n}, {n,m}, {n,} }
//	atom   := group | class | escape | anchor | char | '_' | '.'
//
// Complement `~(...)` is at the atom level because it always wraps a group.
type parser struct {
	src []byte
	pos int

	// active flags (mutable via (?flags) groups)
	flags patternFlags

	// errors collected during parsing
	err *parseError
}

// patternFlags are the flags that affect parsing/expression-building decisions.
type patternFlags struct {
	caseInsensitive bool // (?i) - fold ASCII letters in literals and classes
	dotAll          bool // (?s) - '.' matches '\n'
	multiLine       bool // (?m) - ^/$ are line anchors (default true)
	ignoreSpace     bool // (?x) - whitespace and '#' comments are ignored
}

func newParser(src string, opts Options) *parser {
	return &parser{
		src: []byte(src),
		pos: 0,
		flags: patternFlags{
			caseInsensitive: opts.CaseInsensitive,
			dotAll:          opts.DotAll,
			multiLine:       opts.MultiLine,
			ignoreSpace:     opts.IgnoreWhitespace,
		},
	}
}

func (p *parser) errorf(pos int, format string, args ...any) *parseError {
	if p.err != nil {
		return p.err
	}
	p.err = &parseError{Pos: pos, Msg: fmt.Sprintf(format, args...)}
	return p.err
}

func (p *parser) parse() (expr, error) {
	e, err := p.parseAlt(false)
	if err != nil {
		return nil, err
	}
	p.skipIgnored()
	if p.pos < len(p.src) {
		return nil, p.errorf(p.pos, "unexpected %q", p.src[p.pos])
	}
	return e, nil
}

// ── peek / advance / whitespace ─────────────────────────────────────────────

func (p *parser) peek() (byte, bool) {
	p.skipIgnored()
	if p.pos >= len(p.src) {
		return 0, false
	}
	return p.src[p.pos], true
}

func (p *parser) peekRaw() (byte, bool) {
	if p.pos >= len(p.src) {
		return 0, false
	}
	return p.src[p.pos], true
}

func (p *parser) advance() byte {
	b := p.src[p.pos]
	p.pos++
	return b
}

// skipIgnored skips whitespace and '#'-comments when (?x) is on.
func (p *parser) skipIgnored() {
	if !p.flags.ignoreSpace {
		return
	}
	for p.pos < len(p.src) {
		c := p.src[p.pos]
		switch {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			p.pos++
		case c == '#':
			// comment to end of line
			for p.pos < len(p.src) && p.src[p.pos] != '\n' {
				p.pos++
			}
		default:
			return
		}
	}
}

// ── grammar ─────────────────────────────────────────────────────────────────

func (p *parser) parseAlt(inGroup bool) (expr, error) {
	left, err := p.parseInter(inGroup)
	if err != nil {
		return nil, err
	}
	for {
		c, ok := p.peek()
		if !ok || c != '|' {
			break
		}
		p.advance()
		right, err := p.parseInter(inGroup)
		if err != nil {
			return nil, err
		}
		left = mkUnion(left, right)
	}
	return left, nil
}

func (p *parser) parseInter(inGroup bool) (expr, error) {
	left, err := p.parseConcat(inGroup)
	if err != nil {
		return nil, err
	}
	for {
		c, ok := p.peek()
		if !ok || c != '&' {
			break
		}
		p.advance()
		right, err := p.parseConcat(inGroup)
		if err != nil {
			return nil, err
		}
		left = mkInter(left, right)
	}
	return left, nil
}

func (p *parser) parseConcat(inGroup bool) (expr, error) {
	out := exprEps
	for {
		c, ok := p.peek()
		if !ok {
			return out, nil
		}
		// Terminators of a concatenation:
		if c == '|' || c == '&' {
			return out, nil
		}
		if inGroup && c == ')' {
			return out, nil
		}
		// Misplaced postfix operators:
		if c == '*' || c == '+' || c == '?' || c == '{' {
			return nil, p.errorf(p.pos, "unexpected quantifier %q (no preceding atom)", c)
		}
		piece, err := p.parseRepeat()
		if err != nil {
			return nil, err
		}
		if piece == nil {
			return out, nil
		}
		out = mkConcat(out, piece)
	}
}

func (p *parser) parseRepeat() (expr, error) {
	atom, err := p.parseAtom()
	if err != nil {
		return nil, err
	}
	if atom == nil {
		return nil, nil
	}
	c, ok := p.peekRaw()
	if !ok {
		return atom, nil
	}
	switch c {
	case '*':
		p.advance()
		if err := p.rejectLazy(); err != nil {
			return nil, err
		}
		return mkStar(atom), nil
	case '+':
		p.advance()
		if err := p.rejectLazy(); err != nil {
			return nil, err
		}
		return mkPlus(atom), nil
	case '?':
		p.advance()
		if err := p.rejectLazy(); err != nil {
			return nil, err
		}
		return mkOpt(atom), nil
	case '{':
		// {n}, {n,}, {n,m} — but only if it actually parses as a repetition.
		// Save position so we can back off if it's a literal "{".
		save := p.pos
		min, max, ok := p.tryParseRepCount()
		if !ok {
			p.pos = save
			return atom, nil
		}
		if err := p.rejectLazy(); err != nil {
			return nil, err
		}
		return mkRepeat(atom, min, max), nil
	}
	return atom, nil
}

func (p *parser) rejectLazy() error {
	if p.pos < len(p.src) && p.src[p.pos] == '?' {
		return p.errorf(p.pos, "lazy quantifiers (*?, +?, ??, {n,m}?) are not supported; rewrite with complement")
	}
	return nil
}

// tryParseRepCount tries to parse {n}, {n,}, or {n,m} starting at the '{'.
// Returns ok=false (and does not advance) if the construct isn't a valid
// repetition (so '{' can be treated as a literal character).
func (p *parser) tryParseRepCount() (min, max int, ok bool) {
	if p.pos >= len(p.src) || p.src[p.pos] != '{' {
		return 0, 0, false
	}
	save := p.pos
	p.pos++ // consume '{'
	n, gotN := p.readNumber()
	if !gotN {
		p.pos = save
		return 0, 0, false
	}
	min = n
	max = n
	if p.pos < len(p.src) && p.src[p.pos] == ',' {
		p.pos++
		m, gotM := p.readNumber()
		if gotM {
			max = m
		} else {
			max = -1 // {n,} unbounded
		}
	}
	if p.pos >= len(p.src) || p.src[p.pos] != '}' {
		p.pos = save
		return 0, 0, false
	}
	p.pos++ // consume '}'
	return min, max, true
}

func (p *parser) readNumber() (int, bool) {
	start := p.pos
	for p.pos < len(p.src) && p.src[p.pos] >= '0' && p.src[p.pos] <= '9' {
		p.pos++
	}
	if p.pos == start {
		return 0, false
	}
	n := 0
	for i := start; i < p.pos; i++ {
		n = n*10 + int(p.src[i]-'0')
	}
	return n, true
}

// ── atom ────────────────────────────────────────────────────────────────────

func (p *parser) parseAtom() (expr, error) {
	c, ok := p.peek()
	if !ok {
		return nil, nil
	}
	switch c {
	case '(':
		return p.parseGroup()
	case '[':
		return p.parseClass()
	case '\\':
		return p.parseEscape()
	case '.':
		p.advance()
		if p.flags.dotAll {
			return exprAny, nil
		}
		return mkCls(nonNewlineSet()), nil
	case '_':
		p.advance()
		return exprAny, nil
	case '^':
		p.advance()
		if p.flags.multiLine {
			return exprBL, nil
		}
		return exprBT, nil
	case '$':
		p.advance()
		if p.flags.multiLine {
			return exprEL, nil
		}
		return exprET, nil
	case '~':
		// resharp complement: must be followed by '('
		p.advance()
		if p.pos >= len(p.src) || p.src[p.pos] != '(' {
			return nil, p.errorf(p.pos, "'~' must be followed by '(' (complement requires a parenthesised body)")
		}
		body, err := p.parseGroup()
		if err != nil {
			return nil, err
		}
		return mkCompl(body), nil
	case '|', '&', ')', '*', '+', '?', '{', '}', ']':
		// Should not be reached as an atom-start; the caller handles these.
		return nil, nil
	default:
		p.advance()
		return p.literalExpr(c), nil
	}
}

func (p *parser) literalExpr(b byte) expr {
	if p.flags.caseInsensitive && isAsciiLetter(b) {
		var s byteSet
		s.add(b)
		s.add(flipCase(b))
		return mkCls(s)
	}
	return mkLit(b)
}

func isAsciiLetter(b byte) bool { return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') }
func flipCase(b byte) byte {
	switch {
	case b >= 'a' && b <= 'z':
		return b - 'a' + 'A'
	case b >= 'A' && b <= 'Z':
		return b - 'A' + 'a'
	}
	return b
}

func nonNewlineSet() byteSet {
	s := fullByteSet()
	s[byte('\n')>>6] &^= 1 << ('\n' & 63)
	return s
}

// ── group: '(' ... ')' with optional inline flags ───────────────────────────

func (p *parser) parseGroup() (expr, error) {
	openPos := p.pos
	p.advance() // consume '('

	// Inline flag group: (?flags) or (?flags:expr) or (?flags-flags) or (?-flags)
	if p.pos+1 < len(p.src) && p.src[p.pos] == '?' {
		// Detect (?...) — including lookaround forms we don't support yet.
		look := p.src[p.pos+1]
		if look == '=' || look == '!' {
			return nil, p.errorf(p.pos, "lookarounds are not supported in Phase 1")
		}
		if look == '<' {
			return nil, p.errorf(p.pos, "lookbehinds / named groups are not supported in Phase 1")
		}
		if look == 'P' {
			return nil, p.errorf(p.pos, "named groups (?P<name>...) are not supported")
		}
		// Try to parse as a flag group.
		consumed, isScoped, set, clear, ok := p.tryParseFlags()
		if ok {
			savedFlags := p.flags
			applyFlagDelta(&p.flags, set, clear)
			if isScoped {
				// (?flags:expr) — parse inner expression, then close.
				inner, err := p.parseAlt(true)
				if err != nil {
					return nil, err
				}
				if p.pos >= len(p.src) || p.src[p.pos] != ')' {
					return nil, p.errorf(p.pos, "expected ')' to close (?flags:...) group opened at %d", openPos)
				}
				p.advance() // ')'
				p.flags = savedFlags
				return inner, nil
			}
			// (?flags) — flags apply to the remainder of the enclosing scope.
			// No body to consume. Expect immediate ')'.
			_ = consumed
			if p.pos >= len(p.src) || p.src[p.pos] != ')' {
				return nil, p.errorf(p.pos, "expected ')' after flag group opened at %d", openPos)
			}
			p.advance() // ')'
			return exprEps, nil
		}
		// Otherwise, treat as a regular non-capturing group beginning with "?:"
		if p.pos+1 < len(p.src) && p.src[p.pos] == '?' && p.src[p.pos+1] == ':' {
			p.pos += 2
			inner, err := p.parseAlt(true)
			if err != nil {
				return nil, err
			}
			if p.pos >= len(p.src) || p.src[p.pos] != ')' {
				return nil, p.errorf(p.pos, "expected ')' to close group opened at %d", openPos)
			}
			p.advance()
			return inner, nil
		}
		return nil, p.errorf(p.pos, "unrecognised (?...) construct")
	}

	// Ordinary group (capture-less in our engine).
	inner, err := p.parseAlt(true)
	if err != nil {
		return nil, err
	}
	if p.pos >= len(p.src) || p.src[p.pos] != ')' {
		return nil, p.errorf(p.pos, "expected ')' to close group opened at %d", openPos)
	}
	p.advance()
	return inner, nil
}

// tryParseFlags attempts to read a flag spec starting at the '?'.
// Returns:
//
//	consumed bytes after the '?' (informational),
//	isScoped if the spec ends in ':' (i.e. (?flags:expr)),
//	set / clear bit masks,
//	ok if the syntax was valid.
//
// On failure, position is restored.
func (p *parser) tryParseFlags() (consumed int, isScoped bool, set, clear flagBits, ok bool) {
	save := p.pos
	if p.pos >= len(p.src) || p.src[p.pos] != '?' {
		return 0, false, 0, 0, false
	}
	p.pos++ // '?'
	target := &set
	for p.pos < len(p.src) {
		c := p.src[p.pos]
		switch c {
		case 'i':
			*target |= flagI
		case 's':
			*target |= flagS
		case 'm':
			*target |= flagM
		case 'x':
			*target |= flagX
		case '-':
			if target == &clear {
				p.pos = save
				return 0, false, 0, 0, false
			}
			target = &clear
		case ':':
			p.pos++
			return p.pos - save, true, set, clear, true
		case ')':
			return p.pos - save, false, set, clear, true
		default:
			p.pos = save
			return 0, false, 0, 0, false
		}
		p.pos++
	}
	p.pos = save
	return 0, false, 0, 0, false
}

type flagBits uint8

const (
	flagI flagBits = 1 << iota
	flagS
	flagM
	flagX
)

func applyFlagDelta(f *patternFlags, set, clear flagBits) {
	if set&flagI != 0 {
		f.caseInsensitive = true
	}
	if set&flagS != 0 {
		f.dotAll = true
	}
	if set&flagM != 0 {
		f.multiLine = true
	}
	if set&flagX != 0 {
		f.ignoreSpace = true
	}
	if clear&flagI != 0 {
		f.caseInsensitive = false
	}
	if clear&flagS != 0 {
		f.dotAll = false
	}
	if clear&flagM != 0 {
		f.multiLine = false
	}
	if clear&flagX != 0 {
		f.ignoreSpace = false
	}
}

// ── character class: [ ... ] ────────────────────────────────────────────────

func (p *parser) parseClass() (expr, error) {
	startPos := p.pos
	p.advance() // '['

	negate := false
	if p.pos < len(p.src) && p.src[p.pos] == '^' {
		negate = true
		p.pos++
	}

	var set byteSet
	first := true
	for {
		if p.pos >= len(p.src) {
			return nil, p.errorf(startPos, "unterminated character class")
		}
		// ']' closes; an initial ']' (right after '[' or '[^') is literal in most engines.
		if p.src[p.pos] == ']' && !first {
			p.pos++
			break
		}
		lo, err := p.readClassChar()
		if err != nil {
			return nil, err
		}
		// Range?
		if p.pos+1 < len(p.src) && p.src[p.pos] == '-' && p.src[p.pos+1] != ']' {
			p.pos++ // '-'
			hi, err := p.readClassChar()
			if err != nil {
				return nil, err
			}
			if lo.isSet {
				p.applyClassSet(&set, lo.set)
				continue
			}
			if hi.isSet {
				return nil, p.errorf(p.pos, "shorthand classes cannot be the upper bound of a range")
			}
			if lo.b > hi.b {
				return nil, p.errorf(p.pos, "range out of order: %q > %q", lo.b, hi.b)
			}
			set.addRange(lo.b, hi.b)
			if p.flags.caseInsensitive {
				p.addCaseFolded(&set, lo.b, hi.b)
			}
			first = false
			continue
		}
		// Single element.
		if lo.isSet {
			p.applyClassSet(&set, lo.set)
		} else {
			set.add(lo.b)
			if p.flags.caseInsensitive && isAsciiLetter(lo.b) {
				set.add(flipCase(lo.b))
			}
		}
		first = false
	}

	if negate {
		set = set.complement()
	}
	return mkCls(set), nil
}

func (p *parser) applyClassSet(out *byteSet, in byteSet) {
	*out = out.union(in)
}

func (p *parser) addCaseFolded(out *byteSet, lo, hi byte) {
	for b := int(lo); b <= int(hi); b++ {
		if isAsciiLetter(byte(b)) {
			out.add(flipCase(byte(b)))
		}
	}
}

// classChar is the result of reading one element from inside a class:
// either a literal byte, or a shorthand set (e.g. \d -> 0-9).
type classChar struct {
	b     byte
	set   byteSet
	isSet bool
}

func (p *parser) readClassChar() (classChar, error) {
	if p.pos >= len(p.src) {
		return classChar{}, p.errorf(p.pos, "unexpected end of pattern inside character class")
	}
	c := p.src[p.pos]
	if c != '\\' {
		p.pos++
		return classChar{b: c}, nil
	}
	// Escape inside class.
	p.pos++ // '\\'
	if p.pos >= len(p.src) {
		return classChar{}, p.errorf(p.pos, "trailing '\\' inside character class")
	}
	esc := p.src[p.pos]
	p.pos++
	switch esc {
	case 'd':
		return classChar{set: digitSet(), isSet: true}, nil
	case 'D':
		return classChar{set: digitSet().complement(), isSet: true}, nil
	case 'w':
		return classChar{set: wordSet(), isSet: true}, nil
	case 'W':
		return classChar{set: wordSet().complement(), isSet: true}, nil
	case 's':
		return classChar{set: spaceSet(), isSet: true}, nil
	case 'S':
		return classChar{set: spaceSet().complement(), isSet: true}, nil
	case 'n':
		return classChar{b: '\n'}, nil
	case 't':
		return classChar{b: '\t'}, nil
	case 'r':
		return classChar{b: '\r'}, nil
	case 'f':
		return classChar{b: '\f'}, nil
	case 'v':
		return classChar{b: '\v'}, nil
	case '0':
		return classChar{b: 0}, nil
	case 'a':
		return classChar{b: 7}, nil // bell
	case 'e':
		return classChar{b: 27}, nil // escape
	case 'x':
		b, err := p.readHexEscape()
		if err != nil {
			return classChar{}, err
		}
		return classChar{b: b}, nil
	default:
		// Treat all other escapes as literal (\\, \[, \], \-, etc.).
		if unicode.IsLetter(rune(esc)) {
			return classChar{}, p.errorf(p.pos, "unknown escape \\%c inside class", esc)
		}
		return classChar{b: esc}, nil
	}
}

// ── \-escapes outside of classes ────────────────────────────────────────────

func (p *parser) parseEscape() (expr, error) {
	startPos := p.pos
	p.advance() // '\\'
	if p.pos >= len(p.src) {
		return nil, p.errorf(startPos, "trailing '\\'")
	}
	c := p.src[p.pos]
	p.pos++
	switch c {
	case 'A':
		return exprBT, nil
	case 'z':
		return exprET, nil
	case 'd':
		return mkCls(digitSet()), nil
	case 'D':
		return mkCls(digitSet().complement()), nil
	case 'w':
		return mkCls(wordSet()), nil
	case 'W':
		return mkCls(wordSet().complement()), nil
	case 's':
		return mkCls(spaceSet()), nil
	case 'S':
		return mkCls(spaceSet().complement()), nil
	case 'b', 'B':
		return nil, p.errorf(startPos, "\\b/\\B word boundaries are not supported in Phase 1")
	case 'n':
		return p.literalExpr('\n'), nil
	case 't':
		return p.literalExpr('\t'), nil
	case 'r':
		return p.literalExpr('\r'), nil
	case 'f':
		return p.literalExpr('\f'), nil
	case 'v':
		return p.literalExpr('\v'), nil
	case '0':
		return mkLit(0), nil
	case 'a':
		return mkLit(7), nil
	case 'e':
		return mkLit(27), nil
	case 'x':
		b, err := p.readHexEscape()
		if err != nil {
			return nil, err
		}
		return p.literalExpr(b), nil
	default:
		// Reject backreferences and unknown alphabetic escapes; allow
		// non-alphanumeric escapes through as their literal byte.
		if unicode.IsDigit(rune(c)) {
			return nil, p.errorf(startPos, "backreferences (\\%c) are not supported", c)
		}
		if unicode.IsLetter(rune(c)) {
			return nil, p.errorf(startPos, "unknown escape \\%c", c)
		}
		return p.literalExpr(c), nil
	}
}

func (p *parser) readHexEscape() (byte, error) {
	// \xHH or \x{HH...}
	if p.pos < len(p.src) && p.src[p.pos] == '{' {
		p.pos++
		start := p.pos
		for p.pos < len(p.src) && isHex(p.src[p.pos]) {
			p.pos++
		}
		if p.pos == start || p.pos >= len(p.src) || p.src[p.pos] != '}' {
			return 0, p.errorf(start, "malformed \\x{...} escape")
		}
		hex := string(p.src[start:p.pos])
		p.pos++ // '}'
		v, err := hexValue(hex)
		if err != nil || v > 0xFF {
			return 0, p.errorf(start, "\\x{%s} is out of byte range", hex)
		}
		return byte(v), nil
	}
	if p.pos+1 >= len(p.src) || !isHex(p.src[p.pos]) || !isHex(p.src[p.pos+1]) {
		return 0, p.errorf(p.pos, "expected two hex digits after \\x")
	}
	hex := string(p.src[p.pos : p.pos+2])
	p.pos += 2
	v, err := hexValue(hex)
	if err != nil {
		return 0, p.errorf(p.pos-2, "malformed \\x escape: %v", err)
	}
	return byte(v), nil
}

func isHex(b byte) bool {
	return (b >= '0' && b <= '9') || (b >= 'a' && b <= 'f') || (b >= 'A' && b <= 'F')
}

func hexValue(s string) (int, error) {
	v := 0
	for _, c := range []byte(strings.ToLower(s)) {
		v <<= 4
		switch {
		case c >= '0' && c <= '9':
			v |= int(c - '0')
		case c >= 'a' && c <= 'f':
			v |= int(c-'a') + 10
		default:
			return 0, fmt.Errorf("invalid hex %q", c)
		}
	}
	return v, nil
}

// ── shorthand character classes ─────────────────────────────────────────────
//
// Phase 1 uses ASCII-only definitions; Phase 3 will extend these per the
// UnicodeMode option (resharp's default 2-byte \w, etc.).

func digitSet() byteSet {
	var s byteSet
	s.addRange('0', '9')
	return s
}

func wordSet() byteSet {
	var s byteSet
	s.addRange('a', 'z')
	s.addRange('A', 'Z')
	s.addRange('0', '9')
	s.add('_')
	return s
}

func spaceSet() byteSet {
	var s byteSet
	for _, c := range []byte{' ', '\t', '\n', '\r', '\v', '\f'} {
		s.add(c)
	}
	return s
}
