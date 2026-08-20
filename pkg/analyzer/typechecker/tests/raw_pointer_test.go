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

// `p.offset(n)` — the language's only pointer arithmetic, and a method rather than an
// operator on purpose. `p[i]` was the obvious spelling and is the wrong one: it is
// `xs[i]`'s spelling with none of `xs[i]`'s bounds check, so two things that behave
// differently would look alike. Written this way the rule stays statable, and `^` remains
// the only load — `p.offset(3)^` is visibly the two acts it is.
func TestPointers_OffsetProducesAPointerToRead(t *testing.T) {
	assertNoErrors(t, parseCollectAndCheck(t, `
let main = () -> void => {
  var xs: []i64 = [1, 2, 3]
  unsafe {
    let p = &xs[0]
    println("${p.offset(2)^}")
  }
}`, false))
}

// **Mutability propagates**, so a `^mut T` offsets to a `^mut T` and the write direction
// works at all. The inverse is what pins it: offsetting a read-only pointer must not
// launder it into a writable one.
func TestPointers_OffsetOfAReadOnlyPointerCannotBeWritten(t *testing.T) {
	res := parseCollectAndCheck(t, `
let main = () -> void => {
  var xs: []i64 = [1, 2, 3]
  unsafe {
    let p = &xs[0]
    p.offset(1)^ = 9
  }
}`, false)
	assertHasErrorContaining(t, res, "it is a read-only pointer")
}

func TestPointers_OffsetOfAMutablePointerCanBeWritten(t *testing.T) {
	assertNoErrors(t, parseCollectAndCheck(t, `
let main = () -> void => {
  var xs: []i64 = [1, 2, 3]
  unsafe {
    let p = &mut xs[0]
    p.offset(1)^ = 9
  }
}`, false))
}

// It needs an `unsafe` context like every other pointer operation — and the check lives in
// the typechecker rather than in the syntactic pass that owns the rest of lyra-E011,
// because it needs the **receiver's type**: `p.offset(n)` and `xs.offset(n)` are the same
// three tokens, and a name-keyed check would refuse both or neither.
func TestPointers_OffsetNeedsAnUnsafeContext(t *testing.T) {
	res := parseCollectAndCheck(t, `
let main = () -> void => {
  var xs: []i64 = [1, 2, 3]
  let p = unsafe { &xs[0] }
  let q = p.offset(1)
  println("${unsafe { q^ }}")
}`, false)
	assertHasErrorContaining(t, res, "pointer arithmetic with `offset` requires an `unsafe`")
}

// And it is a pointer method only. `offset` is an ordinary name a user type may declare,
// so nothing here may claim it for every receiver.
func TestPointers_OffsetIsNotAMethodOnAnArray(t *testing.T) {
	res := parseCollectAndCheck(t, `
let main = () -> void => {
  var xs: []i64 = [1, 2, 3]
  println("${xs.offset(1)}")
}`, false)
	if len(res.errors) == 0 {
		t.Error("`offset` is a raw-pointer method; an array receiver must not resolve to it")
	}
}

// **A `^mut T` is assignable to a `^T`, and not the reverse.** Dropping the permission to
// write is safe — the two are the same machine value and `^T` can do strictly less —
// where adding it is the hole lyra-E061 exists to close. Refused until 08/19, which meant
// `CBuffer { ptr: &mut xs[0], … }` had to be written `&xs[0]`: harmless where a read-only
// pointer was wanted anyway, which is why it blocked nothing and was wrong as a rule.
//
// All four positions, because they are one rule and would otherwise be four.
func TestPointers_MutablePointerIsAssignableToAReadOnlyOne(t *testing.T) {
	assertNoErrors(t, parseCollectAndCheck(t, `
struct Buf { ptr: ^u8, len: i64 }
let takes = pure (p: ^u8) -> u8 => unsafe { p^ }
let gives = pure (xs: mut []u8) -> ^u8 => unsafe { &mut xs[0] }
let main = () -> void => {
  var xs: []u8 = [1, 2, 3]
  unsafe {
    let m = &mut xs[0]
    let a: ^u8 = m
    let b = Buf { ptr: m, len: 3 }
    println("${a^} ${b.ptr^} ${takes(m)} ${gives(xs)^}")
  }
}`, false))
}

func TestPointers_ReadOnlyPointerIsNotAssignableToAMutableOne(t *testing.T) {
	res := parseCollectAndCheck(t, `
let main = () -> void => {
  var x: i64 = 5
  unsafe {
    let r = &x
    let bad: ^mut i64 = r
  }
}`, false)
	assertHasErrorContaining(t, res, "cannot assign ^i64 to ^mut i64")
}

// **The downgrade is real, not cosmetic**, which is the property that makes the rule safe
// rather than merely convenient: the binding takes the annotated type, so writing through
// it is refused even though the pointer it came from was writable.
func TestPointers_ADowngradedPointerCannotBeWrittenThrough(t *testing.T) {
	res := parseCollectAndCheck(t, `
let main = () -> void => {
  var x: i64 = 5
  unsafe {
    let down: ^i64 = &mut x
    down^ = 7
  }
}`, false)
	assertHasErrorContaining(t, res, "it is a read-only pointer")
}

// The pointee stays invariant. `^Meters` to `^i64` would let a write through the second
// land in storage the first names — the mixup a newtype exists to prevent — and unlike
// mutability it is not a strictly-weaker permission.
func TestPointers_PointeeIsInvariant(t *testing.T) {
	res := parseCollectAndCheck(t, `
newtype Meters = i64
let takes = pure (p: ^i64) -> i64 => unsafe { p^ }
let main = () -> void => {
  var m: Meters = 5
  unsafe { println("${takes(&mut m)}") }
}`, false)
	assertHasErrorContaining(t, res, "cannot assign ^mut Meters to ^i64")
}

// **A generic function over a pointer is callable**, which it was not until 08/19:
// `unifyGenericTarget` had no raw-pointer case, so `^t` never bound `t` and every such
// call reported "cannot infer type variable t" — for a plain `^u8` as much as a `^mut u8`,
// so this is independent of the assignability rule above.
func TestPointers_GenericOverAPointerSolvesItsTypeVariable(t *testing.T) {
	assertNoErrors(t, parseCollectAndCheck(t, `
let first<t> = pure (p: ^t) -> t => unsafe { p^ }
let poke<t> = (p: ^mut t, v: t) -> void => unsafe { p^ = v }
let main = () -> void => {
  var xs: []u8 = [40, 41]
  var n: i64 = 7
  unsafe {
    println("${first(&xs[1])} ${first(&mut xs[0])} ${first(&n)}")
    poke(&mut n, 9)
  }
}`, false))
}

// And solving `t` does not launder the mutability: unification decides what `t` is,
// assignability decides whether the argument may be passed, and the diagnostic names the
// *substituted* types rather than the type variable.
func TestPointers_GenericMutablePointerParameterStillRefusesAReadOnlyArgument(t *testing.T) {
	res := parseCollectAndCheck(t, `
let poke<t> = (p: ^mut t, v: t) -> void => unsafe { p^ = v }
let main = () -> void => {
  var n: i64 = 7
  unsafe { poke(&n, 9) }
}`, false)
	assertHasErrorContaining(t, res, "cannot assign ^i64 to ^mut i64")
}
