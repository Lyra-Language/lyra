package llvm

import "testing"

// An `if let`'s alternative chains as a plain `if`'s does — `else if c`, `else if let`
// — and a plain `if`'s may be an `if let`. The collector erases the chain into the
// one-statement block `else { if … }` produces, so this pins the spelling end to end:
// until 10/19 `if let … else if …` was a syntax error, reported at the enclosing
// declaration's first line. Under ASan, with a managed `string` out of the chain.
func TestExec_IfLetElseIf(t *testing.T) {
	src := `module main

let stmt = (m: Maybe<i64>, b: bool) -> i64 => {
  var r = 0
  if let Some(x) = m { r = x } else if b { r = 1 } else { r = 2 }
  r
}

let lets = (m: Maybe<i64>, e: Result<i64, string>) -> i64 => {
  var r = 0
  if let Some(x) = m {
    r = x
  } else if let Ok(y) = e {
    r = y
  } else {
    r = 3
  }
  r
}

let mirror = (b: bool, m: Maybe<i64>) -> i64 =>
  if b { 4 } else if let Some(x) = m { x } else { 5 }

let value = (m: Maybe<string>, n: Maybe<string>) -> string => {
  if let Some(s) = m { s ++ "!" } else if let Some(t) = n { t } else { "none" }
}

let main = () -> u8 => {
  let bad: Result<i64, string> = Err("no")
  var n = stmt(None, true)
  n += stmt(None, false) + stmt(Some(6), true)
  n += lets(Some(7), bad) + lets(None, Ok(8)) + lets(None, bad)
  n += mirror(true, Some(9)) + mirror(false, Some(1)) + mirror(false, None)
  n += value(Some("hi"), None).len() + value(None, Some("abcd")).len() + value(None, None).len()
  u8(n)
}
`
	// 1 + 2 + 6 + 7 + 8 + 3 + 4 + 1 + 5 + (3 + 4 + 4) = 48
	if got := buildAndRunASanWithPrelude(t, src); got != 48 {
		t.Errorf("exited %d; want 48 (ASan aborts with another code)", got)
	}
}
