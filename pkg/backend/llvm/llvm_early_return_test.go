package llvm

import "testing"

// **An early exit releases the temporaries of every statement it leaves**, not just its
// own (09/18).
//
// A `return` nested in a block is a statement of its own, and the flush a return runs
// covers only its own statement's temporaries. The scrutinee of the `if let` or `match`
// around it belongs to the *enclosing* statement, whose flush this path never reaches —
// control has left the function — so it was released by nothing: `if let Some(w) =
// bag.first_of() { return 0 }` leaked the string on every early exit. Found by
// `examples/calendar`'s `main`, which returns out of `if let Some(when) = args.value(…)`.
// break/continue had always recorded what they skip past; a return now does the same, from
// the bottom of the stack, resolved at the end of the function where dominance is final.
//
// **The danger of the fix is the opposite bug**, so every case runs under ASan: a double
// free or a use-after-free fails on either platform, and LeakSanitizer adds the leak on
// Linux (./asan.sh). The shapes are the ones where a release can land wrong:
//
//   - returning the borrowed payload *itself*, whose reference must outlive the
//     scrutinee's release;
//   - `?` inside the body, which releases its own statement's temporaries before it
//     returns and must not have the return release them again;
//   - an argument temporary still in flight when a later argument returns;
//   - and break, whose path was already right and must stay unchanged.
//
// Each program answers a distinct exit code, so a memory error's abort cannot be mistaken
// for success. **Every early exit sits in a helper called twice, never in `main`**:
// LeakSanitizer scans conservatively at exit, and a leaked pointer left in `main`'s dead
// stack slots reads as reachable — three of these cases passed with the fix reverted until
// they moved, while `leaks` on macOS reported the same shape as lost.
func TestExec_AnEarlyExitReleasesEnclosingTemporaries(t *testing.T) {
	const bag = `
struct Bag { xs: []string }
let first_of = pure (self: Bag) -> Maybe<string> => if self.xs.len() > 0 { Some(self.xs[0]) } else { None }
let bag = () -> Bag => Bag { xs: ["a" ++ "b"] }
`
	cases := []struct {
		name, src string
		want      int
	}{
		{"return out of an if let", `
let f = () -> u8 => {
  let b = bag()
  if let Some(w) = b.first_of() { return 3 }
  0
}
let main = () -> u8 => { let _ = f()
  f() }`, 3},
		{"return out of a match arm", `
let f = () -> u8 => {
  let b = bag()
  match b.first_of() { Some(w) => { return 4 }, None => {} }
  0
}
let main = () -> u8 => { let _ = f()
  f() }`, 4},
		{"return the borrowed payload itself", `
let pick = (b: Bag) -> string => {
  if let Some(w) = b.first_of() { return w }
  "none"
}
let main = () -> u8 => u8(pick(bag()).len())`, 2},
		{"? inside an if let body", `
let parse = (s: string) -> Result<i64, string> => if s == "ab" { Err("nope") } else { Ok(1) }
let run = (b: Bag) -> Result<i64, string> => {
  if let Some(w) = b.first_of() {
    let n = parse(w)?
    return Ok(n)
  }
  Ok(0)
}
let main = () -> u8 => match run(bag()) { Ok(n) => 1, Err(e) => u8(e.len()) + 1 }`, 5},
		{"an if let inside a match arm", `
let f = () -> u8 => {
  let b = bag()
  match b.first_of() {
    Some(w) => { if let Some(v) = b.first_of() { return 6 } },
    None => {},
  }
  0
}
let main = () -> u8 => { let _ = f()
  f() }`, 6},
		{"break out of an if let in a loop", `
let main = () -> u8 => {
  let b = bag()
  var n = 0
  for i in 0..<3 {
    if let Some(w) = b.first_of() { if i == 1 { break } }
    n += 1
  }
  u8(n + 10)
}`, 11},
		{"an argument in flight when a later one returns", `
let g = (s: string, n: i64) -> i64 => s.len() + n
let main = () -> u8 => {
  let b = bag()
  let r = g("x" ++ "y", if b.xs.len() > 0 { return 7 } else { 2 })
  u8(r)
}`, 7},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if got := buildAndRunASanWithPrelude(t, "module main\n"+bag+c.src); got != c.want {
				t.Errorf("exited %d; want %d (a memory error aborts with a different code)", got, c.want)
			}
		})
	}
}
