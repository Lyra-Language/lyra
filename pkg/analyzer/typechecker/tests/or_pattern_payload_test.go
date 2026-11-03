package typechecker_test

import "testing"

// A name written as a whole alternative is no alternative at all: `x | 1` is the bitwise
// expression's spelling everywhere a `(` leads, so it does not parse.
func TestOrPattern_ABareNameIsNoAlternative(t *testing.T) {
	res := parseCollectAndCheckAllowingSyntaxErrors(t, `let f = (n: i64) -> i64 => match n { 1 | x => 1 }`, false)
	if len(res.errors) == 0 {
		t.Error("expected a diagnostic")
	}
}

// **A data match covers what its alternatives cover, at any depth**: the matrix gets one row per
// alternative, and specializing is what brings a nested alternation to the head of a column.
func TestOrPattern_Exhaustiveness(t *testing.T) {
	const decls = `data Opt<t> = Yes(t) | No
data Tool = Pencil | Eraser(bool) | Brush(bool, bool)
`
	covers := []struct{ name, src string }{
		{"both constructors", `let f = (m: Opt<i64>) -> i64 => match m { Yes(_) | No => 1 }`},
		{"a nested alternation", `let f = (m: Opt<Opt<i64>>) -> i64 => match m { Yes(Yes(_) | No) => 1, No => 2 }`},
		{"a bool payload", `let f = (m: Opt<bool>) -> i64 => match m { Yes(true | false) | No => 1 }`},
		{"spread over fields", `let f = (t: Tool) -> i64 => match t { Pencil | Eraser(_) => 1, Brush(true | false, true) => 2, Brush(_, false) => 3 }`},
		{"in a tuple's columns", `let f = (q: (bool, bool)) -> i64 => match q { (true | false, true) | (_, false) => 1 }`},
		{"a bool in a tuple", `let f = (q: (bool, Opt<i64>)) -> i64 => match q { (true, Yes(_) | No) => 1, (false, _) => 2 }`},
	}
	for _, c := range covers {
		t.Run(c.name, func(t *testing.T) {
			assertNoErrors(t, parseCollectAndCheck(t, decls+c.src, false))
		})
	}
	gaps := []struct{ name, src, want string }{
		{"a tested payload", `let f = (m: Opt<i64>) -> i64 => match m { Yes(1) | No => 1 }`, "not every payload of Yes"},
		{"an alternation of literals", `let f = (m: Opt<i64>) -> i64 => match m { Yes(1 | 2) => 1, No => 0 }`, "not every payload of Yes"},
		{"a missing constructor", `let f = (t: Tool) -> i64 => match t { Pencil | Eraser(_) => 1 }`, "Brush"},
		{"half a bool", `let f = (m: Opt<bool>) -> i64 => match m { Yes(true) | No => 1 }`, "not every payload of Yes"},
		{"a column left open", `let f = (q: (bool, bool)) -> i64 => match q { (true | false, true) => 1 }`, "not exhaustive"},
	}
	for _, c := range gaps {
		t.Run(c.name, func(t *testing.T) {
			assertHasErrorContaining(t, parseCollectAndCheck(t, decls+c.src, false), c.want)
		})
	}
}

// Each alternative is held to the rule of the type in its position, and reported where written.
func TestOrPattern_AlternativesAreCheckedAgainstTheirType(t *testing.T) {
	const decls = `data Opt = Yes(u8) | No
data Other = Elsewhere
`
	cases := []struct{ name, src, want string }{
		{"a kind", `let f = (m: Opt) -> i64 => match m { Yes(1 | "a") => 1, _ => 0 }`, `literal pattern '"a"' is not an integer type`},
		{"a width", `let f = (m: Opt) -> i64 => match m { Yes(1 | 300) => 1, _ => 0 }`, "300 does not fit"},
		{"another type's constructor", `let f = (m: Opt) -> i64 => match m { Yes(_) | Elsewhere => 1, _ => 0 }`, "Elsewhere is not a constructor of Opt"},
		{"a literal on a data scrutinee", `let f = (m: Opt) -> i64 => match m { Yes 1 | 2 => 1, _ => 0 }`, "literal patterns are not allowed"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assertHasErrorContaining(t, parseCollectAndCheck(t, decls+c.src, false), c.want)
		})
	}
}

// A plain `let` takes a pattern that cannot fail. An alternation that is only sure as a whole is
// still refused: a `let` that binds nothing has no use for the proof.
func TestOrPattern_APlainLetStaysRefutable(t *testing.T) {
	res := parseCollectAndCheck(t, `data Opt = Yes(i64) | No
let f = (m: Opt) -> i64 => {
  let Yes(_) | No = m
  1
}`, false)
	assertHasErrorContaining(t, res, "can fail to match")
}

// lyra-W021 reads through the wider alternatives as it did through the nullary ones.
func TestOrPattern_Reachability(t *testing.T) {
	const decls = "data Opt = Yes(i64) | No\n"
	t.Run("an arm covered by an alternative", func(t *testing.T) {
		res := parseCollectAndCheck(t, decls+`let f = (m: Opt) -> i64 => match m {
  Yes(_) | No => 1,
  Yes(2) => 2,
}`, false)
		assertHasErrorContaining(t, res, "unreachable match arm")
	})
	t.Run("an alternation covered only in part", func(t *testing.T) {
		res := parseCollectAndCheck(t, decls+`let f = (m: Opt) -> i64 => match m {
  No => 1,
  Yes(_) | No => 2,
}`, false)
		assertNoErrors(t, res)
	})
	t.Run("a payload test covers nothing", func(t *testing.T) {
		res := parseCollectAndCheck(t, decls+`let f = (m: Opt) -> i64 => match m {
  Yes(1 | 2) | No => 1,
  Yes(_) => 2,
}`, false)
		assertNoErrors(t, res)
	})
}
