package typechecker_test

import (
	"strings"
	"testing"
)

// A block whose value is used and whose last statement is not an expression: an
// `if let … else …` there is the block's value (checked as the `match` it means), and any
// other statement is no value — refused here, where until 09/28 it type-checked as
// "unknown" and the backend refused it ("block has no value").

const tailTypes = `
  data Opt = Val(i64) | Nothing
`

func TestBlockTail_IfLetElseIsTheValue(t *testing.T) {
	res := parseCollectAndCheck(t, tailTypes+`
  let body = (m: Opt) -> i64 => {
    if let Val(v) = m { v + 1 } else { 0 }
  }
  let branch = (c: bool, m: Opt) -> i64 => if c { if let Val(v) = m { v } else { 1 } } else { 2 }
  let chained = (a: Opt, b: Opt) -> i64 => {
    if let Val(x) = a { x } else { if let Val(y) = b { y } else { 0 } }
  }
	`, false)
	assertNoErrors(t, res)
}

// Its arms must agree, as any value's branches must.
func TestBlockTail_IfLetArmsMustAgree(t *testing.T) {
	res := parseCollectAndCheck(t, tailTypes+`
  let body = (m: Opt) -> i64 => {
    if let Val(v) = m { v } else { "none" }
  }
	`, false)
	if len(res.errors) == 0 {
		t.Fatal("want the arms' disagreement reported")
	}
}

func TestBlockTail_StatementTailIsNoValue(t *testing.T) {
	cases := []struct{ name, body, want string }{
		{"declaration", `{ let x = 1 }`, "the body ends in a declaration"},
		{"let-else", `{ let Val(v) = m else { return 0 } }`, "the body ends in a `let … else`"},
		{"if let without else", `{ if let Val(v) = m { v } }`, "with an `else`, an `if let` ending the body is its value"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			res := parseCollectAndCheck(t, tailTypes+`
  let f = (m: Opt) -> i64 => `+c.body+`
			`, false)
			if len(res.errors) != 1 || !strings.Contains(res.errors[0].Message, c.want) ||
				!strings.Contains(res.errors[0].Message, "reaches its end without a value") {
				t.Errorf("want one error containing %q; got %v", c.want, res.errors)
			}
		})
	}
}

// A tail that cannot fall through needs no value: a `return`, or an `if let` whose both
// branches leave.
func TestBlockTail_DivergingTailIsFine(t *testing.T) {
	res := parseCollectAndCheck(t, tailTypes+`
  let a = (m: Opt) -> i64 => {
    let x = 1
    return x
  }
  let b = (c: bool) -> i64 => {
    let n: i64 = if c { 1 } else { return 5 }
    n
  }
	`, false)
	assertNoErrors(t, res)
}

// A value-position block ending in a statement is `void`, which the consumer refuses.
func TestBlockTail_ValueBlockEndingInAStatementIsVoid(t *testing.T) {
	res := parseCollectAndCheck(t, `
  let f = () -> i64 => {
    let n: i64 = { let x = 1 }
    0
  }
	`, false)
	if len(res.errors) == 0 || !strings.Contains(res.errors[0].Message, "cannot assign void to i64") {
		t.Errorf("want the void block refused; got %v", res.errors)
	}
}

// In a `void` body, and in a statement-position block, an `if let` stays a statement:
// its branches need not agree.
func TestBlockTail_StatementPositionIfLetIsUnchanged(t *testing.T) {
	res := parseCollectAndCheck(t, tailTypes+`
  let f = (m: Opt) -> void => {
    var n = 0
    if let Val(v) = m { n = v } else { n = 1 }
  }
  let g = (m: Opt) -> i64 => {
    var n = 0
    if true { if let Val(v) = m { n = v } else { n = 2 } }
    n
  }
	`, false)
	for _, e := range res.errors {
		if !strings.Contains(e.Message, "always true") {
			t.Errorf("unexpected error: %s", e.Message)
		}
	}
}
