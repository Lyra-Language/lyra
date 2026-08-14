package typechecker_test

import (
	"strings"
	"testing"

	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/types"
)

// --- valid concatenation ---

func TestStringConcat(t *testing.T) {
	res := parseCollectAndCheck(t, `let str = "hello" ++ "world"`, false)
	assertNoErrors(t, res)
}

func TestStringConcat_StringAnnotation(t *testing.T) {
	res := parseCollectAndCheck(t, `let s: string = "hello" ++ " world"`, false)
	assertNoErrors(t, res)
}

func TestStringConcat_InterpolatedLeft(t *testing.T) {
	// Interpolation segments are now type-checked, so the interpolated name must
	// be a declared, printable binding.
	res := parseCollectAndCheck(t, `
		let name: string = "Ada"
		let s = "hello ${name}" ++ " world"`, false)
	assertNoErrors(t, res)
}

func TestStringConcat_InterpolatedRight(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let name: string = "Ada"
		let s = "hello " ++ " ${name}"`, false)
	assertNoErrors(t, res)
}

func TestStringConcat_BothInterpolated(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let first: string = "Ada"
		let last: string = "Lovelace"
		let s = "hello ${first}" ++ " ${last}"`, false)
	assertNoErrors(t, res)
}

// --- type-table entry ---

func TestStringConcat_TypeTable_RecordsString(t *testing.T) {
	res := parseCollectAndCheck(t, `let s = "hello" ++ " world"`, false)
	assertNoErrors(t, res)
	decl := res.program.Statements[0].(*ast.VarDeclStmt)
	typ, ok := res.typeTable.Get(decl.Value)
	if !ok {
		t.Fatal("expected type table entry for string concat expr")
	}
	want := types.PrimitiveType{Name: types.String}
	if !types.TypesEqual(typ, want) {
		t.Errorf("expected %s, got %s", want, typ)
	}
}

// --- operand type errors ---

func TestStringConcat_NonStringLeft_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let n: i64 = 42
		let s = n ++ " world"
	`, false)
	assertErrorsAre(t, res, "operator ++: operands must be strings, got i64 and string — `++` does not convert; render the left operand first, with `.show()` or `\"${…}\"` interpolation")
}

func TestStringConcat_NonStringRight_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let n: i64 = 42
		let s = "hello" ++ n
	`, false)
	assertErrorsAre(t, res, "operator ++: operands must be strings, got string and i64 — `++` does not convert; render the right operand first, with `.show()` or `\"${…}\"` interpolation")
}

func TestStringConcat_BothNonString_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let n: i64 = 42
		let b: bool = true
		let s = n ++ b
	`, false)
	assertErrorsAre(t, res, "operator ++: operands must be strings, got i64 and boolean — `++` does not convert; render each operand first, with `.show()` or `\"${…}\"` interpolation")
}

func TestStringConcat_BoolVar_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let flag: bool = true
		let s = flag ++ "hello"
	`, false)
	assertErrorsAre(t, res, "operator ++: operands must be strings, got boolean and string — `++` does not convert; render the left operand first, with `.show()` or `\"${…}\"` interpolation")
}

// --- annotation mismatch ---

func TestStringConcat_WrongAnnotation_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `let s: i64 = "hello" ++ " world"`, false)
	assertErrorsAre(t, res, "s: cannot assign string to i64")
}

// ── `++` refuses a non-string operand and names the fix (08/14) ─────────────
//
// **It deliberately does not convert.** Accepting `string ++ rune` would be an implicit
// conversion in a language that refuses them everywhere else — `let c: Cents = plain_i64`
// is lyra-E046, `i64(x)` on a float is refused, and `string(r)` on a rune is refused *by
// name*. An operator quietly performing the conversion its own conversion function
// declines would be two mechanisms disagreeing, and the slope has a known bottom: if a
// rune converts then so does an integer, and `++` becomes JavaScript's `+`.
//
// So the message carries the cost: refuse, and name the spelling — the pattern lyra-E046
// and the float→int rejection both follow. `.show()` is not guessable from a rune, which
// is exactly why it belongs in the diagnostic.

func TestConcat_RuneOperandNamesTheFix(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let main = () => {
			let shades = "abc"
			var line = ""
			line = line ++ shades[0]
		}
	`, false)
	if len(res.errors) == 0 {
		t.Fatal("`string ++ rune` must be refused")
	}
	msg := res.errors[0].Error()
	for _, want := range []string{"got string and rune", "does not convert", ".show()", "the right operand"} {
		if !strings.Contains(msg, want) {
			t.Errorf("the diagnostic should mention %q; got: %s", want, msg)
		}
	}
}

// Which side is at fault is part of the fix — "render the right operand" is actionable
// where "operands must be strings" leaves the reader checking both.
func TestConcat_NamesTheOffendingSide(t *testing.T) {
	for name, tc := range map[string]struct{ src, want string }{
		"left":  {`let x = 42 ++ "a"`, "the left operand"},
		"right": {`let x = "a" ++ 42`, "the right operand"},
		"both":  {`let x = 42 ++ 7`, "each operand"},
	} {
		t.Run(name, func(t *testing.T) {
			res := parseCollectAndCheck(t, tc.src, false)
			if len(res.errors) == 0 {
				t.Fatalf("%s must be refused", tc.src)
			}
			if !strings.Contains(res.errors[0].Error(), tc.want) {
				t.Errorf("want %q in: %s", tc.want, res.errors[0])
			}
		})
	}
}
