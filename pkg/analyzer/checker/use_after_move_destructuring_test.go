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
