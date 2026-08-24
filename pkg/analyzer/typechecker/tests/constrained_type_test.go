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
	assertErrorsAre(t, res, "value 150 is outside the range 0..<=100 of Percent")
}

func TestRangeConstraint_IntBelowStart(t *testing.T) {
	res := parseCollectAndCheck(t, `newtype Grade = i32 where range(1..<=5)
let g: Grade = 0`, false)
	assertErrorsAre(t, res, "value 0 is outside the range 1..<=5 of Grade")
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
	assertErrorsAre(t, res, "value 360 is outside the range 0..<360 of Angle")
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
	assertErrorsAre(t, res, "value -5 is outside the range 0.. of NonNeg")
}

func TestRangeConstraint_OpenUpperBound(t *testing.T) {
	res := parseCollectAndCheck(t, `newtype Small = i32 where range(..<=100)
let s: Small = 150`, false)
	assertErrorsAre(t, res, "value 150 is outside the range ..<=100 of Small")
}

// A negative start via a negated-literal bound.
func TestRangeConstraint_NegativeStart(t *testing.T) {
	res := parseCollectAndCheck(t, `newtype Temp = i32 where range(-40..<=50)
let t2: Temp = -50`, false)
	assertErrorsAre(t, res, "value -50 is outside the range -40..<=50 of Temp")
}

func TestRangeConstraint_FloatAboveRange(t *testing.T) {
	res := parseCollectAndCheck(t, `newtype Ratio = f64 where range(0..<=1)
let r: Ratio = 1.5`, false)
	assertErrorsAre(t, res, "value 1.5 is outside the range 0..<=1 of Ratio")
}

func TestRangeConstraint_FloatInRange_NoError(t *testing.T) {
	res := parseCollectAndCheck(t, `newtype Ratio = f64 where range(0..<=1)
let r: Ratio = 0.5`, false)
	assertNoErrors(t, res)
}

// A non-constant value is not checked at compile time (the value-range pass / the
// runtime owns it) — no false positive. Written through the constructor because a
// typed value no longer converts implicitly (lyra-E046); the two rules meet here, and
// the point survives: `Percent(x)` for a runtime x is accepted and unchecked.
func TestRangeConstraint_NonConstant_NoError(t *testing.T) {
	res := parseCollectAndCheck(t, `newtype Percent = u8 where range(0..<=100)
let f = (x: u8) -> Percent => Percent(x)`, false)
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
	assertErrorsAre(t, res, "value 200 is outside the range 0..<=100 of Percent")
}

// ── the constraint follows the type, not the binding (08/12) ─────────────────
//
// The three constraint checks used to be called from the assignment sites only, so
// `let p: Percent = 150` was caught and the *same literal* reaching the *same newtype*
// through an argument, a return or an array element was not — silently, in the feature
// whose entire purpose is to be checked. They ride propagateLiteralType now, which is
// the one point a newtype context reaches a value in every position it can arrive from.
//
// Each of these asserts the *exact* error set, so a duplicate report would fail them
// too — the guard that matters, since a leaf can be narrowed by more than one context.

func TestConstraint_CheckedInArgumentPosition(t *testing.T) {
	res := parseCollectAndCheck(t, `newtype Percent = u8 where range(0..<=100)
let show_it = (p: Percent) -> i64 => 1
let x = show_it(150)`, false)
	assertErrorsAre(t, res, "value 150 is outside the range 0..<=100 of Percent")
}

func TestConstraint_CheckedInReturnPosition(t *testing.T) {
	res := parseCollectAndCheck(t, `newtype Percent = u8 where range(0..<=100)
let make = () -> Percent => 150`, false)
	assertErrorsAre(t, res, "value 150 is outside the range 0..<=100 of Percent")
}

func TestConstraint_CheckedInArrayElementPosition(t *testing.T) {
	res := parseCollectAndCheck(t, `newtype Percent = u8 where range(0..<=100)
let xs: []Percent = [10, 150, 20]`, false)
	assertErrorsAre(t, res, "value 150 is outside the range 0..<=100 of Percent")
}

// A pattern constraint travels the same path, so it is no longer annotation-only either.
func TestConstraint_PatternCheckedInArgumentPosition(t *testing.T) {
	res := parseCollectAndCheck(t, `newtype Hex = string where pattern(r"^#[0-9a-fA-F]{6}$")
let paint = (c: Hex) -> i64 => 1
let x = paint("nope")`, false)
	assertErrorsAre(t, res,
		`value "nope" does not satisfy pattern constraint r"^#[0-9a-fA-F]{6}$" of Hex`)
}

// The pattern message used to wrap an already-delimited pattern in a second `r"…"`,
// reading `pattern constraint r"r"^#…$""`. Pinned by the assertion above and here.
func TestConstraint_PatternMessageIsNotDoubleQuoted(t *testing.T) {
	res := parseCollectAndCheck(t, `newtype Hex = string where pattern(r"^#[0-9a-fA-F]{6}$")
let h: Hex = "nope"`, false)
	assertErrorsAre(t, res,
		`value "nope" does not satisfy pattern constraint r"^#[0-9a-fA-F]{6}$" of Hex`)
}

// ── values(...) is enforced (lyra-E045) ──────────────────────────────────────
//
// Nothing read LiteralUnionConstraint until 08/12: the constraint was collected, its
// shape validated, and then ignored, so `let s: Status = 302` compiled clean. The
// collected-and-unread shape, in the one place where being checked is the whole point
// of writing the declaration.

func TestValuesConstraint_ViolationReported(t *testing.T) {
	res := parseCollectAndCheck(t, `newtype Status = i32 where values(200, 404, 500)
let s: Status = 302`, false)
	assertErrorsAre(t, res, "value 302 is not one of the values allowed by Status (200, 404, 500)")
}

func TestValuesConstraint_AllowedValueOk(t *testing.T) {
	assertNoErrors(t, parseCollectAndCheck(t, `newtype Status = i32 where values(200, 404, 500)
let s: Status = 404`, false))
}

// It rides the same path, so it reaches an argument too.
func TestValuesConstraint_CheckedInArgumentPosition(t *testing.T) {
	res := parseCollectAndCheck(t, `newtype Status = i32 where values(200, 404, 500)
let handle = (s: Status) -> i64 => 1
let x = handle(302)`, false)
	assertErrorsAre(t, res, "value 302 is not one of the values allowed by Status (200, 404, 500)")
}

// A string union compares by value, not by source spelling.
func TestValuesConstraint_StringUnion(t *testing.T) {
	res := parseCollectAndCheck(t, `newtype Mode = string where values("r", "w")
let m: Mode = "x"`, false)
	assertErrorsAre(t, res, `value "x" is not one of the values allowed by Mode ("r", "w")`)
}

// A non-constant is skipped, as everywhere else in this file — definite-only, so
// never a false positive.
func TestValuesConstraint_NonConstantSkipped(t *testing.T) {
	assertNoErrors(t, parseCollectAndCheck(t, `newtype Status = i32 where values(200, 404, 500)
let f = (x: i32) -> Status => Status(x)`, false))
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

// Reading the value back out is the base-name conversion (08/12): `i64(m)`, the
// constructor's mirror and an identity at runtime just as the constructor is. There
// is no `.0` accessor, so this spelling existing is what lets the *implicit* form be
// refused (lyra-E047) without making a newtype write-only.
func TestNewtype_ToBase_Ok(t *testing.T) {
	assertNoErrors(t, parseCollectAndCheck(t, `
newtype Meters = i64
let m: Meters = 5
let raw = i64(m)
let sum = raw + 1
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
let raw = i64(m)
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
let d = i64(t.dist)
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
	assertErrorsAre(t, res, "value 300 is outside the range 0..<=100 of Percent")
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
`, false))
}

// A method written *for* the newtype wins over the base's, matching the
// user-code-beats-builtin ordering everywhere else.
func TestNewtype_OwnMethodBeatsBaseMethod(t *testing.T) {
	res := parseCollectAndCheck(t, `
newtype Name = string
pub let len = (self: Name) -> string => "mine"
let n: Name = "abc"
let a: string = n.len()
`, false)
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

// ── a newtype has a constructor (08/12) ──────────────────────────────────────
//
// `Cents(150)` constructs, and so does the juxtaposed `Cents 150` — the collector
// erases that spelling into this same node, so both forms cost one arm. It is a
// compile-time assertion about which type a value has, not a wrapper: a newtype is
// nominal to the typechecker and transparent to codegen, so it lowers to its operand
// and nothing else.
//
// Until 08/12 this was `lyra-E044`, "a newtype has no constructor", after a shorter
// period reporting "Cents: not a tuple type" — which named the parse rather than the
// language. E044 now covers what is still malformed: the wrong operand count, an
// operand the base cannot hold, and a generic newtype.

func TestNewtype_ConstructorCall(t *testing.T) {
	assertNoErrors(t, parseCollectAndCheck(t, `
newtype Cents = i64
let c = Cents(150)
let raw = i64(c)
`, false))
}

// The juxtaposed spelling reaches the same collector path, so it works for free.
func TestNewtype_JuxtaposedConstructor(t *testing.T) {
	assertNoErrors(t, parseCollectAndCheck(t, `
newtype Cents = i64
let c = Cents 150
let raw = i64(c)
`, false))
}

// The constructor's whole point is a position with no annotation to infer from, and
// the result is nominally a Cents rather than its base — pinned by a *second* newtype
// over the same base refusing it.
func TestNewtype_ConstructorYieldsTheNewtype(t *testing.T) {
	res := parseCollectAndCheck(t, `
newtype Cents = i64
newtype Feet = i64
let takesFeet = (f: Feet) -> i64 => 0
let x = takesFeet(Cents(150))
`, false)
	assertErrorsAre(t, res, "takesFeet: argument 1 (f): cannot assign Cents to Feet")
}

// A newtype names exactly one base, so it takes exactly one operand.
func TestNewtype_ConstructorArity(t *testing.T) {
	res := parseCollectAndCheck(t, `
newtype Cents = i64
let c = Cents(1, 2)
`, false)
	assertErrorsAre(t, res, "Cents is a newtype over i64, so it takes exactly one operand, not 2")
}

// The operand is checked against the base.
func TestNewtype_ConstructorOperandMustFitTheBase(t *testing.T) {
	res := parseCollectAndCheck(t, `
newtype Cents = i64
let c = Cents("hi")
`, false)
	assertErrorsAre(t, res, "cannot construct Cents from string: its base is i64")
}

// Constraints are enforced *through* the constructor, because it propagates the
// newtype onto its operand rather than the base — so this is the same report
// `let p: Percent = 150` gets, from one predicate.
func TestNewtype_ConstructorEnforcesConstraints(t *testing.T) {
	res := parseCollectAndCheck(t, `newtype Percent = u8 where range(0..<=100)
let p = Percent(150)`, false)
	assertErrorsAre(t, res, "value 150 is outside the range 0..<=100 of Percent")
}

// A *generic* newtype constructs by call (08/12): the parameters are solved from the
// operand through the same solver a named tuple's instantiation uses, so `Boxed(5)`
// is `Boxed<i64>` exactly as `Some(5)` is `Maybe<i64>` — the untyped operand promotes
// to its default, and a narrower instantiation is reached by saying so
// (`Boxed(u8(7))`). This arm was refused outright at first ("annotate instead"): the
// base is a type variable with nothing to check the operand against — which was a
// missing solver, not a missing answer.
func TestNewtype_GenericConstructorSolves(t *testing.T) {
	assertNoErrors(t, parseCollectAndCheck(t, `
newtype Boxed<t> = t
let a = Boxed(5)
let ra = i64(a)
let b = Boxed("hi")
let rb = string(b)
let c = Boxed(u8(7))
let rc = u8(c)
`, false))
}

// The turbofish binds the parameters explicitly — `::<>`, the spelling the grammar
// has for a constructor's type arguments.
func TestNewtype_GenericConstructorTurbofish(t *testing.T) {
	assertNoErrors(t, parseCollectAndCheck(t, `
newtype Boxed<t> = t
let c = Boxed::<u8>(200)
let rc = u8(c)
`, false))
}

// The solved result is nominal, exactly as a concrete newtype's construction is —
// solving the parameter must not weaken the identity it parameterizes.
func TestNewtype_GenericConstructorResultIsNominal(t *testing.T) {
	res := parseCollectAndCheck(t, `
newtype Boxed<t> = t
let takes = (n: i64) -> i64 => n
let b = Boxed(5)
let x = takes(b)
`, false)
	assertErrorsAre(t, res,
		"cannot use Boxed as i64 implicitly: reading a newtype out discards the name it carries, so the conversion must be written — `i64(...)`")
}

// A parameter the base never mentions cannot be solved from any operand — only the
// turbofish can bind it, and the message says so.
func TestNewtype_GenericConstructorUnsolvableParam(t *testing.T) {
	res := parseCollectAndCheck(t, `
newtype Weird<t> = i64
let w = Weird(5)
`, false)
	assertErrorsAre(t, res,
		"Weird: cannot infer t from the operand — write the type arguments (`Weird<...>(...)`)")
}

// A genuine named tuple is untouched — the arm keys on the *declaration* being a
// newtype, not on the literal's shape.
func TestNamedTuple_StillConstructs(t *testing.T) {
	assertNoErrors(t, parseCollectAndCheck(t, `
tuple Point(i64, i64)
let p = Point(1, 2)
`, false))
}

// ── a typed value needs the constructor; a literal does not (lyra-E046) ──────
//
// The rule that gives a newtype its meaning at a boundary. Until 08/12 base → newtype
// was assignable everywhere, so `let plain: i64 = 150` followed by `take(plain)`
// compiled silently and a newtype declared a distinction the compiler then declined to
// enforce anywhere it mattered.
//
// The line is provenance, not convenience: a literal has no unit yet, a typed value came
// from somewhere, and that somewhere is where a unit mixup lives. Ada's rule for derived
// types, for the same reason.

func TestImplicitNewtype_TypedValueRefusedAtCall(t *testing.T) {
	res := parseCollectAndCheck(t, `
newtype Cents = i64
let take = (c: Cents) -> i64 => 0
let plain: i64 = 150
let x = take(plain)
`, false)
	assertErrorsAre(t, res,
		"cannot use i64 as Cents implicitly: Cents is a distinct type over i64, so the conversion must be written — `Cents(...)`")
}

func TestImplicitNewtype_TypedValueRefusedAtBinding(t *testing.T) {
	res := parseCollectAndCheck(t, `
newtype Cents = i64
let plain: i64 = 150
let c: Cents = plain
`, false)
	assertErrorsAre(t, res,
		"cannot use i64 as Cents implicitly: Cents is a distinct type over i64, so the conversion must be written — `Cents(...)`")
}

func TestImplicitNewtype_TypedValueRefusedAtReturn(t *testing.T) {
	res := parseCollectAndCheck(t, `
newtype Cents = i64
let widen = (n: i64) -> Cents => n
`, false)
	assertErrorsAre(t, res,
		"cannot use i64 as Cents implicitly: Cents is a distinct type over i64, so the conversion must be written — `Cents(...)`")
}

// The constructor is the way through, and it is accepted in every one of those spots.
func TestImplicitNewtype_ConstructorIsTheWayThrough(t *testing.T) {
	assertNoErrors(t, parseCollectAndCheck(t, `
newtype Cents = i64
let take = (c: Cents) -> i64 => 0
let widen = (n: i64) -> Cents => Cents(n)
let plain: i64 = 150
let c: Cents = Cents(plain)
let x = take(Cents(plain))
`, false))
}

// An untyped literal still converts implicitly — the whole point of drawing the line
// here rather than requiring the constructor everywhere. Constant arithmetic is covered
// by the same clause, because the sum of two untyped literals is still untyped.
func TestImplicitNewtype_LiteralsStillImplicit(t *testing.T) {
	assertNoErrors(t, parseCollectAndCheck(t, `
newtype Cents = i64
let take = (c: Cents) -> i64 => 0
let a: Cents = 150
let b: Cents = 100 + 50
let c: Cents = -5
let x = take(200)
let make = () -> Cents => 999
`, false))
}

// The literals the *type* system cannot identify — a string or bool literal has the
// same type as a variable holding one, so only the syntax says it is a constant. This
// is why the rule reads the expression and not just the type.
func TestImplicitNewtype_NonNumericLiteralsStillImplicit(t *testing.T) {
	assertNoErrors(t, parseCollectAndCheck(t, `
newtype Name = string
newtype Flag = bool
newtype Row = []i64
let n: Name = "abc"
let f: Flag = true
let r: Row = [1, 2, 3]
`, false))
}

// A *computed* string is not a literal, so it needs the constructor even though a
// string literal does not. That asymmetry is the rule working, not an edge: `a ++ b`
// has provenance in a way `"abc"` does not.
func TestImplicitNewtype_ComputedStringNeedsTheConstructor(t *testing.T) {
	res := parseCollectAndCheck(t, `
newtype Email = string
let mk = (a: string, b: string) -> Email => a ++ b
`, false)
	assertErrorsAre(t, res,
		"cannot use string as Email implicitly: Email is a distinct type over string, so the conversion must be written — `Email(...)`")
}

// ── reading out needs the conversion too (lyra-E047) ─────────────────────────
//
// E046's mirror, closed the same day it was left open: `let raw: i64 = c` and
// `f(cents)` against `(x: i64)` silently discarded the name the newtype carries,
// which is the same unit-mixup shape in the other direction. The spelling is the
// base's own name applied — `i64(c)`, `string(e)` — the constructor's mirror, and
// an identity at runtime just as the constructor is. Unlike E046 there is no
// literal half: a newtype value is never a literal, so the refusal is
// unconditional where the base is nameable.

func TestImplicitReadout_RefusedAtBinding(t *testing.T) {
	res := parseCollectAndCheck(t, `
newtype Cents = i64
let c: Cents = 150
let raw: i64 = c
`, false)
	assertErrorsAre(t, res,
		"cannot use Cents as i64 implicitly: reading a newtype out discards the name it carries, so the conversion must be written — `i64(...)`")
}

func TestImplicitReadout_RefusedAtCall(t *testing.T) {
	res := parseCollectAndCheck(t, `
newtype Cents = i64
let take = (x: i64) -> i64 => x
let c: Cents = 150
let r = take(c)
`, false)
	assertErrorsAre(t, res,
		"cannot use Cents as i64 implicitly: reading a newtype out discards the name it carries, so the conversion must be written — `i64(...)`")
}

func TestImplicitReadout_RefusedAtReturn(t *testing.T) {
	res := parseCollectAndCheck(t, `
newtype Cents = i64
let unwrap = (c: Cents) -> i64 => c
`, false)
	assertErrorsAre(t, res,
		"cannot use Cents as i64 implicitly: reading a newtype out discards the name it carries, so the conversion must be written — `i64(...)`")
}

// The conversion is the way through, in every one of those spots — and for a string
// base the spelling is `string(...)`, which exists exactly for this.
func TestImplicitReadout_ConversionIsTheWayThrough(t *testing.T) {
	assertNoErrors(t, parseCollectAndCheck(t, `
newtype Cents = i64
newtype Email = string
let take = (x: i64) -> i64 => x
let c: Cents = 150
let raw = i64(c)
let r = take(i64(c))
let unwrap = (c2: Cents) -> i64 => i64(c2)
let e: Email = "x@y"
let s = string(e)
`, false))
}

// `string(...)` and `bool(...)` are identity-only: there is no stringification and
// no truthiness, so an operand that is not the target (after the newtype strip) is
// refused, naming that.
func TestIdentityConversion_RefusesNonIdentity(t *testing.T) {
	res := parseCollectAndCheck(t, `
let s = string(42)
`, false)
	assertErrorsAre(t, res,
		"cannot convert integer literal to string: `string(...)` only reads a value of that type — or a newtype over it — back out")
}

// A newtype over a base the conversion cannot *name* — an array, here — keeps its
// implicit read-out: refusing with no spelling to offer would make it write-only.
// The documented limit, pinned so a future spelling knows what to flip.
func TestImplicitReadout_UnnameableBaseStaysImplicit(t *testing.T) {
	assertNoErrors(t, parseCollectAndCheck(t, `
newtype Row = []i64
let r: Row = [1, 2, 3]
let base: []i64 = r
`, false))
}

// A same-newtype flow is not a read-out, including through match/if arms — the walk
// carries the base below a newtype context, and must not mistake that for one.
func TestImplicitReadout_SameNewtypeThroughArms(t *testing.T) {
	assertNoErrors(t, parseCollectAndCheck(t, `
newtype Cents = i64
let pick = (b: bool, c: Cents) -> Cents => if b { c } else { 1 }
`, false))
}

// A value that is already the newtype is not a conversion at all.
func TestImplicitNewtype_SameNewtypeUnaffected(t *testing.T) {
	assertNoErrors(t, parseCollectAndCheck(t, `
newtype Cents = i64
let a: Cents = 150
let b: Cents = a
`, false))
}

// Two newtypes over one base still report the *distinctness* error rather than this
// one — the rule exempts a ConstrainedType source so the two do not double up.
func TestImplicitNewtype_DistinctNewtypeKeepsItsOwnError(t *testing.T) {
	res := parseCollectAndCheck(t, `
newtype Meters = i64
newtype Feet = i64
let convert = (m: Meters) -> Feet => m
`, false)
	assertErrorsAre(t, res, "convert: return type mismatch: expected Feet, got Meters")
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
		"arithmetic on a newtype is opt-in: Cents is nominal over i64, so \"wrapping_add\" does not reach through the wrapper — give Cents an operator impl, or convert to its base (`i64(...)`) and operate there")
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
		"arithmetic on a newtype is opt-in: Cents is nominal over i64, so \"wrapping_add\" does not reach through the wrapper — give Cents an operator impl, or convert to its base (`i64(...)`) and operate there")
}

// checked_* is the third member of the family and is refused the same way.
func TestNewtype_CheckedArithmeticRefused(t *testing.T) {
	res := parseCollectAndCheck(t, `
newtype Cents = i64
let a: Cents = 150
let b: Cents = 275
let c = a.checked_add(b)
`, false)
	assertErrorsAre(t, res,
		"arithmetic on a newtype is opt-in: Cents is nominal over i64, so \"checked_add\" does not reach through the wrapper — give Cents an operator impl, or convert to its base (`i64(...)`) and operate there")
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
let raw = i64(a)
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
    let a = i64(self)
    let b = i64(o)
    let sum: Cents = Cents(a.wrapping_add(b))
    sum
  }
}
let x: Cents = 150
let y: Cents = 275
let total: Cents = x + y + x
`, false))
}

// **A newtype whose base leads back to itself is refused, and refused before anything walks
// it.**
//
// The alias-cycle guard does not cover this: resolution deliberately never descends into a
// newtype's base — a newtype is nominal, so resolving its *name* finishes once the newtype
// is in hand — so a cycle of newtypes never enters the resolution stack that guard watches.
// Nothing else looked, and the declaration was accepted.
//
// What happened next depended on which walk reached it first, and all of them were fatal:
// the walks that *recurse* on the resolved base died with `fatal error: stack overflow`,
// while the ones written as `for` loops never terminated at all — a hung compiler, and a
// hung editor.
//
// Both ends are reported, because each declaration is circular on its own terms and fixing
// either one fixes the program.
func TestNewtype_CircularBase_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `
newtype A = B
newtype B = A
let a: A = 0`, false)
	assertErrorsAre(t, res,
		`newtype "A" is circular: its base leads back to itself`,
		`newtype "B" is circular: its base leads back to itself`,
		"a: cannot assign integer literal to A")
}

// A cycle of any length, and the degenerate one-element cycle.
func TestNewtype_LongerCycleAndSelfCycle_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `
newtype P = Q
newtype Q = R
newtype R = P`, false)
	assertErrorsAre(t, res,
		`newtype "P" is circular: its base leads back to itself`,
		`newtype "Q" is circular: its base leads back to itself`,
		`newtype "R" is circular: its base leads back to itself`)

	res = parseCollectAndCheck(t, `newtype S = S`, false)
	assertErrorsAre(t, res, `newtype "S" is circular: its base leads back to itself`)
}

// A newtype chain that *does* terminate is untouched — the guard must refuse cycles, not
// chains.
func TestNewtype_TerminatingChain_Ok(t *testing.T) {
	res := parseCollectAndCheck(t, `
newtype Meters = f64
newtype Distance = Meters
let f = (d: Distance) -> Distance => d`, false)
	assertNoErrors(t, res)
}
