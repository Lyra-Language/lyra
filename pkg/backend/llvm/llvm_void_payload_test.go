package llvm

import "testing"

// A `void` payload — `Ok()` of a `Result<void, e>` — is built as the tag alone and
// matched as a nullary variant, since FieldTypes reports a lone void field as none; `?`
// over it yields nothing. Under ASan, with a `string` error beside it, so a drop that
// read the void variant's payload would fault. Until 09/28 `Result<void, e>` could be
// written as a type and never built.
func TestExec_VoidPayload(t *testing.T) {
	src := `module main

data Step =
  Done(void)
  | Failed(string)

let check = (x: i64) -> Result<void, string> => {
  if x < 0 { return Err("negative: ${x}") }
  Ok()
}

let both = (a: i64, b: i64) -> Result<void, string> => {
  check(a)?
  check(b)?
  Ok()
}

let fetch = (n: i64) -> Result<i64, void> => if n > 0 { Ok(n) } else { Err() }

let doubled = (n: i64) -> Result<i64, void> => {
  let v = fetch(n)?
  Ok(v * 2)
}

let finish = (ok: bool) -> Step => if ok { Done() } else { Failed("jammed") }

let main = () -> u8 => {
  var score: u8 = 0
  if let Ok() = both(1, 2) { score += 1 }
  match both(1, -2) {
    Ok(_) => {},
    Err(why) => if why == "negative: -2" { score += 2 },
  }
  match doubled(4) {
    Ok(v) => if v == 8 { score += 4 },
    Err() => {},
  }
  if let Err(_) = doubled(0) { score += 8 }
  match finish(true) {
    Done() => { score += 16 },
    Failed(_) => {},
  }
  if let Failed(why) = finish(false) { if why == "jammed" { score += 32 } }
  let a: Maybe<void> = Some()
  if a == Some() { score += 64 }
  score
}
`
	if got := buildAndRunASanWithPrelude(t, src); got != 127 {
		t.Errorf("exited %d; want 127 (each check adds its bit; ASan aborts with another code)", got)
	}
}
