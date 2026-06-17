package main

import (
	"strings"
	"testing"

	"github.com/owenrumney/go-lsp/lsp"
	"github.com/owenrumney/go-lsp/servertest"
)

// decodedToken is a semantic token recovered from the flat encoding, annotated
// with the source text it covers for readable assertions.
type decodedToken struct {
	typeName string
	mods     []string
	text     string
}

// decodeSemanticTokens reverses the delta-encoding and resolves each token's
// type/modifier names and covered source text.
func decodeSemanticTokens(t *testing.T, tok *lsp.SemanticTokens, source string) []decodedToken {
	t.Helper()
	if tok == nil {
		return nil
	}
	if len(tok.Data)%5 != 0 {
		t.Fatalf("token data length %d is not a multiple of 5", len(tok.Data))
	}
	lines := strings.Split(source, "\n")
	var out []decodedToken
	line, char := 0, 0
	for i := 0; i < len(tok.Data); i += 5 {
		deltaLine, deltaChar, length, typeIdx, modBits := tok.Data[i], tok.Data[i+1], tok.Data[i+2], tok.Data[i+3], tok.Data[i+4]
		if deltaLine == 0 {
			char += deltaChar
		} else {
			line += deltaLine
			char = deltaChar
		}
		if typeIdx < 0 || typeIdx >= len(semanticTokenTypes) {
			t.Fatalf("token type index %d out of range", typeIdx)
		}
		text := ""
		if line < len(lines) && char+length <= len(lines[line]) {
			text = lines[line][char : char+length]
		}
		var mods []string
		for b, name := range semanticTokenModifiers {
			if modBits&(1<<b) != 0 {
				mods = append(mods, name)
			}
		}
		out = append(out, decodedToken{typeName: semanticTokenTypes[typeIdx], mods: mods, text: text})
	}
	return out
}

// findToken returns the first decoded token whose covered text equals want.
func findToken(toks []decodedToken, want string) (decodedToken, bool) {
	for _, tk := range toks {
		if tk.text == want {
			return tk, true
		}
	}
	return decodedToken{}, false
}

func hasMod(tk decodedToken, mod string) bool {
	for _, m := range tk.mods {
		if m == mod {
			return true
		}
	}
	return false
}

func TestSemanticTokens_BindingKinds(t *testing.T) {
	h := servertest.New(t, newHandler())
	// A let, a var, and a function binding, each referenced so the usage gets a
	// token; the declaration name itself is left to the TextMate grammar.
	src := `
	let a = 1
	var b = 2
	let f = (n: i32) -> i32 => n
	let ra = a
	let rb = b
	let rf = f
`
	openAndWait(t, h, src)

	got, err := h.SemanticTokensFull(testURI)
	if err != nil {
		t.Fatalf("SemanticTokensFull: %v", err)
	}
	toks := decodeSemanticTokens(t, got, src)

	if tk, ok := findToken(toks, "a"); !ok {
		t.Errorf("no token for `a`")
	} else if tk.typeName != "variable" || !hasMod(tk, "readonly") {
		t.Errorf("`a` (let) = %v %v, want variable readonly", tk.typeName, tk.mods)
	}

	if tk, ok := findToken(toks, "b"); !ok {
		t.Errorf("no token for `b`")
	} else if tk.typeName != "variable" || hasMod(tk, "readonly") {
		t.Errorf("`b` (var) = %v %v, want variable (no readonly)", tk.typeName, tk.mods)
	}

	if tk, ok := findToken(toks, "f"); !ok {
		t.Errorf("no token for `f`")
	} else if tk.typeName != "function" {
		t.Errorf("`f` = %v, want function", tk.typeName)
	}
}

func TestSemanticTokens_DeclarationNames(t *testing.T) {
	h := servertest.New(t, newHandler())
	// Every binding here is a declaration with no later usage; the declaration
	// names themselves must still be tokenized.
	src := `
	const MAX = 10
	let imm = 1
	var counter = 2
	struct Point { x: i64, y: i64 }
	data Color = Red | Green | Blue
`
	openAndWait(t, h, src)

	got, err := h.SemanticTokensFull(testURI)
	if err != nil {
		t.Fatalf("SemanticTokensFull: %v", err)
	}
	toks := decodeSemanticTokens(t, got, src)

	cases := []struct {
		text     string
		typeName string
		readonly bool
	}{
		{"MAX", "variable", true},      // const
		{"imm", "variable", true},      // let (deeply immutable)
		{"counter", "variable", false}, // var
		{"Point", "type", false},
		{"Color", "type", false},
	}
	for _, c := range cases {
		tk, ok := findToken(toks, c.text)
		if !ok {
			t.Errorf("no declaration token for %q", c.text)
			continue
		}
		if tk.typeName != c.typeName {
			t.Errorf("%q = %v, want %v", c.text, tk.typeName, c.typeName)
		}
		if hasMod(tk, "readonly") != c.readonly {
			t.Errorf("%q readonly=%v, want %v", c.text, hasMod(tk, "readonly"), c.readonly)
		}
	}
}

func TestSemanticTokens_ParameterAndType(t *testing.T) {
	h := servertest.New(t, newHandler())
	// The lambda has a block body so its parameter scope is reachable; the
	// function scope holding `n` is only registered against a BlockExpr.
	src := `
	struct Point {
		x: i64,
		y: i64,
	}
	let f = (n: i32) -> i32 => {
		n
	}
	let p = Point { x: 1, y: 2 }
	let q = p.x
`
	openAndWait(t, h, src)

	got, err := h.SemanticTokensFull(testURI)
	if err != nil {
		t.Fatalf("SemanticTokensFull: %v", err)
	}
	toks := decodeSemanticTokens(t, got, src)

	// `n` in the lambda body resolves to the parameter.
	if tk, ok := findToken(toks, "n"); !ok {
		t.Errorf("no token for `n`")
	} else if tk.typeName != "parameter" {
		t.Errorf("`n` = %v, want parameter", tk.typeName)
	}

	// The struct literal type name.
	if tk, ok := findToken(toks, "Point"); !ok {
		t.Errorf("no token for `Point`")
	} else if tk.typeName != "type" {
		t.Errorf("`Point` = %v, want type", tk.typeName)
	}

	// Member access property `x` in `p.x` — placed precisely on the property.
	var propX bool
	for _, tk := range toks {
		if tk.text == "x" && tk.typeName == "property" {
			propX = true
		}
	}
	if !propX {
		t.Errorf("expected a `property` token for `x` in `p.x`, got %v", toks)
	}
}

func TestSemanticTokens_DataConstructor(t *testing.T) {
	h := servertest.New(t, newHandler())
	src := `
	data Color = Red | Green | Blue
	let c = Red
`
	openAndWait(t, h, src)

	got, err := h.SemanticTokensFull(testURI)
	if err != nil {
		t.Fatalf("SemanticTokensFull: %v", err)
	}
	toks := decodeSemanticTokens(t, got, src)

	if tk, ok := findToken(toks, "Red"); !ok {
		t.Errorf("no token for `Red`")
	} else if tk.typeName != "enumMember" {
		t.Errorf("`Red` = %v, want enumMember", tk.typeName)
	}
}
