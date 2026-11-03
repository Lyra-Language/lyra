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

// With no return type written, a trailing `if let … else …` is the function's value all the
// same (11/02; it was void). Under ASan for the `string` out of an arm, borrowed and built, and
// through a generic function and a callback, whose return type comes from this inference.
func TestExec_IfLetIsAnUnannotatedFunctionsValue(t *testing.T) {
	t.Parallel()
	src := `var n: i64 = 0
let add = (x: i64) -> void => { n += x }
let top = (m: Maybe<i64>) => { if let Some(v) = m { v + 1 } else { 0 } }
let pass = (m: Maybe<string>) => { if let Some(s) = m { s } else { "none" } }
let shout = (m: Maybe<string>) => { if let Some(s) = m { s ++ "!" } else { "none" ++ "?" } }
let either<e> = (m: Maybe<e>, d: e) => { if let Some(v) = m { v } else { d } }
let chain = (a: Maybe<i64>, b: Maybe<i64>) => {
  if let Some(x) = a { x } else if let Some(y) = b { y + 1 } else if n > 1000 { 1 } else { 2 }
}
let strict = (m: Maybe<i64>) => { if let Some(v) = m { v } else { panic("none") } }
let apply<r> = (x: i64, f: (i64) -> r) -> r => f(x)
let effects = (m: Maybe<i64>) => { if let Some(v) = m { add(v) } else { add(100) } }
let nothing = (m: Maybe<i64>) => { if let Some(_) = m { } else { } }
let main = () -> u8 => {
  let local = (x: i64) => { if let Some(v) = Some(x) { v } else { 0 } }
  effects(Some(1))
  effects(None)
  nothing(None)
  let viaApply = apply(4, (x: i64) => { if let Some(v) = Some(x) { v } else { 0 } })
  let viaMap = [5].map((x: i64) => { if let Some(v) = Some(x) { v } else { 0 } })
  let total = top(Some(2)) + top(None) + local(6) + chain(None, Some(7)) + chain(None, None) +
    strict(Some(8)) + either(Some(9), 0) + either(None, 10) + viaApply + viaMap[0] +
    pass(Some("abc")).len() + pass(None).len() + shout(Some("hi")).len() + shout(None).len() +
    either(None, "xy").len() + n
  u8(total)
}
`
	// 3 + 0 + 6 + 8 + 2 + 8 + 9 + 10 + 4 + 5 + 3 + 4 + 3 + 5 + 2 + 101 = 173
	if got := buildAndRunASanWithPrelude(t, src); got != 173 {
		t.Errorf("exited %d; want 173 (ASan aborts with another code)", got)
	}
}
