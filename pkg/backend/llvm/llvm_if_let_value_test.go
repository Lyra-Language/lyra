package llvm

import "testing"

// `if let … else …` as a block's value — a body's tail, a nested `if` branch, a chain, a
// match arm — lowered as the `match` it means (typechecker/block_tail.go). Until 09/28 it
// type-checked and the backend refused it: "block has no value". Under ASan, with a
// managed `string` flowing out of an arm both borrowed and freshly built.
func TestExec_IfLetAsAValue(t *testing.T) {
	src := `module main

let first = (m: Maybe<u8>) -> u8 => {
  if let Some(v) = m { v } else { 0 }
}

let branch = (c: bool, m: Maybe<u8>) -> u8 =>
  if c { if let Some(v) = m { v } else { 1 } } else { 2 }

let chained = (a: Maybe<u8>, b: Maybe<u8>) -> u8 => {
  if let Some(x) = a { x } else { if let Some(y) = b { y + 100 } else { 200 } }
}

let in_arm = (n: i64, m: Maybe<u8>) -> u8 => match n {
  0 => { if let Some(v) = m { v } else { 3 } },
  _ => 4,
}

let shout = (m: Maybe<string>) -> string => {
  if let Some(s) = m { s ++ "!" } else { "none" }
}

let pass = (m: Maybe<string>) -> string => {
  if let Some(s) = m { s } else { "none" }
}

let main = () -> u8 => {
  var n = first(Some(7)) + first(None)
  n += branch(true, Some(10)) + branch(true, None) + branch(false, None)
  n += chained(None, Some(5)) - 100
  n += in_arm(0, None) + in_arm(1, None)
  n += u8(shout(Some("hi")).len() + shout(None).len() + pass(Some("abc")).len())
  n
}
`
	// 7 + 10 + 1 + 2 + 5 + 3 + 4 + (3 + 4 + 3) = 42
	if got := buildAndRunASanWithPrelude(t, src); got != 42 {
		t.Errorf("exited %d; want 42 (ASan aborts with another code)", got)
	}
}
