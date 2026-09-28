package llvm

import "testing"

// `if c { Ok("yes") } else { Err(7) }` with no annotation: each arm solves half of
// `Result`, and the join settles both (typechecker/join_partial.go). Refused until 09/28.
// Under ASan, with a managed `string` payload; with and without a later context.
func TestExec_BranchesJoinPartialInstantiations(t *testing.T) {
	src := `module main

let pick = (c: bool) -> Result<u8, string> => if c { Ok(200) } else { Err("nope") }

let main = () -> u8 => {
  let c = false
  let r = if c { Ok("yes") } else { Err(7) }
  var n: u8 = match r { Ok(s) => u8(s.len()), Err(k) => u8(k) }
  let m = match n { 7 => Ok(1), 8 => Err("eight"), _ => Ok(3) }
  n += match m { Ok(v) => u8(v), Err(s) => u8(s.len()) }
  n += match pick(true) { Ok(v) => v - 190, Err(_) => 0 }
  n += match pick(false) { Ok(_) => 0, Err(s) => u8(s.len()) }
  n
}
`
	// 7 + 1 + 10 + 4 = 22
	if got := buildAndRunASanWithPrelude(t, src); got != 22 {
		t.Errorf("exited %d; want 22 (ASan aborts with another code)", got)
	}
}

// The same one level down and two: `Some(Ok(1))` beside `Some(Err("xy"))`, a `match` with
// a `None` arm, and `Some(Some(…))`. Under ASan, with the `string` inside two constructions.
func TestExec_NestedBranchesJoin(t *testing.T) {
	src := `module main

let main = () -> u8 => {
  let c = false
  let m = if c { Some(Ok(1)) } else { Some(Err("xy")) }
  var n: u8 = match m { Some(Ok(v)) => u8(v), Some(Err(s)) => u8(s.len()), None => 90 }
  let k = match n { 0 => Some(Ok(1)), 1 => None, _ => Some(Err("abc")) }
  n += match k { Some(Ok(v)) => u8(v), Some(Err(s)) => u8(s.len()), None => 90 }
  let d = if !c { Some(Some(Ok(4))) } else { Some(Some(Err("x"))) }
  n += match d { Some(Some(Ok(v))) => u8(v), _ => 90 }
  n
}
`
	// 2 + 3 + 4 = 9
	if got := buildAndRunASanWithPrelude(t, src); got != 9 {
		t.Errorf("exited %d; want 9 (ASan aborts with another code)", got)
	}
}
