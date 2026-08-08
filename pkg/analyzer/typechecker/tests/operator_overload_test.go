package typechecker_test

import "testing"

// Arithmetic and bitwise operator overloading (08/07): `a + b` on a user type resolves
// to a trait method named `(_+_)`, exactly as `a.show()` resolves to `show`.
//
// The trait is the author's — the compiler knows no name here, which is the difference
// from `Eq`/`Ord`. Those two *are* the comparison operators and one trait has to own
// them so `<` and `<=>` cannot disagree; `+` on a matrix and `+` on a duration share
// nothing, so nothing is bought by insisting they come from one trait.

const vec2Add = `
struct Vec2 { x: i64, y: i64 }
trait Add { (_+_): (Self, Self) -> Self }
impl Add for Vec2 { (_+_) = (self, o) => Vec2 { x: self.x + o.x, y: self.y + o.y } }
`

func TestOperator_StructAddDispatches(t *testing.T) {
	assertNoErrors(t, parseCollectAndCheck(t, vec2Add+`
let sum = (a: Vec2, b: Vec2) -> Vec2 => a + b
`, false))
}

// The right operand need not be Self: the signature says what it is, and a scalar on
// the right is the shape half of the useful impls take (`v * 3`).
func TestOperator_MixedOperandTypes(t *testing.T) {
	assertNoErrors(t, parseCollectAndCheck(t, `
struct Vec2 { x: i64, y: i64 }
trait Scale { (_*_): (Self, i64) -> Self }
impl Scale for Vec2 { (_*_) = (self, k) => Vec2 { x: self.x * k, y: self.y * k } }
let twice = (v: Vec2) -> Vec2 => v * 2
`, false))
}

func TestOperator_WrongRightOperandType(t *testing.T) {
	res := parseCollectAndCheck(t, `
struct Vec2 { x: i64, y: i64 }
trait Scale { (_*_): (Self, i64) -> Self }
impl Scale for Vec2 { (_*_) = (self, k) => Vec2 { x: self.x * k, y: self.y * k } }
let bad = (v: Vec2) -> Vec2 => v * v
`, false)
	assertHasErrorContaining(t, res, "takes i64 on the right, got Vec2")
}

// **A primitive is never routed through an impl.** An impl written for a built-in type
// is inert rather than intermittently winning — the rule `dispatchEq` and
// `dispatchOrdCompare` already follow, and the reason is that arithmetic a library can
// redefine is arithmetic no reader can trust.
func TestOperator_PrimitivesKeepTheirBuiltInMeaning(t *testing.T) {
	assertNoErrors(t, parseCollectAndCheck(t, `
trait Add { (_+_): (Self, Self) -> Self }
impl Add for i64 { (_+_) = (self, o) => 999 }
let n = 1 + 2
`, false))
}

// Two traits providing one operator for one type: reported where the operator is
// written, since ranking them would need a specificity ordering the language does not
// have — the same answer the identifier path gives.
func TestOperator_TwoTraitsProvidingItIsAmbiguous(t *testing.T) {
	res := parseCollectAndCheck(t, `
struct Vec2 { x: i64 }
trait Add1 { (_+_): (Self, Self) -> Self }
trait Add2 { (_+_): (Self, Self) -> Self }
impl Add1 for Vec2 { (_+_) = (self, o) => Vec2 { x: 1 } }
impl Add2 for Vec2 { (_+_) = (self, o) => Vec2 { x: 2 } }
let sum = (a: Vec2, b: Vec2) -> Vec2 => a + b
`, false)
	assertHasErrorContaining(t, res, "is ambiguous: Add1, Add2 each provide it")
}

// A trait declaring an operator with the wrong number of parameters. The receiver is
// parameter 0 as it is for every trait method, so a binary operator needs two.
func TestOperator_WrongArityInTheTrait(t *testing.T) {
	res := parseCollectAndCheck(t, `
struct Vec2 { x: i64 }
trait Add { (_+_): (Self) -> Self }
impl Add for Vec2 { (_+_) = (self) => self }
let sum = (a: Vec2, b: Vec2) -> Vec2 => a + b
`, false)
	assertHasErrorContaining(t, res, "with 1 parameter(s)")
}

// Prefix `-` and `~` are their own methods, told from the binary spellings by *kind*:
// a type may implement either without the other.
func TestOperator_PrefixFormsDispatch(t *testing.T) {
	assertNoErrors(t, parseCollectAndCheck(t, `
struct Vec2 { x: i64, y: i64 }
trait Neg { (-_): (Self) -> Self }
impl Neg for Vec2 { (-_) = (self) => Vec2 { x: 0 - self.x, y: 0 - self.y } }
trait Comp { (~_): (Self) -> Self }
impl Comp for Vec2 { (~_) = (self) => self }
let flip = (v: Vec2) -> Vec2 => -v
let comp = (v: Vec2) -> Vec2 => ~v
`, false))
}

// A prefix impl does not give the type a *binary* `-`, and the diagnostic is the
// ordinary one — nothing dispatched, so the built-in rule reports the operands.
func TestOperator_PrefixImplIsNotABinaryImpl(t *testing.T) {
	res := parseCollectAndCheck(t, `
struct Vec2 { x: i64 }
trait Neg { (-_): (Self) -> Self }
impl Neg for Vec2 { (-_) = (self) => self }
let diff = (a: Vec2, b: Vec2) -> Vec2 => a - b
`, false)
	assertHasErrorContaining(t, res, "operands must be numeric")
}

// `x += y` is `x = x + y`, so an overloaded `+` reaches it.
func TestOperator_CompoundAssignmentDispatches(t *testing.T) {
	assertNoErrors(t, parseCollectAndCheck(t, vec2Add+`
let bump = (b: Vec2) -> void => {
  var a = Vec2 { x: 1, y: 2 };
  a += b
}
`, false))
}

// The hole the compound form used to have, and the reason it needed closing before
// this feature shipped: `checkAssignToBinding` asks whether the right side is
// *assignable* to the left, which two structs of one type satisfy — so `a += b` with
// no impl type-checked clean and then failed to lower ("type not found for
// *ast.StructInstanceExpr"). The binary spelling always reported it.
func TestOperator_CompoundAssignmentWithoutAnImplIsRejected(t *testing.T) {
	res := parseCollectAndCheck(t, `
struct Vec2 { x: i64, y: i64 }
let bump = (b: Vec2) -> void => {
  var a = Vec2 { x: 1, y: 2 };
  a += b
}
`, false)
	assertHasErrorContaining(t, res, "operator +=: operands must be numeric, got Vec2")
}

// Immutability is the assignment's own rule and dispatch says nothing about it — so it
// is still reported, and exactly once (asking resolveAssignTarget twice printed it
// twice, which is what assignTargetType exists to avoid).
func TestOperator_CompoundAssignmentToLetIsStillImmutable(t *testing.T) {
	res := parseCollectAndCheck(t, vec2Add+`
let bump = (b: Vec2) -> void => {
  let a = Vec2 { x: 1, y: 2 };
  a += b
}
`, false)
	assertErrorsAre(t, res, "a: 'let' binding is immutable; use 'var' to allow reassignment")
}

// A type parameter names no impl, and unlike `==` there is no structural rule to fall
// back on. The message names both readings because the author meant one of them.
func TestOperator_OnATypeParameterIsRejected(t *testing.T) {
	res := parseCollectAndCheck(t, `
let double = (x: t) -> t => x + x
`, false)
	assertHasErrorContaining(t, res, "is a type parameter — built-in arithmetic needs a numeric type")
}

// A generic impl matches the same way it does for an identifier-named method — the
// resolution goes through one function, so the two cannot drift.
func TestOperator_GenericImplDispatches(t *testing.T) {
	assertNoErrors(t, parseCollectAndCheck(t, `
struct Box<t> { v: t }
trait Add { (_+_): (Self, Self) -> Self }
impl Add for Box<t> { (_+_) = (self, o) => self }
let sum = (a: Box<i64>, b: Box<i64>) -> Box<i64> => a + b
`, false))
}

// ── an operator through a `where` bound (08/08) ──────────────────────────────

const addTrait = `
struct Vec2 { x: i64 }
trait Add { (_+_): (Self, Self) -> Self }
impl Add for Vec2 { (_+_) = (self, o) => Vec2 { x: self.x + o.x } }
`

func TestOperator_ThroughAWhereBound(t *testing.T) {
	assertNoErrors(t, parseCollectAndCheck(t, addTrait+`
let total<t> where t: Add = (a: t, b: t) -> t => a + b
let ok = total(Vec2 { x: 1 }, Vec2 { x: 2 })
`, false))
}

// A bound whose trait does not declare the operator is not enough, and the message says
// what to add rather than restating that a type parameter is not numeric.
func TestOperator_BoundWithoutTheOperatorIsRejected(t *testing.T) {
	res := parseCollectAndCheck(t, `
trait Sized { size: (Self) -> i64 }
let f<t> where t: Sized = (a: t, b: t) -> t => a + b
`, false)
	assertHasErrorContaining(t, res, "needs a `where t: Trait` bound whose trait declares `(_+_)`")
}

// The bound is enforced at the instantiation by the machinery that already existed, so
// nothing in operator dispatch has to re-ask whether `t` implements the trait.
func TestOperator_BoundUnsatisfiedAtInstantiation(t *testing.T) {
	res := parseCollectAndCheck(t, addTrait+`
struct Other { y: i64 }
let total<t> where t: Add = (a: t, b: t) -> t => a + b
let bad = total(Other { y: 1 }, Other { y: 2 })
`, false)
	assertHasErrorContaining(t, res, "does not implement Add (required by `where t: Add`)")
}

// The operand is checked against the trait's declared signature with Self bound to the
// type *parameter* — so a mixed-operand trait still constrains the right-hand side.
func TestOperator_BoundChecksTheRightOperandAgainstTheSignature(t *testing.T) {
	res := parseCollectAndCheck(t, `
trait Scale { (_*_): (Self, i64) -> Self }
let twice<t> where t: Scale = (a: t, b: t) -> t => a * b
`, false)
	assertHasErrorContaining(t, res, "takes i64 on the right, got t")
}
