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
