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
