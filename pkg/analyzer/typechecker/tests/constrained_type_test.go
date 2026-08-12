package typechecker_test

import "testing"

// checkConstrainedTypeDecl compiles every pattern() constraint on a
// newtype/constrained-type declaration so a malformed regex is reported at
// declaration time rather than first use.

func TestConstrainedType_ValidPattern(t *testing.T) {
	res := parseCollectAndCheck(t, `newtype HexStr = string where pattern(r"^#[0-9a-fA-F]{6}$")`, false)
	assertNoErrors(t, res)
}

func TestConstrainedType_InvalidPatternReported(t *testing.T) {
	// `[` opens a character class that is never closed — an invalid regex.
	res := parseCollectAndCheck(t, `newtype Bad = string where pattern(r"[")`, false)
	assertErrorsAre(t, res,
		`type Bad: invalid pattern constraint r"[": regex parse error at offset 0: unterminated character class`)
}

// The non-pattern constraint kinds (range/values/step/precision) carry no
// declaration-time validation today, so a well-formed constrained type
// type-checks cleanly. These pin that and exercise checkTypeDecl's dispatch
// into the constrained-type branch.

func TestConstrainedType_RangeNoErrors(t *testing.T) {
	res := parseCollectAndCheck(t, `newtype Angle = f64 where range(0.0..<360.0)`, false)
	assertNoErrors(t, res)
}

func TestConstrainedType_ValuesNoErrors(t *testing.T) {
	res := parseCollectAndCheck(t, `newtype Status = i32 where values(200, 404, 500)`, false)
	assertNoErrors(t, res)
}

func TestConstrainedType_StepConstraintNoErrors(t *testing.T) {
	res := parseCollectAndCheck(t, `newtype Heading = i64 where range(0..<360), step(15)`, false)
	assertNoErrors(t, res)
}

func TestConstrainedType_PrecisionConstraintNoErrors(t *testing.T) {
	res := parseCollectAndCheck(t, `newtype Ratio = f64 where range(0.0..<1.0), precision(0.01)`, false)
	assertNoErrors(t, res)
}

// ── range-constraint value enforcement (lyra-E023) ───────────────────────────
//
// A compile-time numeric constant assigned to a range-constrained newtype must
// fall within the declared range.

func TestRangeConstraint_IntAboveInclusiveEnd(t *testing.T) {
	res := parseCollectAndCheck(t, `newtype Percent = u8 where range(0..<=100)
let p: Percent = 150`, false)
	assertErrorsAre(t, res, "p: value 150 is outside the range 0..<=100 of Percent")
}

func TestRangeConstraint_IntBelowStart(t *testing.T) {
	res := parseCollectAndCheck(t, `newtype Grade = i32 where range(1..<=5)
let g: Grade = 0`, false)
	assertErrorsAre(t, res, "g: value 0 is outside the range 1..<=5 of Grade")
}

func TestRangeConstraint_IntInRange_NoError(t *testing.T) {
	res := parseCollectAndCheck(t, `newtype Percent = u8 where range(0..<=100)
let p: Percent = 50`, false)
	assertNoErrors(t, res)
}

// The exclusive end `..<`: the end value itself is out of range, the one below is in.
func TestRangeConstraint_ExclusiveEndBoundary(t *testing.T) {
	res := parseCollectAndCheck(t, `newtype Angle = i32 where range(0..<360)
let a: Angle = 360`, false)
	assertErrorsAre(t, res, "a: value 360 is outside the range 0..<360 of Angle")
}

func TestRangeConstraint_ExclusiveEndInRange_NoError(t *testing.T) {
	res := parseCollectAndCheck(t, `newtype Angle = i32 where range(0..<360)
let a: Angle = 359`, false)
	assertNoErrors(t, res)
}

// Open-ended bounds: only a lower / only an upper bound.
func TestRangeConstraint_OpenLowerBound(t *testing.T) {
	res := parseCollectAndCheck(t, `newtype NonNeg = i32 where range(0..)
let n: NonNeg = -5`, false)
	assertErrorsAre(t, res, "n: value -5 is outside the range 0.. of NonNeg")
}

func TestRangeConstraint_OpenUpperBound(t *testing.T) {
	res := parseCollectAndCheck(t, `newtype Small = i32 where range(..<=100)
let s: Small = 150`, false)
	assertErrorsAre(t, res, "s: value 150 is outside the range ..<=100 of Small")
}

// A negative start via a negated-literal bound.
func TestRangeConstraint_NegativeStart(t *testing.T) {
	res := parseCollectAndCheck(t, `newtype Temp = i32 where range(-40..<=50)
let t2: Temp = -50`, false)
	assertErrorsAre(t, res, "t2: value -50 is outside the range -40..<=50 of Temp")
}

func TestRangeConstraint_FloatAboveRange(t *testing.T) {
	res := parseCollectAndCheck(t, `newtype Ratio = f64 where range(0..<=1)
let r: Ratio = 1.5`, false)
	assertErrorsAre(t, res, "r: value 1.5 is outside the range 0..<=1 of Ratio")
}

func TestRangeConstraint_FloatInRange_NoError(t *testing.T) {
	res := parseCollectAndCheck(t, `newtype Ratio = f64 where range(0..<=1)
let r: Ratio = 0.5`, false)
	assertNoErrors(t, res)
}

// A non-constant value is not checked at compile time (a future flow-sensitive
// pass / the runtime owns it) — no false positive.
func TestRangeConstraint_NonConstant_NoError(t *testing.T) {
	res := parseCollectAndCheck(t, `newtype Percent = u8 where range(0..<=100)
let f = (x: u8) -> Percent => x`, false)
	assertNoErrors(t, res)
}

// Reassigning an out-of-range constant to a constrained var is also enforced.
func TestRangeConstraint_Reassignment(t *testing.T) {
	res := parseCollectAndCheck(t, `newtype Percent = u8 where range(0..<=100)
let f = () -> u8 => {
	var p: Percent = 50
	p = 200
	0
}`, false)
	assertErrorsAre(t, res, "p: value 200 is outside the range 0..<=100 of Percent")
}

// ── nominal isolation ─────────────────────────────────────────────────────────
//
// Two newtypes over the same base are distinct types. Each single-step rule is
// right on its own — a base value is assignable *to* a newtype (construction),
// and a newtype value is assignable to its base (there is no field accessor, so
// that is the only way to read it) — but chaining them made every newtype over a
// common base mutually assignable, which is exactly the mixup a newtype exists to
// prevent.

func TestNewtype_DistinctNewtypesDoNotInterconvert(t *testing.T) {
	res := parseCollectAndCheck(t, `
newtype Meters = i64
newtype Feet = i64
let convert = (m: Meters) -> Feet => m
`, false)
	assertErrorsAre(t, res, "convert: return type mismatch: expected Feet, got Meters")
}

// Construction from the base still works — otherwise a newtype would be
// unbuildable.
func TestNewtype_ConstructionFromBase_Ok(t *testing.T) {
	assertNoErrors(t, parseCollectAndCheck(t, `
newtype Meters = i64
let m: Meters = 5
`, false))
}

// Reading the value back out still works — there is no `.0` accessor, so
// blocking this would make a newtype write-only.
func TestNewtype_ToBase_Ok(t *testing.T) {
	assertNoErrors(t, parseCollectAndCheck(t, `
newtype Meters = i64
let m: Meters = 5
let raw: i64 = m
`, false))
}

// The same newtype is of course assignable to itself.
func TestNewtype_SameType_Ok(t *testing.T) {
	assertNoErrors(t, parseCollectAndCheck(t, `
newtype Meters = i64
let a: Meters = 5
let b: Meters = a
`, false))
}

// Distinctness holds through a call argument too, not just an annotation.
func TestNewtype_DistinctAtCallSite_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `
newtype Meters = i64
newtype Feet = i64
let takesFeet = (f: Feet) -> i64 => 0
let m: Meters = 5
let x = takesFeet(m)
`, false)
	assertErrorsAre(t, res, "takesFeet: argument 1 (f): cannot assign Meters to Feet")
}

// ── crossing a function or field boundary ────────────────────────────────────
//
// A declared return type or field type that *names* a type is stored as an
// UnresolvedType, and an unresolved name compared unequal to the same type
// resolved from an annotation — so a newtype (and, identically, a struct)
// couldn't survive a round trip through a function or a field read. The reported
// error was the tell: "cannot assign Meters to Meters".

func TestNewtype_ThroughCallReturn_Ok(t *testing.T) {
	assertNoErrors(t, parseCollectAndCheck(t, `
newtype Meters = i64
let mk = () -> Meters => 5
let m: Meters = mk()
let raw: i64 = m
`, false))
}

// The same gap hit every named type, not just newtypes.
func TestStruct_ThroughCallReturn_Ok(t *testing.T) {
	assertNoErrors(t, parseCollectAndCheck(t, `
struct Point { x: i64 }
let mk = () -> Point => Point { x: 1 }
let p: Point = mk()
let v: i64 = p.x
`, false))
}

func TestNewtype_ReadFromStructField_Ok(t *testing.T) {
	assertNoErrors(t, parseCollectAndCheck(t, `
newtype Meters = i64
struct Trip { dist: Meters }
let t = Trip { dist: 5 }
let d: i64 = t.dist
`, false))
}

// Resolving those types must not weaken distinctness: a Meters-returning call is
// still not a Feet.
func TestNewtype_DistinctThroughCallReturn_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `
newtype Meters = i64
newtype Feet = i64
let mk = () -> Meters => 5
let f: Feet = mk()
`, false)
	assertErrorsAre(t, res, "f: cannot assign Meters to Feet")
}

// ── the base type's own range still applies ──────────────────────────────────
//
// A newtype narrows its base; it never widens it. With no range constraint of
// its own there was no check at all, so an out-of-range constant sailed through
// the front end and reached codegen — where it would be silently truncated into
// the base's width.

func TestNewtype_LiteralOverflowsBase_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `
newtype Small = u8
let s: Small = 300
`, false)
	assertErrorsAre(t, res, "s: literal value 300 overflows u8")
}

// A folded constant expression is checked the same way.
func TestNewtype_FoldedConstantOverflowsBase_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `
newtype Small = u8
let s: Small = 200 + 100
`, false)
	assertErrorsAre(t, res, "s: literal value 300 overflows u8")
}

// A value inside the base's range is fine.
func TestNewtype_LiteralWithinBase_Ok(t *testing.T) {
	assertNoErrors(t, parseCollectAndCheck(t, `
newtype Small = u8
let s: Small = 200
`, false))
}

// When the newtype *does* declare a range, that constraint owns the report — the
// two checks must not both fire on one mistake (the constraint is a subset of
// the base, so a violation of it subsumes any base overflow).
func TestNewtype_RangeConstraintOwnsTheReport(t *testing.T) {
	res := parseCollectAndCheck(t, `
newtype Percent = u8 where range(0..<=100)
let p: Percent = 300
`, false)
	assertErrorsAre(t, res, "p: value 300 is outside the range 0..<=100 of Percent")
}

// ── the base must be structural ──────────────────────────────────────────────
//
// `newtype` gives nominal identity to a type that has none. A struct, a `data`
// type and a *named* tuple already have their own, so wrapping one buys a second
// name and nothing else — and neither had ever been usable (a struct base could
// not be constructed by any spelling; a data base crashed the backend).

func TestNewtype_StructBase_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `
struct Pt { x: i64, y: i64 }
newtype WrapS = Pt
`, false)
	assertHasErrorContaining(t, res, "Pt is a struct, which already has its own identity")
}

func TestNewtype_DataBase_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `
data Color = Red | Green
newtype WrapD = Color
`, false)
	assertHasErrorContaining(t, res, "is a `data` type, which already has its own identity")
}

// An anonymous tuple is refused too, and for the sharper reason: `tuple Rgb(...)`
// already names a product, so the two differ only in whether the name is a
// constructor. The message shows the `tuple` line to write.
func TestNewtype_AnonymousTupleBase_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `
newtype Rgb = (u8, u8, u8)
`, false)
	assertHasErrorContaining(t, res, "write `tuple Rgb(u8, u8, u8)` instead")
}

func TestNewtype_NamedTupleBase_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `
tuple Pair(i64, i64)
newtype WrapT = Pair
`, false)
	assertHasErrorContaining(t, res, "is a named tuple, which already has its own identity")
}

// A chain is reported at the newtype the author can fix, not at the one below it.
func TestNewtype_ChainedStructBase_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `
struct Pt { x: i64, y: i64 }
newtype A = Pt
newtype B = A
`, false)
	assertHasErrorContaining(t, res, "is a struct, which already has its own identity")
}

// The structural bases keep working — this is the feature, not a casualty of it.
func TestNewtype_StructuralBases_Ok(t *testing.T) {
	assertNoErrors(t, parseCollectAndCheck(t, `
newtype Meters = f64
newtype Name = string
newtype Grid = [3]i64
newtype Handle = ^u8
`, false))
}

// ── a newtype is transparent to its base's methods ───────────────────────────

func TestNewtype_StringBase_HasStringMethods(t *testing.T) {
	assertNoErrors(t, parseCollectAndCheck(t, `
newtype Name = string
let n: Name = "abc"
let a = n.len()
let b = n.slice(0, 2)
`, true))
}

// A method written *for* the newtype wins over the base's, matching the
// user-code-beats-builtin ordering everywhere else.
func TestNewtype_OwnMethodBeatsBaseMethod(t *testing.T) {
	res := parseCollectAndCheck(t, `
newtype Name = string
pub let len = (self: Name) -> string => "mine"
let n: Name = "abc"
let a: string = n.len()
`, true)
	assertNoErrors(t, res)
}

// A UFCS function taking the base receives the newtype through the same fallback.
func TestNewtype_BaseUFCSFunctionReachable(t *testing.T) {
	assertNoErrors(t, parseCollectAndCheck(t, `
newtype Meters = f64
pub let describe = (self: f64) -> string => "float"
let m: Meters = 1.5
let d = m.describe()
`, false))
}

// ── a newtype has no constructor call (lyra-E044) ────────────────────────────
//
// `Cents(150)` parses as a named-tuple literal, so it used to report "Cents: not a
// tuple type" — true, useless, and naming a concept the author did not write. The
// language constructs a newtype by *annotation*, since a value satisfying the base is
// assignable to the newtype, and the message now says so. Same fix lyra-E035 applied
// to `Rng.seeded(42)`: name what the language has rather than what the parse was.
//
// Whether newtypes *should* have constructors is open (todo.md) — the spelling alone
// would be a third way to say what annotation already says, and is only worth adding
// as part of making construction explicit. These pin today's rules, not that decision.

func TestNewtype_ConstructorCallRefused(t *testing.T) {
	res := parseCollectAndCheck(t, `
newtype Cents = i64
let c = Cents(150)
`, false)
	assertErrorsAre(t, res,
		"Cents is a newtype over i64 and has no constructor: write the i64 value where a Cents is expected (`let x: Cents = ...`, a parameter, or a return), which is how a Cents is made")
}

// The juxtaposed spelling reaches the same collector path, so it gets the same message
// rather than falling through to the tuple one.
func TestNewtype_JuxtaposedConstructorRefused(t *testing.T) {
	res := parseCollectAndCheck(t, `
newtype Cents = i64
let c = Cents 150
`, false)
	assertErrorsAre(t, res,
		"Cents is a newtype over i64 and has no constructor: write the i64 value where a Cents is expected (`let x: Cents = ...`, a parameter, or a return), which is how a Cents is made")
}

// A genuine named tuple is untouched — the new arm keys on the *declaration* being a
// newtype, not on the literal's shape.
func TestNamedTuple_StillConstructs(t *testing.T) {
	assertNoErrors(t, parseCollectAndCheck(t, `
tuple Point(i64, i64)
let p = Point(1, 2)
`, false))
}

// ── the overflow-arithmetic family stops at the wrapper (lyra-E043) ──────────
//
// Arithmetic on a newtype is opt-in: `Cents + Cents` is refused until the type has
// an operator impl, and `wrapping_*`/`saturating_*`/`checked_*` are those operators'
// escape hatches — so reaching them through the transparency fallback handed out
// exactly the arithmetic the operator rule withholds. The sharpest case was the
// mixed operand: the base-typed parameter accepted `cents.wrapping_add(plain_i64)`,
// the silent unit-mixup a newtype exists to prevent, while the checked `+` refused
// even two Cents. Found by the 08/12 audit; both spellings now require the same
// explicitness.

func TestNewtype_WrappingArithmeticRefused(t *testing.T) {
	res := parseCollectAndCheck(t, `
newtype Cents = i64
let a: Cents = 150
let b: Cents = 275
let c = a.wrapping_add(b)
`, false)
	assertErrorsAre(t, res,
		"arithmetic on a newtype is opt-in: Cents is nominal over i64, so \"wrapping_add\" does not reach through the wrapper — give Cents an operator impl, or read the value into its base (`let raw: i64 = ...`) and operate there")
}

// The mixed operand — the case the bypass made silently legal.
func TestNewtype_MixedOperandArithmeticRefused(t *testing.T) {
	res := parseCollectAndCheck(t, `
newtype Cents = i64
let a: Cents = 150
let plain: i64 = 5
let c = a.wrapping_add(plain)
`, false)
	assertErrorsAre(t, res,
		"arithmetic on a newtype is opt-in: Cents is nominal over i64, so \"wrapping_add\" does not reach through the wrapper — give Cents an operator impl, or read the value into its base (`let raw: i64 = ...`) and operate there")
}

// checked_* is the third member of the family and is refused the same way.
func TestNewtype_CheckedArithmeticRefused(t *testing.T) {
	res := parseCollectAndCheck(t, `
newtype Cents = i64
let a: Cents = 150
let b: Cents = 275
let c = a.checked_add(b)
`, true)
	assertErrorsAre(t, res,
		"arithmetic on a newtype is opt-in: Cents is nominal over i64, so \"checked_add\" does not reach through the wrapper — give Cents an operator impl, or read the value into its base (`let raw: i64 = ...`) and operate there")
}

// The refusal is exactly the overflow-arithmetic family. The float rounding ops are
// `i64(x)`'s alternative rather than an operator's, so they stay transparent — as do
// the string methods the transparency rule was argued for in the first place.
func TestNewtype_FloatRoundingStaysTransparent(t *testing.T) {
	assertNoErrors(t, parseCollectAndCheck(t, `
newtype Meters = f64
let m: Meters = 1.5
let f = m.floor()
`, false))
}

// The read-out escape the E043 message names: one-step newtype → base assignment is
// documented assignability, and the base value then has the whole family.
func TestNewtype_BaseReadoutReachesArithmetic(t *testing.T) {
	assertNoErrors(t, parseCollectAndCheck(t, `
newtype Cents = i64
let a: Cents = 150
let raw: i64 = a
let c = raw.wrapping_add(5)
`, false))
}

// The other path the E043 message names must actually exist: an operator impl on a
// scalar newtype dispatches. The dispatch guard used to newtype-strip its receiver
// before refusing scalar receivers, which made a scalar newtype operator-dead from
// both sides — the numeric rule refused the nominal type and the guard refused the
// base — so `impl Add for Cents` was silently inert. Fixed alongside E043 (08/12);
// `impl Add for i64` staying inert is pinned by operator_overload_test.go.
//
// The impl yields a `Cents`, not the base it computed in — a newtype whose `+` hands
// back an i64 would defeat the point of declaring it — and the chained `x + y + x` is
// what pins that: the second `+` needs a Cents on its left, where an i64 result is
// refused ("operands must be numeric, got i64 and Cents"). A `let sum: Cents`
// annotation alone would not pin it, since base → newtype is assignable anyway.
func TestNewtype_OperatorImplDispatches(t *testing.T) {
	assertNoErrors(t, parseCollectAndCheck(t, `
newtype Cents = i64
trait Add { (_+_): (Self, Self) -> Self }
impl Add for Cents {
  (_+_) = (self, o) => {
    let a: i64 = self
    let b: i64 = o
    let sum: Cents = a.wrapping_add(b)
    sum
  }
}
let x: Cents = 150
let y: Cents = 275
let total: Cents = x + y + x
`, false))
}
