package typechecker_test

import (
	"cmp"
	"fmt"
	"slices"
	"testing"

	"github.com/Lyra-Language/lyra/pkg/ast"
)

// A `let`/`var`/`for` binding shadows a parameter or pattern binding of the same name from
// its declaration on. Resolution consulted the parameter first, so the shadow was ignored
// whenever its type differed — and passed by accident when it did not, which is all
// TestTypeCheck_ParamShadowing_Ok exercised. Each test here shadows with a *different*
// type, and pins what every use resolved to, so a use reading the wrong binding is a
// visible failure rather than a coincidence of types.

const shadowTypes = `
struct Box { side: i64 }
struct Pt { x: i64 }
data Opt = Has(i64) | Nothing
`

// identTypes lists the recorded type of every use of name, in source order.
func identTypes(t *testing.T, res checkResult, name string) []string {
	t.Helper()
	var uses []*ast.IdentifierExpr
	for _, node := range res.program.Statements {
		stmt, ok := node.(ast.Statement)
		if !ok {
			continue
		}
		ast.WalkStmt(stmt, nil, func(e ast.Expression) bool {
			if id, ok := e.(*ast.IdentifierExpr); ok && id.Name == name {
				uses = append(uses, id)
			}
			return true
		})
	}
	slices.SortFunc(uses, func(a, b *ast.IdentifierExpr) int {
		la, lb := a.GetLocation(), b.GetLocation()
		return cmp.Or(cmp.Compare(la.StartLine, lb.StartLine), cmp.Compare(la.StartCol, lb.StartCol))
	})
	out := make([]string, len(uses))
	for i, id := range uses {
		if typ, ok := res.typeTable.Get(id); ok && typ != nil {
			out[i] = fmt.Sprint(typ)
		} else {
			out[i] = "<none>"
		}
	}
	return out
}

func assertIdentTypes(t *testing.T, res checkResult, name string, want ...string) {
	t.Helper()
	if got := identTypes(t, res, name); !slices.Equal(got, want) {
		t.Errorf("uses of %q resolved to %v, want %v", name, got, want)
	}
}

func TestParamShadowing_LetOfAnotherType(t *testing.T) {
	res := parseCollectAndCheck(t, shadowTypes+`
let g = pure (c: Box) -> i64 => {
  let c = c.side + 1
  c * 2
}`, false)
	assertNoErrors(t, res)
	// The initializer's `c` is still the parameter; the one after the declaration is the let.
	assertIdentTypes(t, res, "c", "Box", "i64")
}

func TestParamShadowing_VarOfAnotherType(t *testing.T) {
	res := parseCollectAndCheck(t, shadowTypes+`
let g = (c: Box) -> i64 => {
  var c = c.side
  c += 1
  c = c * 2
  c
}`, false)
	// No lyra-E025: the reassignments write the local var, not the borrowed parameter.
	assertNoErrors(t, res)
	assertIdentTypes(t, res, "c", "Box", "i64", "i64", "i64")
}

func TestParamShadowing_ForInLoopVariable(t *testing.T) {
	res := parseCollectAndCheck(t, shadowTypes+`
let f = pure (c: Box, items: []Pt) -> i64 => {
  var sum = 0
  for c in items { sum += c.x }
  sum + c.side
}`, false)
	assertNoErrors(t, res)
	// The loop variable inside the body; the parameter again after the loop.
	assertIdentTypes(t, res, "c", "Pt", "Box")
}

func TestParamShadowing_ForInIterableIsTheParameter(t *testing.T) {
	// The iterable is outside the loop variable's scope, so it names the parameter.
	res := parseCollectAndCheck(t, shadowTypes+`
let f = pure (c: []Pt) -> i64 => {
  var sum = 0
  for c in c { sum += c.x }
  sum
}`, false)
	assertNoErrors(t, res)
	assertIdentTypes(t, res, "c", "DynamicArray<Pt>", "Pt")
}

func TestParamShadowing_NestedBlockEndsTheShadow(t *testing.T) {
	res := parseCollectAndCheck(t, shadowTypes+`
let f = pure (c: Box) -> i64 => {
  var total = 0
  if c.side > 0 {
    let c = "inner"
    total += 1
  }
  {
    let c = c.side * 10
    total += c
  }
  total + c.side
}`, false)
	assertNoErrors(t, res)
	assertIdentTypes(t, res, "c", "Box", "Box", "i64", "Box")
}

func TestParamShadowing_UseBeforeTheShadowingLetIsTheParameter(t *testing.T) {
	// The collector has already put the later `let c` in the block's scope when the first
	// use is checked; it must not be read before it is declared.
	res := parseCollectAndCheck(t, shadowTypes+`
let f = pure (c: Box) -> string => {
  let n = c.side
  let c = "n=${n}"
  c
}`, false)
	assertNoErrors(t, res)
	assertIdentTypes(t, res, "c", "Box", "string")
}

func TestParamShadowing_ClosureSeesTheShadow(t *testing.T) {
	res := parseCollectAndCheck(t, shadowTypes+`
let f = (c: Box) -> i64 => {
  let c = c.side
  let k = () -> i64 => c * 2
  k()
}`, false)
	assertNoErrors(t, res)
	assertIdentTypes(t, res, "c", "Box", "i64")
}

func TestParamShadowing_ClosureParameterBeatsAnOuterLet(t *testing.T) {
	res := parseCollectAndCheck(t, shadowTypes+`
let f = (c: Box) -> i64 => {
  let c = c.side
  let k = (c: Box) -> i64 => c.side + 1
  k(Box { side: c })
}`, false)
	assertNoErrors(t, res)
	assertIdentTypes(t, res, "c", "Box", "Box", "i64")
}

func TestParamShadowing_LetInsideAClosureShadowsItsParameter(t *testing.T) {
	res := parseCollectAndCheck(t, shadowTypes+`
let f = (b: Box) -> i64 => {
  let k = (c: Box) -> i64 => {
    let c = c.side
    c + 1
  }
  k(b)
}`, false)
	assertNoErrors(t, res)
	assertIdentTypes(t, res, "c", "Box", "i64")
}

func TestParamShadowing_LetShadowsAPatternBinding(t *testing.T) {
	res := parseCollectAndCheck(t, shadowTypes+`
let f = pure (o: Opt) -> string => match o {
  Has(c) => {
    let c = "has ${c}"
    c
  },
  Nothing => "nothing",
}`, false)
	assertNoErrors(t, res)
	assertIdentTypes(t, res, "c", "i64", "string")
}

func TestParamShadowing_PatternBindingBeatsAnEarlierLet(t *testing.T) {
	// The `let x` is in the enclosing block, before the match: the arm's own `x` wins,
	// whether the arm body is a bare expression or a block.
	res := parseCollectAndCheck(t, shadowTypes+`
let f = pure (o: Opt) -> i64 => {
  let x = "outer"
  let a = match o { Has(x) => x + 1, Nothing => 0 }
  let b = match o { Has(x) => { x + 2 }, Nothing => 0 }
  a + b
}`, false)
	assertNoErrors(t, res)
	assertIdentTypes(t, res, "x", "i64", "i64")
}

func TestParamShadowing_InteriorMutationFollowsTheShadow(t *testing.T) {
	// A `var` shadowing a borrowed parameter may be written through; the parameter could
	// not (lyra-E025's interior twin).
	res := parseCollectAndCheck(t, shadowTypes+`
let f = (c: Box) -> i64 => {
  var c = Box { side: c.side }
  c.side = 5
  c.side
}`, false)
	assertNoErrors(t, res)

	// And a `let` shadowing a `mut` parameter is a let: deeply immutable.
	res = parseCollectAndCheck(t, shadowTypes+`
let f = (c: mut Box) -> i64 => {
  let c = Box { side: 1 }
  c.side = 5
  c.side
}`, false)
	assertHasErrorContaining(t, res, "let")
}

func TestParamShadowing_ReassignmentFollowsTheShadow(t *testing.T) {
	// Before the `var`, `n = …` is a write to a borrowed parameter (lyra-E025); after it,
	// an ordinary write to the var. (The target is a name, not an expression, so the three
	// typed uses are the reads.)
	res := parseCollectAndCheck(t, `
let f = (n: i64) -> i64 => {
  var n = n * 10
  n = n + 1
  n
}`, false)
	assertNoErrors(t, res)
	assertIdentTypes(t, res, "n", "i64", "i64", "i64")

	res = parseCollectAndCheck(t, `
let f = (n: i64) -> i64 => {
  n = 1
  var n = 2
  n
}`, false)
	assertHasErrorContaining(t, res, "cannot reassign a borrowed parameter")
}

func TestSequentialRebinding_UseBetweenTwoBindings(t *testing.T) {
	// The scope holds only the latest of a sequentially rebound name, so a use between the
	// two declarations resolved to the second — untyped at that point, which the backend
	// then failed on. It means the first.
	res := parseCollectAndCheck(t, `
let f = pure (n: i64) -> string => {
  let x = n + 1
  let y = x * 2
  let x = "s${y}"
  x
}`, false)
	assertNoErrors(t, res)
	assertIdentTypes(t, res, "x", "i64", "string")
}

func TestSequentialRebinding_SelfReferenceInANestedScopeReadsTheOuterBinding(t *testing.T) {
	res := parseCollectAndCheck(t, shadowTypes+`
let f = pure (b: Box) -> i64 => {
  let x = b
  {
    let x = x.side + 1
    x * 2
  }
}`, false)
	assertNoErrors(t, res)
	assertIdentTypes(t, res, "x", "Box", "i64")
}

func TestShadowing_UseBeforeALocalReadsTheModuleBinding(t *testing.T) {
	// The block's own `let total` is not in effect yet, and nothing in the function binds
	// the name before it, so the first use is the top-level `total`.
	res := parseCollectAndCheck(t, shadowTypes+`
let total = Box { side: 7 }
let f = pure () -> i64 => {
  let a = total.side
  let total = 5
  a + total
}`, false)
	assertNoErrors(t, res)
	assertIdentTypes(t, res, "total", "Box", "i64")
}
