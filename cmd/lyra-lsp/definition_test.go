package main

import (
	"strings"
	"testing"

	"github.com/owenrumney/go-lsp/servertest"
)

func TestDefinition_LocalVariable(t *testing.T) {
	h := servertest.New(t, newHandler())
	src := `
	let x = 5
	let y = x`
	openAndWait(t, h, src)
	// "x" in "let y = x" is at line 2 (0-based), col 9 (0-based).
	locs, err := h.Definition(testURI, 2, 9)
	if err != nil {
		t.Fatalf("Definition: %v", err)
	}
	if len(locs) != 1 {
		t.Fatalf("expected 1 location, got %d", len(locs))
	}
	// "let x = 5" is on line 1 (0-based).
	if locs[0].Range.Start.Line != 1 {
		t.Errorf("expected definition on line 1, got line %d", locs[0].Range.Start.Line)
	}
}

func TestDefinition_StructTypeName(t *testing.T) {
	h := servertest.New(t, newHandler())
	src := `
	struct Point {
		x: i32,
		y: i32,
	}
	var origin = Point {
		x: 0,
		y: 0,
	}`
	openAndWait(t, h, src)
	// "Point" in "var origin = Point {" starts at line 5, col 14 (0-based).
	locs, err := h.Definition(testURI, 5, 14)
	if err != nil {
		t.Fatalf("Definition: %v", err)
	}
	if len(locs) != 1 {
		t.Fatalf("expected 1 location for struct type name, got %d", len(locs))
	}
	// struct declaration is on line 1.
	if locs[0].Range.Start.Line != 1 {
		t.Errorf("expected definition on line 1, got line %d", locs[0].Range.Start.Line)
	}
}

func TestDefinition_NoResult_OnLiteral(t *testing.T) {
	h := servertest.New(t, newHandler())
	openAndWait(t, h, "let x = 42")
	// cursor on the literal "42" — should return empty, not an error
	locs, err := h.Definition(testURI, 0, 8)
	if err != nil {
		t.Fatalf("Definition: %v", err)
	}
	if len(locs) != 0 {
		t.Errorf("expected no locations for literal, got %d", len(locs))
	}
}

func TestDefinition_NestedScope(t *testing.T) {
	h := servertest.New(t, newHandler())
	src := `
	let f = () -> i32 => {
		let n = 10
		n
	}`
	openAndWait(t, h, src)
	// "n" on line 3 (0-based), col 2.
	locs, err := h.Definition(testURI, 3, 2)
	if err != nil {
		t.Fatalf("Definition: %v", err)
	}
	if len(locs) != 1 {
		t.Fatalf("expected 1 location, got %d", len(locs))
	}
	// "let n = 10" is on line 2 (0-based).
	if locs[0].Range.Start.Line != 2 {
		t.Errorf("expected definition on line 2, got line %d", locs[0].Range.Start.Line)
	}
}

func TestDefinition_OuterScopeFromInner(t *testing.T) {
	h := servertest.New(t, newHandler())
	src := `
	let x = 99
	let f = () -> i32 => {
		x
	}`
	openAndWait(t, h, src)
	// "x" on line 3 (0-based), col 2.
	locs, err := h.Definition(testURI, 3, 2)
	if err != nil {
		t.Fatalf("Definition: %v", err)
	}
	if len(locs) != 1 {
		t.Fatalf("expected 1 location, got %d", len(locs))
	}
	// "let x = 99" is on line 1.
	if locs[0].Range.Start.Line != 1 {
		t.Errorf("expected definition on line 1, got line %d", locs[0].Range.Start.Line)
	}
}

// **Go-to-definition on a type in a *type* position.** Until 08/25 this did nothing at
// all: `Definition` starts from `findExprAtPos`, which walks expressions, and a type in a
// signature is not an expression — so `Node { … }` resolved while `(n: Node)`, `-> Node`
// and a field's `n: Node` silently answered nothing. It reads as the server not
// supporting types, which is why it was reported as an editor limitation.
//
// The positions are enumerated rather than sampled, because each is a different collector
// path — a generic argument recurses through parseType, a parameterized *head* does not,
// an `impl` target is a third — and one working says nothing about the others.
func TestDefinition_TypeInATypePosition(t *testing.T) {
	for _, tc := range []struct {
		name     string
		line     int
		needle   string
		wantLine int
	}{
		{"parameter", 5, "Point)", 1},
		{"return type", 6, "Point =>", 1},
		{"local annotation", 8, "Point = p", 1},
		{"generic argument", 7, "Point>", 1},
		{"parameterized head", 7, "Maybe<", 0},
		{"array element", 7, "Point)", 1},
		{"field type", 2, "Point, b", 1},
		{"impl target", 4, "Point {", 1},
		{"alias target", 3, "Point", 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := servertest.New(t, newHandler())
			src := `data Maybe<t> = None | Some t
struct Point { x: i64, y: i64 }
struct Line { a: Point, b: Point }
type Spot = Point
impl Shownish for Point { }
let takes = pure (p: Point) -> i64 => p.x
let gives = pure (n: i64) -> Point => Point { x: n, y: n }
let holds = pure (m: Maybe<Point>, xs: []Point) -> i64 => 0
let main = () -> void => { let p = Point { x: 1, y: 2 }; let q: Point = p; println(q.x) }`
			openAndWait(t, h, src)
			col := strings.Index(strings.Split(src, "\n")[tc.line], tc.needle)
			if col < 0 {
				t.Fatalf("%q not on line %d", tc.needle, tc.line)
			}
			locs, err := h.Definition(testURI, tc.line, col)
			if err != nil {
				t.Fatalf("Definition: %v", err)
			}
			if len(locs) != 1 {
				t.Fatalf("expected 1 location, got %d", len(locs))
			}
			if got := int(locs[0].Range.Start.Line); got != tc.wantLine {
				t.Errorf("jumped to line %d; want %d", got, tc.wantLine)
			}
		})
	}
}

// **A primitive has no declaration to jump to**, so the answer is nothing rather than a
// guess. Asserted because the fallback resolves by *name*, and a name that resolves to no
// declaration must fall through rather than land somewhere plausible.
func TestDefinition_APrimitiveTypeResolvesToNothing(t *testing.T) {
	h := servertest.New(t, newHandler())
	src := `
struct Point { x: i64 }
let f = pure (n: i64) -> i64 => n`
	openAndWait(t, h, src)
	col := strings.Index(strings.Split(src, "\n")[2], "i64)")
	locs, err := h.Definition(testURI, 2, col)
	if err != nil {
		t.Fatalf("Definition: %v", err)
	}
	if len(locs) != 0 {
		t.Errorf("a primitive must resolve to nothing; got %v", locs)
	}
}

// **A trait name is written in positions that look like a type's and are not** — a
// `where` bound, an inline `<t: Shown>`, an `impl`'s trait, a supertrait list. None of
// them passes through `parseType`, since a bound is collected as a `[]string`, so each
// needed its own recording; and a trait is not in `SymbolTable.Types`, so resolving one
// needs `LookupTraitFrom` beside `LookupTypeFrom`.
func TestDefinition_TraitNameInABoundPosition(t *testing.T) {
	for _, tc := range []struct {
		name   string
		line   int
		needle string
	}{
		{"where bound", 5, "Shown ="},
		{"inline bound", 6, "Shown>"},
		{"impl trait", 3, "Shown for"},
		{"supertrait", 2, "Shown {"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := servertest.New(t, newHandler())
			src := `
trait Shown { pure show: (Self) -> string }
trait Sub: Shown { }
impl Shown for Point { show = pure (self) => "p" }
struct Point { x: i64 }
let a<t> where t: Shown = pure (v: t) -> string => v.show()
let b<t: Shown> = pure (v: t) -> string => v.show()`
			openAndWait(t, h, src)
			col := strings.Index(strings.Split(src, "\n")[tc.line], tc.needle)
			if col < 0 {
				t.Fatalf("%q not on line %d", tc.needle, tc.line)
			}
			locs, err := h.Definition(testURI, tc.line, col)
			if err != nil {
				t.Fatalf("Definition: %v", err)
			}
			if len(locs) != 1 {
				t.Fatalf("expected 1 location, got %d", len(locs))
			}
			// `trait Shown` is on line 1.
			if got := int(locs[0].Range.Start.Line); got != 1 {
				t.Errorf("jumped to line %d; want 1", got)
			}
		})
	}
}

// **A constructor in a `match` arm is a pattern, not an expression**, and every
// position-based feature starts from a walk over expressions — so go-to-definition on the
// `Keyboard` of `Keyboard(Up) => …` did nothing, while the same constructor used as a value
// resolved fine. Reported from the editor, where all three "Go to …" items appear to do
// nothing on a constructor.
//
// The failure was worse than silence: `findExprAtPos` returned whatever *expression* spanned
// the cursor — a nearby tuple literal — so the wrong node was resolved rather than none.
// That is why patterns get their own lookup rather than a fallback inside the expression one.
func TestDefinition_ConstructorInAPattern(t *testing.T) {
	h := servertest.New(t, newHandler())
	src := `
data Shape = Dot | Box(i64)
let area = pure (s: Shape) -> i64 => match s {
	Dot => 0,
	Box(n) => n,
}`
	openAndWait(t, h, src)
	for _, tc := range []struct {
		name   string
		line   int
		needle string
	}{
		{"nullary constructor", 3, "Dot =>"},
		{"constructor with a payload", 4, "Box(n)"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			col := strings.Index(strings.Split(src, "\n")[tc.line], tc.needle)
			locs, err := h.Definition(testURI, tc.line, col)
			if err != nil {
				t.Fatalf("Definition: %v", err)
			}
			if len(locs) != 1 {
				t.Fatalf("expected 1 location, got %d", len(locs))
			}
			// `data Shape` is on line 1.
			if got := int(locs[0].Range.Start.Line); got != 1 {
				t.Errorf("jumped to line %d; want 1 (the data declaration)", got)
			}
		})
	}
}

// A name the pattern **binds** is itself a declaration, so definition on it answers with
// itself — the same as on a `let`. Returning nothing there is what makes the editor look
// broken on a name that is plainly a binding site.
func TestDefinition_APatternBindingResolvesToItself(t *testing.T) {
	h := servertest.New(t, newHandler())
	src := `
data Shape = Dot | Box(i64)
let area = pure (s: Shape) -> i64 => match s {
	Dot => 0,
	Box(n) => n,
}`
	openAndWait(t, h, src)
	col := strings.Index(strings.Split(src, "\n")[4], "n)")
	locs, err := h.Definition(testURI, 4, col)
	if err != nil {
		t.Fatalf("Definition: %v", err)
	}
	if len(locs) != 1 {
		t.Fatalf("expected 1 location, got %d", len(locs))
	}
	if got := int(locs[0].Range.Start.Line); got != 4 {
		t.Errorf("jumped to line %d; want 4 (the binding itself)", got)
	}
}

// **Go to Declaration is Go to Definition here.** The two differ in a language where a name
// is declared in one place and defined in another; Lyra has no such split, so answering with
// the definition is correct — and answering at all matters, because an editor's menu is
// static and an unimplemented item sits there doing nothing, which reads as broken.
func TestDeclaration_AnswersAsDefinitionDoes(t *testing.T) {
	h := servertest.New(t, newHandler())
	src := `
let helper = pure (n: i64) -> i64 => n
let main = () -> void => println(helper(1))`
	openAndWait(t, h, src)
	col := strings.Index(strings.Split(src, "\n")[2], "helper(1)")
	decl, err := h.Declaration(testURI, 2, col)
	if err != nil {
		t.Fatalf("Declaration: %v", err)
	}
	def, err := h.Definition(testURI, 2, col)
	if err != nil {
		t.Fatalf("Definition: %v", err)
	}
	if len(decl) != 1 || len(def) != 1 || decl[0] != def[0] {
		t.Errorf("declaration %v and definition %v must agree", decl, def)
	}
}
