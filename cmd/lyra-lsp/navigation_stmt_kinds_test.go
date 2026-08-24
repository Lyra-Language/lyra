package main

import (
	"testing"

	"github.com/owenrumney/go-lsp/servertest"
)

// **Navigation inside a trait impl, and in the seven other statement kinds the position
// lookup could not reach.**
//
// `findExprAtPos` was three hand-written switches mirroring pkg/ast's canonical walkers.
// The *expression* half was registered in `pkg/ast/exhaustive_test.go` and so was kept in
// step; the *statement* half never was, and had fallen eight kinds behind — `WithStmt`,
// `IfDestructuringStmt`, `ElseDestructuringStmt`, `LValueAssignmentStmt`, `BreakStmt`,
// `DestructuringDeclStmt`, `TraitDeclStmt` and `TraitImplStmt`.
//
// The last two are the expensive ones: a trait impl's methods are top-level statements, so
// hover, go-to-definition, references, rename and document-highlight all returned nothing
// anywhere inside an `impl` body or a trait's default method — every operator overload and
// every `Show` implementation in every program. The symptom of a missing case in a position
// lookup is an editor doing nothing, which reads as "unsupported" rather than as a bug.
//
// It walks through `ast.WalkStmt` now, so there is no statement-kind list left to fall
// behind. These are the regressions that would notice if one came back.
func TestNavigation_InsideATraitImplMethod(t *testing.T) {
	h := servertest.New(t, newHandler())
	src := `
let helper = pure (n: i64) -> i64 => n
type Counter = struct { n: i64 }
trait Bump { pure bump: (Self) -> i64 }
impl Bump for Counter {
  bump = pure (self) => helper(self.n)
}`
	openAndWait(t, h, src)

	// `helper`, called from inside the impl method's body.
	locs, err := h.Definition(testURI, 5, 25)
	if err != nil {
		t.Fatalf("Definition: %v", err)
	}
	if len(locs) != 1 || locs[0].Range.Start.Line != 1 {
		t.Errorf("definition inside an impl method = %v; want one location on line 1", locs)
	}
}

// A trait's **default method** body is the other half of the same gap: it hangs off a
// TraitDeclStmt, which the statement switch also had no case for.
func TestNavigation_InsideATraitDefaultMethod(t *testing.T) {
	h := servertest.New(t, newHandler())
	src := `
let helper = pure (n: i64) -> i64 => n
trait Named {
  pure size: (Self) -> i64
  pure doubled: (Self) -> i64 = pure (self) => helper(self.size())
}`
	openAndWait(t, h, src)

	locs, err := h.Definition(testURI, 4, 48)
	if err != nil {
		t.Fatalf("Definition: %v", err)
	}
	if len(locs) != 1 || locs[0].Range.Start.Line != 1 {
		t.Errorf("definition inside a trait default method = %v; want one location on line 1", locs)
	}
}

// `p.x = v` is a LValueAssignmentStmt, and `let (a, b) = …` a DestructuringDeclStmt —
// two more the statement switch skipped, so an assignment target and a destructured
// initializer were both unnavigable.
func TestNavigation_InLValueAssignmentAndDestructuring(t *testing.T) {
	h := servertest.New(t, newHandler())
	src := `
let helper = pure (n: i64) -> i64 => n
type Point = struct { x: i64, y: i64 }
let main = () -> void => {
  var p = Point { x: 1, y: 2 }
  p.x = helper(3)
  let (a, b) = (helper(4), 5)
  println(a + b + p.y)
}`
	openAndWait(t, h, src)

	// `helper` on the right of an lvalue assignment.
	locs, err := h.Definition(testURI, 5, 9)
	if err != nil {
		t.Fatalf("Definition: %v", err)
	}
	if len(locs) != 1 || locs[0].Range.Start.Line != 1 {
		t.Errorf("definition in an lvalue assignment = %v; want one location on line 1", locs)
	}

	// `helper` inside a destructuring declaration's initializer.
	locs, err = h.Definition(testURI, 6, 17)
	if err != nil {
		t.Fatalf("Definition: %v", err)
	}
	if len(locs) != 1 || locs[0].Range.Start.Line != 1 {
		t.Errorf("definition in a destructuring decl = %v; want one location on line 1", locs)
	}
}
