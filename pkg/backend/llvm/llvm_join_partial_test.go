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
