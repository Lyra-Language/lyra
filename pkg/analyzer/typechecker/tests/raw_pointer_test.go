package typechecker_test

import "testing"

// Raw pointers: `&x`, `&mut x`, `p^`, `p^ = v`, and the `unsafe { … }` block they must sit
// inside. Implemented 08/18; refused as lyra-E051 from 08/13 until then, and before that
// they fell to the typechecker's default arm as `unknown expression type
// "address_of_expr"`, which reads like an internal error rather than an unbuilt feature.
//
// The type, the grammar, the collector and lyra-E011's unsafe-context policy all existed
// throughout; what was missing was the two ends — nothing inferred these expressions and
// nothing lowered them. E011 is reported again now, because its advice ("requires an
// `unsafe` block") finally names something that compiles.

func TestPointers_TakeAndRead(t *testing.T) {
	assertNoErrors(t, parseCollectAndCheck(t, `
let main = () -> void => {
  var x: i64 = 5
  unsafe {
    let p = &x
    println("${p^}")
  }
}`, false))
}

func TestPointers_WriteThroughAMutablePointer(t *testing.T) {
	assertNoErrors(t, parseCollectAndCheck(t, `
let main = () -> void => {
  var x: i64 = 5
  unsafe {
    let p = &mut x
    p^ = 6
  }
}`, false))
}

// **Mutability is checked twice, and the two are not interchangeable.** Taking `&mut x`
// asks whether *x* may be mutated — the binding rule every interior mutation obeys — while
// writing `p^ = v` asks whether *p* is a `^mut`. A `^mut T` can be copied into a `let` and
// a `^T` can be taken of a `var`, so neither answer implies the other, and a program that
// checked only one could write through a pointer it was never allowed to take.
func TestPointers_WriteThroughAReadOnlyPointerRefused(t *testing.T) {
	res := parseCollectAndCheck(t, `
let main = () -> void => {
  var x: i64 = 5
  unsafe {
    let p = &x
    p^ = 6
  }
}`, false)
	assertHasErrorContaining(t, res, "it is a read-only pointer")
}

func TestPointers_MutablePointerToAnImmutableBindingRefused(t *testing.T) {
	res := parseCollectAndCheck(t, `
let main = () -> void => {
  let x: i64 = 5
  unsafe {
    let p = &mut x
  }
}`, false)
	assertHasErrorContaining(t, res, "deeply immutable")
}

// Only storage has an address. `&f()` would name a temporary that stops existing at the
// end of the statement, so the pointer dangles immediately — a compile-time fact, not
// something to leave to the reader.
func TestPointers_AddressOfATemporaryRefused(t *testing.T) {
	res := parseCollectAndCheck(t, `
let f = pure () -> i64 => 1
let main = () -> void => {
  unsafe {
    let p = &f()
  }
}`, false)
	assertHasErrorContaining(t, res, "cannot take the address of a temporary")
}

func TestPointers_DerefOfANonPointerRefused(t *testing.T) {
	res := parseCollectAndCheck(t, `
let main = () -> void => {
  var x: i64 = 5
  unsafe {
    let v = x^
  }
}`, false)
	assertHasErrorContaining(t, res, "it is not a raw pointer")
}

// The pointee's type is checked on a write, like any other assignment.
func TestPointers_WriteOfTheWrongTypeRefused(t *testing.T) {
	res := parseCollectAndCheck(t, `
let main = () -> void => {
  var x: i64 = 5
  unsafe {
    let p = &mut x
    p^ = "hi"
  }
}`, false)
	assertHasErrorContaining(t, res, "cannot assign string through a pointer to i64")
}

// A field and an element are storage too, so both have addresses.
func TestPointers_AddressOfAFieldAndAnElement(t *testing.T) {
	assertNoErrors(t, parseCollectAndCheck(t, `
struct Pt { x: i64, y: i64 }
let main = () -> void => {
  var p = Pt { x: 1, y: 2 }
  var xs: []i64 = [1, 2]
  unsafe {
    let fp = &mut p.y
    fp^ = 9
    let ep = &mut xs[0]
    ep^ = 8
  }
}`, false))
}

// An `unsafe` block is its body: it changes what is *permitted* inside it, never what
// anything means or produces. So it takes a value in value position…
func TestPointers_UnsafeBlockIsItsBody(t *testing.T) {
	assertNoErrors(t, parseCollectAndCheck(t, `
let main = () -> void => {
  var x: i64 = 5
  let doubled = unsafe {
    let p = &x
    p^ * 2
  }
  println("${doubled}")
}`, false))
}

// …and a binding declared inside it is in scope for the rest of it. That reads as
// obvious and was not: `UnsafeBlockExpr.Body` was a BlockExpr **by value**, so the block
// the typechecker saw was a copy with a different address from the one the scope table
// was keyed on, and every reference to such a binding reported "undefined identifier".
// Invisible for as long as the block was refused before anything looked inside it.
func TestPointers_UnsafeBlockBodyKeepsItsScope(t *testing.T) {
	assertNoErrors(t, parseCollectAndCheck(t, `
let main = () -> void => {
  var x: i64 = 5
  unsafe {
    let p = &mut x
    p^ = p^ + 1
    println("${p^}")
  }
}`, false))
}

// An ordinary mistake inside an `unsafe` block is still reported — the keyword suspends a
// safety *rule*, not type checking.
func TestPointers_UnsafeBlockBodyIsStillChecked(t *testing.T) {
	res := parseCollectAndCheck(t, `
let main = () -> void => {
  unsafe {
    let x: i64 = "not an int"
  }
}`, false)
	assertHasErrorContaining(t, res, "x: cannot assign string to i64")
}

// `^T` remains a legal *type*, and a signature mentioning one needs no `unsafe`: it is the
// operations that are unsafe, not naming the type.
func TestPointers_RawPointerTypeAnnotationResolves(t *testing.T) {
	assertNoErrors(t, parseCollectAndCheck(t, `
let take = pure (p: ^i64) -> i64 => 0
let main = () -> void => { println(take2()) }
let take2 = pure () -> i64 => 0`, false))
}

// Calling an `unsafe` function needs an `unsafe` context. Checked here rather than in the
// syntactic pass that owns the rest of lyra-E011, because it is the only question in that
// policy needing a **resolved** callee: matching the callee's name against the top-level
// unsafe functions is hazard 9, and it made every `f(…)` in the prelude report as an
// unsafe call the moment an `extern f` existed anywhere.
func TestPointers_CallingAnUnsafeFunctionNeedsUnsafe(t *testing.T) {
	res := parseCollectAndCheck(t, `
let load = unsafe (n: i64) -> i64 => n
let use = pure (n: i64) -> i64 => load(n)
`, false)
	assertHasErrorContaining(t, res, `calling unsafe function "load" requires an `+"`unsafe`")
}

func TestPointers_CallingAnUnsafeFunctionInsideUnsafeIsFine(t *testing.T) {
	assertNoErrors(t, parseCollectAndCheck(t, `
let load = unsafe (n: i64) -> i64 => n
let use = pure (n: i64) -> i64 => unsafe { load(n) }
`, false))
}

// **The case that moved the check.** A parameter named after an unsafe function is an
// ordinary local, and calling it is an ordinary call — the resolution says so and a name
// could not.
func TestPointers_AParameterShadowingAnUnsafeFunctionIsNotAnUnsafeCall(t *testing.T) {
	assertNoErrors(t, parseCollectAndCheck(t, `
let load = unsafe (n: i64) -> i64 => n
let apply = pure (load: (i64) -> i64, n: i64) -> i64 => load(n)
`, false))
}
