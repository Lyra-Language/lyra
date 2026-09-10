package captures_test

import "testing"

// **A global of the same name is only a global where nothing else binds it.**
//
// `globals` is keyed by bare name, so subtracting it from a lambda's free variables
// dropped every capture whose name a top-level declaration anywhere in the program
// happened to share. Nothing reported it: the backend then lowered the read as a
// reference to that declaration, which for a function means a closure *value* — so a
// closure over a parameter named `tint` handed raylib's `tint` function to a C call
// expecting a `Color`, and the ABI lowering died in a Go panic storing a `{ i8*, i8* }`
// into a `%Color*` (09/10).
//
// The shape is why it resisted reduction for a day: the colliding declaration was in a
// **sibling file of the same module**, so every self-contained reproduction lowered fine.
func TestCaptures_ANameAlsoDeclaredAtTopLevel(t *testing.T) {
	assertCaptures(t, `
let tint = (n: i64) -> i64 => n
let outer = (tint: i64) -> (i64) -> i64 => (n: i64) -> i64 => n + tint
let main = () -> void => { }
`, "", "", "tint", "")
}

// The other direction, and the reason `globals` exists at all: a genuine top-level
// function called from a closure is not a capture, so nothing tries to put it in an
// environment.
func TestCaptures_AnUnshadowedGlobalIsStillNotACapture(t *testing.T) {
	assertCaptures(t, `
let helper = (n: i64) -> i64 => n
let outer = (k: i64) -> (i64) -> i64 => (n: i64) -> i64 => helper(n) + k
let main = () -> void => { }
`, "", "", "k", "")
}

// A **sibling** closure's parameter is not in scope, so a name it binds must not make an
// unrelated global of that name look capturable — which is what `directBinders` stopping
// at a nested lambda is for. Getting this wrong is loud rather than silent (the backend
// finds no enclosing binding to copy), but it would turn working programs into errors.
func TestCaptures_ASiblingLambdasParameterIsNotInScope(t *testing.T) {
	assertCaptures(t, `
let helper = (n: i64) -> i64 => n
let outer = (k: i64) -> i64 => {
  let a = (helper: i64) -> i64 => helper + 1
  let b = (n: i64) -> i64 => helper(n) + k
  a(1) + b(2)
}
let main = () -> void => { }
`, "", "", "", "k", "")
}
