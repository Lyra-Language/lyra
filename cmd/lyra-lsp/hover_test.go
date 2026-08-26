package main

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/owenrumney/go-lsp/servertest"
)

const testURI = "file:///test.lyra"

// openAndWait opens a document and waits for the analysis to complete.
func openAndWait(t *testing.T, h *servertest.Harness, source string) {
	t.Helper()
	if err := h.DidOpen(testURI, "lyra", source); err != nil {
		t.Fatalf("DidOpen: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := h.WaitForDiagnostics(ctx, testURI); err != nil {
		t.Fatalf("WaitForDiagnostics: %v", err)
	}
}

func TestHover_IdentifierExpr_ShowsType(t *testing.T) {
	h := servertest.New(t, newHandler())
	src := `
	let xs: [3]int = [1, 2, 3]
	let y = xs`
	openAndWait(t, h, src)
	// "xs" in "let y = xs" starts at line 2 (0-based), col 9 (0-based).
	hover, err := h.Hover(testURI, 2, 9)
	if err != nil {
		t.Fatalf("Hover: %v", err)
	}
	if hover == nil {
		t.Fatal("expected hover result, got nil")
	}
	if !strings.Contains(hover.Contents.Value, "xs") {
		t.Errorf("expected hover to mention 'xs', got: %q", hover.Contents.Value)
	}
	if !strings.Contains(hover.Contents.Value, "StaticArray") {
		t.Errorf("expected hover to mention type, got: %q", hover.Contents.Value)
	}
}

func TestHover_IndexExpr_ShowsElementType(t *testing.T) {
	h := servertest.New(t, newHandler())
	src := `
	let xs: [3]int = [1, 2, 3]
	let y = xs[0]`
	openAndWait(t, h, src)
	// "xs[0]" starts at line 2, col 9; hover on the 's' of xs.
	hover, err := h.Hover(testURI, 2, 10)
	if err != nil {
		t.Fatalf("Hover: %v", err)
	}
	if hover == nil {
		t.Fatal("expected hover result, got nil")
	}
	if !strings.Contains(hover.Contents.Value, "int") {
		t.Errorf("expected hover to show int type, got: %q", hover.Contents.Value)
	}
}

func TestHover_NoResult_ForBlankPosition(t *testing.T) {
	h := servertest.New(t, newHandler())
	openAndWait(t, h, "let x: int = 42")
	// Line 5 doesn't exist — should return nil without error.
	hover, err := h.Hover(testURI, 5, 0)
	if err != nil {
		t.Fatalf("Hover: %v", err)
	}
	if hover != nil {
		t.Errorf("expected nil hover for out-of-range position, got: %+v", hover)
	}
}

func TestHover_ClosedDoc_ReturnsNil(t *testing.T) {
	h := servertest.New(t, newHandler())
	// Hover on a URI that was never opened — should return nil gracefully.
	hover, err := h.Hover("file:///never-opened.lyra", 0, 0)
	if err != nil {
		t.Fatalf("Hover: %v", err)
	}
	if hover != nil {
		t.Errorf("expected nil hover for unknown URI, got: %+v", hover)
	}
}

// Hover on a written type name — the other half of the same gap go-to-definition had, and
// through the same table. It shows what kind of declaration the name refers to and its
// documentation, which is what a reader hovering a name in a signature is asking.
func TestHover_TypeInATypePosition(t *testing.T) {
	for _, tc := range []struct {
		name, needle, wantCode, wantDoc string
	}{
		{"struct", "Point,", "struct Point", "A point in the plane."},
		{"alias", "Coord,", "type Coord = i64", "A column index."},
		{"newtype", "Cents,", "newtype Cents = i64", "Money, in cents."},
		{"data", "Shape)", "data Shape", "A shape, one way or another."},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := servertest.New(t, newHandler())
			src := `
/// A point in the plane.
struct Point { x: i64, y: i64 }
/// A column index.
type Coord = i64
/// Money, in cents.
newtype Cents = i64
/// A shape, one way or another.
data Shape = Dot | Box i64
let f = pure (p: Point, c: Coord, m: Cents, s: Shape) -> i64 => 0`
			openAndWait(t, h, src)
			col := strings.Index(strings.Split(src, "\n")[9], tc.needle)
			hover, err := h.Hover(testURI, 9, col)
			if err != nil {
				t.Fatalf("Hover: %v", err)
			}
			if hover == nil {
				t.Fatal("no hover on a written type name")
			}
			if !strings.Contains(hover.Contents.Value, tc.wantCode) {
				t.Errorf("hover = %q; want it to contain %q", hover.Contents.Value, tc.wantCode)
			}
			// The doc is the half that says what the type is *for*, and it is the reason
			// a signature's type name is worth hovering at all.
			if !strings.Contains(hover.Contents.Value, tc.wantDoc) {
				t.Errorf("hover = %q; want the declaration's doc comment", hover.Contents.Value)
			}
		})
	}
}

// Hovering a trait name shows the trait, its supertraits and its documentation. The
// supertraits are part of the summary because `trait B: A` makes two claims at once —
// every implementer of B implements A, and a `where t: B` bound reaches A's methods — so
// hiding the `: A` would hide half of what the name means.
func TestHover_TraitNameInABoundPosition(t *testing.T) {
	h := servertest.New(t, newHandler())
	src := `
/// Anything that can render itself.
trait Shown { pure show: (Self) -> string }
/// Shown, and more.
trait Sub: Shown { }
let a<t> where t: Sub = pure (v: t) -> string => v.show()`
	openAndWait(t, h, src)
	col := strings.Index(strings.Split(src, "\n")[5], "Sub =")
	hover, err := h.Hover(testURI, 5, col)
	if err != nil {
		t.Fatalf("Hover: %v", err)
	}
	if hover == nil {
		t.Fatal("no hover on a trait name in a where bound")
	}
	if !strings.Contains(hover.Contents.Value, "trait Sub: Shown") {
		t.Errorf("hover = %q; want it to show the trait and its supertrait", hover.Contents.Value)
	}
	if !strings.Contains(hover.Contents.Value, "Shown, and more.") {
		t.Errorf("hover = %q; want the trait's doc comment", hover.Contents.Value)
	}
}

// Hovering a constructor in a pattern shows which data type it belongs to, and that type's
// documentation — the same blindness definition had, in the feature next door.
func TestHover_ConstructorInAPattern(t *testing.T) {
	h := servertest.New(t, newHandler())
	src := `
/// A shape, of which there are two.
data Shape = Dot | Box(i64)
let area = pure (s: Shape) -> i64 => match s {
	Dot => 0,
	Box(n) => n,
}`
	openAndWait(t, h, src)
	col := strings.Index(strings.Split(src, "\n")[4], "Dot =>")
	hover, err := h.Hover(testURI, 4, col)
	if err != nil {
		t.Fatalf("Hover: %v", err)
	}
	if hover == nil {
		t.Fatal("no hover on a constructor in a pattern")
	}
	if !strings.Contains(hover.Contents.Value, "Dot: Shape") {
		t.Errorf("hover = %q; want it to name the constructor and its type", hover.Contents.Value)
	}
	if !strings.Contains(hover.Contents.Value, "A shape, of which there are two.") {
		t.Errorf("hover = %q; want the data type's doc comment", hover.Contents.Value)
	}
}
