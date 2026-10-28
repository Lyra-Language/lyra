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

// A `void` field **beside others** takes no space either: `Checkable(bool, e)`, a struct's
// `ev: e` and a tuple's element at `e = void` are each an empty struct in the layout, built
// from the void value's effects alone, and compare equal. Until 10/27 the payload lowered
// as `{ i1, void }`, invalid IR, so `ui.layout`'s `Node<void>` stood in as `NoEvent`.
// Under ASan, with strings beside the void fields, so a copy or drop misreading the
// layout would fault.
func TestExec_AVoidFieldBesideOthers(t *testing.T) {
	t.Parallel()
	src := `module main
data Ev<e> = Tick(string, e) | Idle | Pair(e, i64)
struct W<e> { n: i64, ev: e, label: string }
let tick<e> = (s: string, f: () -> e) -> Ev<e> => Tick(s, f())
let pair<e> = (f: () -> e, n: i64) -> Ev<e> => Pair(f(), n)
let wrap<e> = (f: () -> e) -> W<e> => W { n: 3, ev: f(), label: "w" ++ "!" }
let both<e> = (f: () -> e) -> (string, e) => ("t" ++ "u", f())
let name<e> = (v: Ev<e>) -> string => match v {
  Tick(s, _) => s,
  Idle => "idle",
  Pair(_, n) => "pair ${n}",
}
var hits = 0
let main = () -> u8 => {
  let noise = () -> void => { hits += 1 }
  let xs: []Ev<void> = [tick("a" ++ "b", noise), Idle, pair(noise, 9), tick("c", noise)]
  var out: []string = []
  for x in xs { out.push(name(x)) }
  let w = wrap(noise)
  let t = both(noise)
  var score: u8 = 0
  if out.join(",") == "ab,idle,pair 9,c" { score += 1 }
  if hits == 5 { score += 2 }
  if w.n == 3 && w.label == "w!" && t.0 == "tu" { score += 4 }
  if tick("a", noise) == tick("a", noise) && tick("a", noise) != tick("b", noise) { score += 8 }
  if let Tick(s, _) = xs[3] { if s == "c" { score += 16 } }
  score
}
`
	if got := buildAndRunASanWithPrelude(t, src); got != 31 {
		t.Errorf("exited %d; want 31 (each check adds its bit; ASan aborts with another code)", got)
	}
}
