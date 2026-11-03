package types

import (
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
)

// An array's size in terms of `const` parameters is **canonical text**: `[N + 1]t` and
// `[1 + N]t` both hold "N+1" in StaticArrayType.SizeVar, and a bare `[N]t` holds "N" as it
// always did. So everything that compares two sizes (SameArraySize, TypesEqual, an
// instantiation's key) goes on comparing strings and is right, and a type stays a plain
// comparable value, which a pointer to an expression wouldn't be.
//
// **Linear, and that is the rule rather than a limit of the parser.** A sum of constants and
// constant multiples of parameters has one canonical form, so two equal sizes are equal
// strings with no proof needed, and one unknown in it can be solved from an argument's size
// with a division. `[N * M]t` has neither, so it is refused where it is written.

// linearSize is a size read out of its text: constant + the sum of its terms, sorted by name
// with no zero coefficient, which is what makes the text canonical.
type linearSize struct {
	constant int
	terms    []sizeTerm
}

type sizeTerm struct {
	coef int
	name string
}

// ParseSize reads a size as written or as canonical text and answers its canonical text. It
// answers false for anything that isn't linear in names and integers.
func ParseSize(text string) (string, bool) {
	s, ok := parseLinearSize(text)
	if !ok {
		return "", false
	}
	return s.String(), true
}

// SizeVarNames answers the `const` parameters a size mentions, in canonical order.
func SizeVarNames(size string) []string {
	s, ok := parseLinearSize(size)
	if !ok {
		return nil
	}
	names := make([]string, len(s.terms))
	for i, t := range s.terms {
		names[i] = t.name
	}
	return names
}

// SubstituteSize replaces each name in size that bindings holds an ArraySize for, and answers
// what a StaticArrayType holds: a number, or the text of what is still unknown. The number
// can be negative (`[N - 1]t` at N = 0), which is the reader's to refuse.
func SubstituteSize(size string, bindings map[string]Type) (int, string) {
	s, ok := parseLinearSize(size)
	if !ok {
		return 0, size
	}
	out := linearSize{constant: s.constant}
	for _, t := range s.terms {
		binding, bound := bindings[t.name].(ArraySize)
		if !bound {
			out = out.plus(linearSize{terms: []sizeTerm{t}}, 1)
			continue
		}
		out = out.plus(binding.linear(), t.coef)
	}
	return out.answer()
}

// SolveSize unifies a size written with parameters against an actual one: `[N + 1]t` against
// `[5]t` binds N to 4, and against `[M]t` binds N to M-1, settled when the caller is
// specialized. generics says which names are the callee's to solve; any other name in pattern
// is a name of the same scope as actual. It answers false when they can't agree.
//
// **Two unknowns answer true, unbound.** `[N + M]t` says nothing about either until another
// argument settles one, and the argument's assignability is checked once the call's bindings
// are all in, so a wrong size is still refused. If nothing ever settles them, the call reports
// the parameter as one it can't infer.
func SolveSize(pattern string, actual ArraySize, generics map[string]bool, bindings map[string]Type) bool {
	p, ok := parseLinearSize(pattern)
	if !ok {
		return false
	}
	// What is known of pattern, in actual's terms, and what is still to solve. Kept apart
	// because a callee's N and a caller's N are different parameters that can share a name.
	residual := actual.linear().plus(linearSize{constant: p.constant}, -1)
	var unknown []sizeTerm
	for _, t := range p.terms {
		if !generics[t.name] {
			residual = residual.plus(linearSize{terms: []sizeTerm{t}}, -1)
			continue
		}
		if binding, bound := bindings[t.name].(ArraySize); bound {
			residual = residual.plus(binding.linear(), -t.coef)
			continue
		}
		unknown = append(unknown, t)
	}
	switch len(unknown) {
	case 0:
		return residual.constant == 0 && len(residual.terms) == 0
	case 1:
		value, ok := residual.dividedBy(unknown[0].coef)
		if !ok || (len(value.terms) == 0 && value.constant < 0) {
			return false
		}
		size, sizeVar := value.answer()
		bindings[unknown[0].name] = ArraySize{Size: size, Var: sizeVar}
		return true
	}
	return true
}

// HasNegativeSize reports whether t holds an array whose size came to less than zero.
func HasNegativeSize(t Type) bool {
	negative := false
	walkSignature(t, func(t Type) {
		if arr, ok := t.(StaticArrayType); ok && arr.SizeVar == "" && arr.Size < 0 {
			negative = true
		}
	})
	return negative
}

func (a ArraySize) linear() linearSize {
	if a.Var == "" {
		return linearSize{constant: a.Size}
	}
	s, ok := parseLinearSize(a.Var)
	if !ok {
		return linearSize{terms: []sizeTerm{{coef: 1, name: a.Var}}}
	}
	return s
}

// plus answers s + scale*other.
func (s linearSize) plus(other linearSize, scale int) linearSize {
	coefs := make(map[string]int, len(s.terms)+len(other.terms))
	for _, t := range s.terms {
		coefs[t.name] += t.coef
	}
	for _, t := range other.terms {
		coefs[t.name] += scale * t.coef
	}
	out := linearSize{constant: s.constant + scale*other.constant}
	for _, name := range slices.Sorted(maps.Keys(coefs)) {
		if coefs[name] != 0 {
			out.terms = append(out.terms, sizeTerm{coef: coefs[name], name: name})
		}
	}
	return out
}

// dividedBy answers s / by when every coefficient divides exactly.
func (s linearSize) dividedBy(by int) (linearSize, bool) {
	if by == 0 || s.constant%by != 0 {
		return linearSize{}, false
	}
	out := linearSize{constant: s.constant / by}
	for _, t := range s.terms {
		if t.coef%by != 0 {
			return linearSize{}, false
		}
		out.terms = append(out.terms, sizeTerm{coef: t.coef / by, name: t.name})
	}
	return out, true
}

func (s linearSize) answer() (int, string) {
	if len(s.terms) == 0 {
		return s.constant, ""
	}
	return 0, s.String()
}

func (s linearSize) String() string {
	var b strings.Builder
	for _, t := range s.terms {
		switch {
		case t.coef == 1:
			if b.Len() > 0 {
				b.WriteByte('+')
			}
		case t.coef == -1:
			b.WriteByte('-')
		default:
			if t.coef > 0 && b.Len() > 0 {
				b.WriteByte('+')
			}
			fmt.Fprintf(&b, "%d*", t.coef)
		}
		b.WriteString(t.name)
	}
	if len(s.terms) == 0 {
		return strconv.Itoa(s.constant)
	}
	if s.constant > 0 {
		b.WriteByte('+')
	}
	if s.constant != 0 {
		b.WriteString(strconv.Itoa(s.constant))
	}
	return b.String()
}

// parseLinearSize is a small recursive descent over + - * and parentheses: the size arithmetic
// the grammar admits, and the canonical text this file writes, which has a leading minus.
func parseLinearSize(text string) (linearSize, bool) {
	p := sizeParser{text: strings.ReplaceAll(text, " ", "")}
	s, ok := p.sum()
	if !ok || p.at != len(p.text) {
		return linearSize{}, false
	}
	return s, true
}

type sizeParser struct {
	text string
	at   int
}

func (p *sizeParser) peek() byte {
	if p.at < len(p.text) {
		return p.text[p.at]
	}
	return 0
}

func (p *sizeParser) sum() (linearSize, bool) {
	scale := 1
	if p.peek() == '-' {
		p.at++
		scale = -1
	}
	first, ok := p.product()
	if !ok {
		return linearSize{}, false
	}
	out := linearSize{}.plus(first, scale)
	for p.peek() == '+' || p.peek() == '-' {
		scale = 1
		if p.peek() == '-' {
			scale = -1
		}
		p.at++
		next, ok := p.product()
		if !ok {
			return linearSize{}, false
		}
		out = out.plus(next, scale)
	}
	return out, true
}

func (p *sizeParser) product() (linearSize, bool) {
	out, ok := p.factor()
	if !ok {
		return linearSize{}, false
	}
	for p.peek() == '*' {
		p.at++
		next, ok := p.factor()
		if !ok {
			return linearSize{}, false
		}
		switch {
		case len(out.terms) == 0:
			out = linearSize{}.plus(next, out.constant)
		case len(next.terms) == 0:
			out = linearSize{}.plus(out, next.constant)
		default:
			return linearSize{}, false
		}
	}
	return out, true
}

func (p *sizeParser) factor() (linearSize, bool) {
	c := p.peek()
	switch {
	case c == '(':
		p.at++
		inner, ok := p.sum()
		if !ok || p.peek() != ')' {
			return linearSize{}, false
		}
		p.at++
		return inner, true
	case c >= '0' && c <= '9':
		start := p.at
		for p.peek() >= '0' && p.peek() <= '9' {
			p.at++
		}
		n, err := strconv.Atoi(p.text[start:p.at])
		if err != nil {
			return linearSize{}, false
		}
		return linearSize{constant: n}, true
	case c == '_' || (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z'):
		start := p.at
		for {
			c := p.peek()
			if c != '_' && (c < 'A' || c > 'Z') && (c < 'a' || c > 'z') && (c < '0' || c > '9') {
				break
			}
			p.at++
		}
		return linearSize{terms: []sizeTerm{{coef: 1, name: p.text[start:p.at]}}}, true
	}
	return linearSize{}, false
}
