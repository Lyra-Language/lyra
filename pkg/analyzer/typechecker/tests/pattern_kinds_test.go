package typechecker_test

import (
	"strings"
	"testing"
)

// **A nested pattern is checked against the type in its position**, in every position a
// pattern is written. A match arm's per-kind check is one level deep and the destructuring
// walk's errors were discarded behind it, while a `let`, a `let … else` and a parameter never
// looked at a literal, range or regex at all — so each of these type-checked clean and failed
// in the backend, or was not refused at all (09/13).
func TestPatternKinds_NestedLeavesAreChecked(t *testing.T) {
	const decls = `
data Opt = Has(string) | Gone
struct Pt { name: string, n: i64 }
`
	for _, c := range []struct{ name, body, want string }{
		{"literal in a payload", `match Has("a") { Has(5) => 1, _ => 0 }`, "literal pattern '5' is not a string type"},
		{"literal in a struct field", `match Pt { name: "a", n: 1 } { Pt { name: 5, n } => n, _ => 0 }`, "literal pattern '5' is not a string type"},
		{"range in a tuple", `match (1, "a") { (x, 0..<3) => x, _ => 0 }`, "range patterns are not allowed on string scrutinees"},
		{"range in an array", `{ let xs: []string = ["a"]; match xs { [0..<3] => 1, _ => 0 } }`, "range patterns are not allowed on string scrutinees"},
		{"regex that cannot be tabled", `match Has("a") { Has(r"(?<=a)b") => 1, _ => 0 }`, "cannot be compiled to a runtime matcher"},
		{"literal in let-else", `{ let (x, 5) = (1, "s") else { return 0 }; x }`, "literal pattern '5' is not a string type"},
		{"literal against a struct", `{ let (a, 5) = (1, Pt { name: "x", n: 1 }) else { return 0 }; a }`, "pattern 5 cannot match a value of type Pt"},
	} {
		t.Run(c.name, func(t *testing.T) {
			res := parseCollectAndCheck(t, decls+"let f = () -> i64 => "+c.body, false)
			found := false
			for _, e := range res.errors {
				found = found || strings.Contains(e.Message, c.want)
			}
			if !found {
				t.Errorf("want an error containing %q, got %v", c.want, res.errors)
			}
		})
	}
}

// A capitalized name in a match arm is a value the author meant to compare against, so the
// advice is a guard — not `const`, which they have already written.
func TestPatternKinds_ConstantInAMatchArm(t *testing.T) {
	res := parseCollectAndCheck(t, `
const LIMIT = 5
let f = (x: i64) -> i64 => match x { LIMIT => 1, _ => 0 }
`, false)
	found := false
	for _, e := range res.errors {
		found = found || strings.Contains(e.Message, "test it in a guard, as `v if v == LIMIT`")
	}
	if !found {
		t.Errorf("want the guard advice, got %v", res.errors)
	}
}

// **A tuple rest binds the tuple of the positions it covers**, and the positions after it
// count from the end — in a `let`, a match arm and a constructor payload alike.
func TestPatternKinds_TupleRestBindsTheCoveredPositions(t *testing.T) {
	res := parseCollectAndCheck(t, `
data Shape = Tri(i64, string, bool) | Dot
let f = () -> i64 => {
  let (a, ...mid, z) = (1, "s", true, 2.5)
  let m: (string, bool) = mid
  let w: f64 = z
  let n = match (1, "s", true) { (x, ...r) => { let q: (string, bool) = r; x }, }
  let k = match Tri(1, "t", false) { Tri(i, ...more) => { let q: (string, bool) = more; i }, Dot => 0 }
  if m.1 && w > 0.0 { a + n + k } else { 0 }
}
`, false)
	assertNoErrors(t, res)
}

func TestPatternKinds_TupleRestArity(t *testing.T) {
	res := parseCollectAndCheck(t, `let f = () -> i64 => { let (a, b, ...r, z) = (1, 2); a }`, false)
	found := false
	for _, e := range res.errors {
		found = found || e.Message == "tuple pattern needs at least 3 element(s) but tuple has 2"
	}
	if !found {
		t.Errorf("want the at-least arity error, got %v", res.errors)
	}
}
