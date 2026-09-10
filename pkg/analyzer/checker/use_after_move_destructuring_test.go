package checker_test

import "testing"

// A binding a destructuring introduces is **fresh**, so a move recorded against that name
// does not survive to the next iteration of a loop.
//
// `loopBody` seeds the state with every move performed anywhere in the body, so that a move
// flags the *next* iteration's read — correct for a binding that outlives the loop. A
// declaration inside the body then deletes its own name, because that binding is new each
// time round; `VarDeclStmt` did, and `DestructuringDeclStmt` did not, so it fell to the
// generic walker which walks the value and clears nothing.
//
// The result was `lyra-E019` on correct code, claiming a later iteration would read a value
// moved by an earlier one — when each iteration binds a different value. The control below
// is the same loop written `let x = p`, which was always clean: a difference between two
// spellings of one thing is the signature of a missing case rather than a rule.
func TestUseAfterMove_DestructuringInALoopIsAFreshBinding(t *testing.T) {
	for _, c := range []struct{ name, src string }{
		{"tuple destructuring", `
let take = (s: own string) -> i64 => s.len()
let main = () -> void => {
  var total = 0
  let ps: [](string, string) = [("aa", "bb"), ("cc", "dd")]
  for pair in ps {
    let (x, y) = pair
    total = total + take(x)
  }
}`},
		// `if let` embeds the declaration **by value**, so the walker never sees a
		// *DestructuringDeclStmt and the case above cannot cover it.
		{"if let", `
data Box = Full(string) | Empty
let take = (s: own string) -> i64 => s.len()
let main = () -> void => {
  var total = 0
  let bs: []Box = [Full("aa"), Full("bb")]
  for b in bs {
    if let Full(x) = b {
      total = total + take(x)
    }
  }
}`},
		// The control: identical shape, single binding, always accepted.
		{"plain let (control)", `
let take = (s: own string) -> i64 => s.len()
let main = () -> void => {
  var total = 0
  let ps: []string = ["aa", "bb"]
  for p in ps {
    let x = p
    total = total + take(x)
  }
}`},
	} {
		t.Run(c.name, func(t *testing.T) {
			if diags := checkMoves(t, c.src); len(diags) != 0 {
				t.Errorf("expected no use-after-move diagnostics, got %v — each iteration "+
					"binds a different value", diags)
			}
		})
	}
}

// The other direction: a genuine double move of a destructured binding is still caught, and
// so is one inside an `if let` branch. Clearing the name at its declaration must not clear
// it for the rest of the body.
func TestUseAfterMove_DestructuredBindingStillCatchesARealDoubleMove(t *testing.T) {
	for _, c := range []struct{ name, src string }{
		{"tuple destructuring", `
let take = (s: own string) -> i64 => s.len()
let main = () -> void => {
  let pair = ("aa", "bb")
  let (x, y) = pair
  println(take(x) + take(x))
}`},
		{"if let", `
data Box = Full(string) | Empty
let take = (s: own string) -> i64 => s.len()
let main = () -> void => {
  let b: Box = Full("aa")
  if let Full(x) = b {
    println(take(x) + take(x))
  }
}`},
	} {
		t.Run(c.name, func(t *testing.T) {
			if diags := checkMoves(t, c.src); len(diags) == 0 {
				t.Error("expected a use-after-move diagnostic for a binding moved twice")
			}
		})
	}
}

// `let … else` reads like a branch and is not one, and analyzing it as one reported
// `lyra-E019` — a hard error — on two shapes of correct code.
//
// It is Rust's `let … else`: the payload binds in the **enclosing** scope and outlives the
// statement, while the else block is the diverging path that never sees it. The collector
// says so where it builds the node, and the compiler agrees: `let Some(v) = opt() else {
// println("${v}") }` is `undefined identifier "v"`, while a use after the statement checks
// clean. The pass had the opposite reading, binding the names in the else branch.
//
// Found 09/10 while giving `@must_release` the same construct — the two passes answer
// mirror-image questions over the same AST, so a shape misread by one is worth checking in
// the other. This is that check, kept.
const letElseMovePrelude = `
data Maybe<t> = None | Some(t)
let take = (s: own string) -> i64 => s.len()
let opt = () -> Maybe<i64> => Some(1)
let optS = () -> Maybe<string> => Some("x")
`

// A move inside the diverging `else` must not escape it: the statement after runs only
// when the pattern matched, in which case the else never ran.
//
// Discarding those moves is sound because the else always diverges — `lyrac check` alone
// does not enforce that, but the backend refuses a non-diverging one by name, so it holds
// for every program that can be built.
func TestUseAfterMove_LetElseDivergingBranchDoesNotEscape(t *testing.T) {
	if diags := checkMoves(t, letElseMovePrelude+`
let main = () -> void => {
  var s = "hello"
  let Some(v) = opt() else { let a = take(s); return }
  let b = take(s)
}
`); len(diags) != 0 {
		t.Errorf("expected no diagnostics — the else returns, so the move cannot reach "+
			"the statement after it; got %v", diags)
	}
}

// A name **rebound** by a `let … else` is a fresh binding, so a move recorded against the
// old one does not survive. The clear used to happen inside the else's own state, where
// the union then threw it away.
func TestUseAfterMove_LetElseReboundNameIsFresh(t *testing.T) {
	if diags := checkMoves(t, letElseMovePrelude+`
let main = () -> void => {
  var s = "hello"
  let n = take(s)
  let Some(s) = optS() else { return }
  let m = take(s)
}
`); len(diags) != 0 {
		t.Errorf("expected no diagnostics — the let-else rebinds `s`; got %v", diags)
	}
}

// The other half, and the one that keeps the fix honest: the pass must still *see* into
// the else. Only the escape of its moves was wrong, not the walking of it.
func TestUseAfterMove_LetElseStillReportsInsideTheElse(t *testing.T) {
	assertMoveErrors(t, letElseMovePrelude+`
let main = () -> void => {
  var s = "hello"
  let Some(v) = opt() else { let a = take(s); let b = take(s); return }
}
`, 1)
}

// And a genuine move of the payload after the statement is still reported — the payload
// really does live in the enclosing scope, which is the whole point.
func TestUseAfterMove_LetElsePayloadMovedAfterwardsIsReported(t *testing.T) {
	assertMoveErrors(t, letElseMovePrelude+`
let main = () -> void => {
  let Some(v) = optS() else { return }
  let a = take(v)
  let b = take(v)
}
`, 1)
}

// The control: `if let` genuinely is a branch, its else may fall through, and unioning
// that branch's moves is correct there. This pins that the fix did not spread.
func TestUseAfterMove_IfLetElseBranchStillUnions(t *testing.T) {
	assertMoveErrors(t, letElseMovePrelude+`
let main = () -> void => {
  var s = "hello"
  if let Some(v) = opt() { let a = 1 } else { let b = take(s) }
  let c = take(s)
}
`, 1)
}
