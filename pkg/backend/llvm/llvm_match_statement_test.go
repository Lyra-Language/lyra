package llvm

import (
	"strings"
	"testing"
)

// A `match` used as a **statement** — for its effect, with arms that end in an
// assignment rather than an expression.
//
// This did not lower until 08/06: the four arm-body sites lowered through
// `lowerExpr`, which routes a block body to `lowerBlock` and *requires* a value,
// so `match m { Some v => { x = v; }, None => { x = 0; } }` failed with "block has
// no value (empty, or last statement is not an expression)". `if` never had the
// problem because its branches go through `lowerBranchValue`, which is
// value-optional; the arms now use the same helper.
//
// It matters more than the syntax suggests: a `match` for effect is ordinary
// imperative code, and without it every statement-position match had to be
// rewritten as an `if`/`else` chain — backwards for a language whose sum types are
// its main idiom.

// One case per scrutinee shape, because the arm body is lowered at four separate
// sites: the shared ladder (scalar, struct, tuple), the `data` tag switch, and two
// helpers in the array match. Fixing a subset would leave the rest failing on
// exactly the same source with a different scrutinee, which is the remote-symptom
// shape hazard 8 is about.
func TestExec_MatchAsStatement(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, body string
		want       int
	}{
		{
			"data scrutinee (tag switch)",
			`let m = Some 7
  match m { Some v => { x = v; }, None => { x = 0; } }`,
			7,
		},
		{
			"integer scrutinee (ladder)",
			`let n = 2
  match n { 1 => { x = 10; }, 2 => { x = 20; }, _ => { x = 30; } }`,
			20,
		},
		{
			"string scrutinee",
			`let s = "b"
  match s { "a" => { x = 1; }, _ => { x = 2; } }`,
			2,
		},
		{
			"bool scrutinee",
			`let b = false
  match b { true => { x = 1; }, false => { x = 9; } }`,
			9,
		},
		{
			"struct scrutinee",
			`let p = Pt { a: 3, b: 4 }
  match p { Pt { a: 3, b } => { x = b; }, _ => { x = 0; } }`,
			4,
		},
		{
			"tuple scrutinee",
			`let tp = (1, 5)
  match tp { (1, b) => { x = b; }, _ => { x = 0; } }`,
			5,
		},
		{
			"dynamic array scrutinee",
			`let xs: []i64 = [1, 2, 3]
  match xs { [a, b, c] => { x = a + b + c; }, _ => { x = 0; } }`,
			6,
		},
		// A guarded arm goes through lowerGuardedArmBody, a different path into the
		// same arm-body helper.
		{
			"guarded arm",
			`let n = 7
  match n { v if v > 5 => { x = 100; }, _ => { x = 1; } }`,
			100,
		},
		// Arms need not agree on having a value: the match is a statement, so an arm
		// that happens to end in an expression is simply discarded rather than being
		// phi'd against the void one.
		{
			"one arm ends in an expression, the other in an assignment",
			`let m = Some 1
  match m { Some _ => { x = 42; }, None => 0 }`,
			42,
		},
		// A diverging arm contributes no edge to the merge at all, which is the case
		// the void bookkeeping must not confuse with "reached with no value".
		{
			"diverging arm beside a statement arm",
			`let m = Some 8
  match m { Some v => { x = v; }, None => panic("unreachable") }`,
			8,
		},
		{
			"a statement match nested inside a match arm",
			`let m = Some 5
  match m { Some v => { match v { 5 => { x = 55; }, _ => { x = 0; } } }, None => { x = 0; } }`,
			55,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			// This harness is driver.Analyze — a single unit with no prelude — so the
			// canonical Maybe is declared here. Its shape and name are what stamp it
			// (collector/canonical.go), so `Some`/`None` mean what they usually mean.
			src := "data Maybe<t> = None | Some t\nstruct Pt { a: i64, b: i64 }\n" +
				"let main = () -> u8 => {\n  var x = 0\n  " + c.body + "\n  u8(x)\n}\n"
			if got := buildAndRun(t, src); got != c.want {
				t.Errorf("got %d, want %d\nsource:\n%s", got, c.want, src)
			}
		})
	}
}

// A `match` in *value* position still produces its value — the change must not
// have made every match void.
func TestExec_MatchAsValueStillWorks(t *testing.T) {
	t.Parallel()
	const src = `
data Maybe<t> = None | Some t
let main = () -> u8 => {
  let m = Some 5
  let a = match m { Some v => v * 2, None => 0 }
  let n = 3
  let b = match n { 1 => 1, 3 => 7, _ => 0 }
  u8(a + b)
}
`
	if got := buildAndRun(t, src); got != 17 {
		t.Errorf("got %d, want 17 (10 + 7)", got)
	}
}

// Binding a **void** expression is refused with a diagnostic naming the binding,
// rather than crashing the compiler.
//
// `lowerVarDecl` had a guard for a *diverging* initializer (`let x = panic("…")`)
// but not for a void one, and the two are different: diverging means control never
// reaches the store, while void means it does reach it with nothing to store. So
// `init.Type()` dereferenced a nil and **segfaulted the compiler** — which is the
// "a well-typed program must never panic the backend" invariant, not a missing
// feature. The `if` form of this was already broken before `match` arms could be
// statements at all; both are covered here because they are one defect.
//
// The typechecker does not reject binding a void expression today (it only warns
// that the binding is unused), so the backend is where this has to be caught.
func TestEmit_BindingAVoidExpressionErrors(t *testing.T) {
	t.Parallel()
	cases := []struct{ name, src string }{
		{"void match", `
data Maybe<t> = None | Some t
let main = () -> u8 => {
  var x = 0
  let m = Some 1
  let r = match m { Some v => { x = v; }, None => { x = 0; } }
  u8(x)
}
`},
		{"void if", `
let main = () -> u8 => {
  var x = 0
  let r = if x == 0 { x = 1; } else { x = 2; }
  u8(x)
}
`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			_, err := emitSource(t, c.src)
			if err == nil {
				t.Fatal("expected an error binding a void expression, got none")
			}
			if !strings.Contains(err.Error(), "produces no value") {
				t.Errorf("error should explain the binding has no value, got: %v", err)
			}
			if !strings.Contains(err.Error(), `"r"`) {
				t.Errorf("error should name the binding, got: %v", err)
			}
		})
	}
}

// A **bare jump** as an arm body — `None => break`, `_ => continue`,
// `v if … => return v`.
//
// The jump forms are statements and an arm body is an expression, so the bare
// spelling parsed `break` as an identifier ("undefined identifier \"break\"") until
// 08/06. The *braced* form already worked end to end, so the collector erases the
// bare one into exactly that block — which is why these are behavioural tests of a
// feature with no backend change behind it. They exist because the erasure is only
// worth anything if the erased form really does run.
func TestExec_BareJumpInMatchArm(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, src string
		want      int
	}{
		{
			// break out of a loop from an arm: sums 1+2+3 and stops at the None.
			"break",
			`
data Maybe<t> = None | Some t
let main = () -> u8 => {
  var n = 0
  var total = 0
  for {
    n = n + 1
    let m: Maybe<i64> = if n < 4 { Some n } else { None }
    match m {
      Some v => { total = total + v; },
      None => break,
    }
  }
  u8(total)
}
`, 6,
		},
		{
			// continue from an arm: sums the odd numbers 1..9.
			"continue",
			`
let main = () -> u8 => {
  var total = 0
  for var i = 1; i <= 9; i += 1 {
    match i %% 2 {
      0 => continue,
      _ => { total = total + i; },
    }
  }
  u8(total)
}
`, 25,
		},
		{
			// return from an arm, out of the enclosing function.
			"return with a value",
			`
let firstBig = (xs: []i64) -> i64 => {
  for x in xs {
    match x {
      v if v > 10 => return v,
      _ => { },
    }
  }
  -1
}
let main = () -> u8 => u8(firstBig([1, 5, 42, 7]))
`, 42,
		},
		{
			// A bare `return` with no value, from a void function.
			"bare return",
			`
data Maybe<t> = None | Some t
var seen = 0
let main = () -> u8 => {
  var n = 0
  let step = (m: Maybe<i64>) -> void => {
    match m {
      None => return,
      Some _ => { },
    }
  }
  step(None)
  u8(n)
}
`, 0,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if got := buildAndRun(t, c.src); got != c.want {
				t.Errorf("got %d, want %d", got, c.want)
			}
		})
	}
}

// The bare and braced spellings must behave identically — the collector claims they
// are the same AST, and this is the end-to-end half of that claim (the collector
// tests pin the ASTs themselves).
func TestExec_BareAndBracedJumpAgree(t *testing.T) {
	t.Parallel()
	const tmpl = `
data Maybe<t> = None | Some t
let main = () -> u8 => {
  var n = 0
  var total = 0
  for {
    n = n + 1
    let m: Maybe<i64> = if n < 5 { Some n } else { None }
    match m {
      Some v => { total = total + v; },
      None => %s,
    }
  }
  u8(total)
}
`
	bare := buildAndRun(t, strings.Replace(tmpl, "%s", "break", 1))
	braced := buildAndRun(t, strings.Replace(tmpl, "%s", "{ break; }", 1))
	if bare != braced {
		t.Errorf("bare break gave %d, braced gave %d", bare, braced)
	}
	if bare != 10 { // 1+2+3+4
		t.Errorf("got %d, want 10", bare)
	}
}
