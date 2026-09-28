package llvm

import "testing"

// `Some(Ok(5))`: an outer construction over an inner one that left a parameter for the
// context. Refused by the typechecker until 09/28, annotated or not; now the context
// narrows the inner construction too. Under ASan, with a `string` error payload two
// levels in, in return, argument, `if` and `match` positions and three levels deep.
func TestExec_NestedConstruction(t *testing.T) {
	src := `module main

let score = (x: Maybe<Result<i64, string>>) -> i64 => match x {
  Some(Ok(v)) => v,
  Some(Err(why)) => why.len(),
  None => 100,
}

let narrow = () -> Maybe<Result<u8, string>> => Some(Ok(200))

let deep = () -> Maybe<Maybe<Result<i64, string>>> => Some(Some(Ok(3)))

let pick = (b: bool) -> Maybe<Result<i64, string>> =>
  if b { Some(Ok(4)) } else { Some(Err("no")) }

let by_match = (n: i64) -> Maybe<Result<i64, string>> => match n {
  0 => None,
  1 => Some(Ok(10)),
  _ => Some(Err("many")),
}

let main = () -> u8 => {
  var s: i64 = score(Some(Ok(1)))
  if let Some(Ok(v)) = narrow() { s += i64(v) }
  if let Some(Some(Ok(v))) = deep() { s += v }
  s += score(pick(true)) + score(pick(false))
  s += score(by_match(0)) + score(by_match(1)) + score(by_match(2))
  u8(s - 200)
}
`
	// 1 + 200 + 3 + 4 + 2 + 100 + 10 + 4 = 324; less 200 is 124.
	if got := buildAndRunASanWithPrelude(t, src); got != 124 {
		t.Errorf("exited %d; want 124 (ASan aborts with another code)", got)
	}
}
