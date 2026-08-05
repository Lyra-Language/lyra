package types

import "math"

// A range step, and the one definition of which steps are legal.
//
// A step means the same thing in both places it can be written: the values
// covered are start, start+step, start+2*step, … Two spellings express it —
// an expression range's `:step` (`0..<=100:2`) and a `newtype`'s `step()`
// constraint (`newtype Quarter = f32 where range(0..<=100), step(0.25)`).
//
// **They stay separate spellings on purpose.** The constraint form composes with
// `precision()` and with the newtype's own domain, which an inline `:step` cannot;
// the expression form drives a loop counter, which a constraint cannot. What they
// must not do is *mean* different things, and until this file nothing stopped
// them: the expression step was checked for numeric type-compatibility and
// nothing else, while the constraint step was collected and validated by nothing
// at all. Both now ask the same question here.
//
// Note the asymmetry that remains, deliberately: a step *constraint* is not yet
// enforced against values at run time (no pass reads StepConstraint), so
// `step(0.25)` documents and validates but does not yet reject 0.3. That is a
// separate feature from the two spellings agreeing on what a legal step is.

// InvalidStepReason returns a human-readable reason a constant range step is not
// well formed, or "" when it is fine. integerDomain says whether the range's
// values are integers, which is what makes a fractional step meaningless rather
// than merely unusual.
//
// Two rules, both of which hold for either spelling:
//
//   - **Zero never advances.** As an expression step it is a loop that cannot
//     terminate; as a constraint it admits only the start value, which `values()`
//     already says better. Neither is plausibly intended.
//   - **A fractional step over an integer domain is unrepresentable.** `0..<=10:0.5`
//     and `newtype N = u8 where step(0.5)` both describe values the domain cannot
//     hold.
//
// **A negative step is now a rule** (08/04), and the reason it was not before is worth
// keeping: which direction a range ran was a question the language had not answered, so
// judging the sign here would have invented one. `..>`/`..>=` answered it — direction is
// the *operator's*, decided at parse time — which leaves the step as a pure magnitude with
// nothing left for a sign to mean. `10..>=0:-2` is not a descending range written another
// way; it is a contradiction between two things that both claim to say direction, and the
// old reading of it (in an ascending range) was an infinite loop.
func InvalidStepReason(step float64, integerDomain bool) string {
	switch {
	case step == 0:
		return "a step of 0 never advances"
	case step < 0:
		return "a step is a distance, not a direction — write a descending range as `start..>end` or `start..>=end` and give the step its magnitude"
	case math.IsNaN(step) || math.IsInf(step, 0):
		return "a step must be a finite number"
	case integerDomain && step != math.Trunc(step):
		return "a fractional step cannot be represented over an integer range"
	}
	return ""
}

// StepDomainIsInteger reports whether a range over t steps in integers, which is
// what InvalidStepReason's integerDomain argument wants. A nil or non-numeric
// type answers false, so an unknown domain never manufactures an error the
// programmer cannot act on.
func StepDomainIsInteger(t Type) bool {
	primitive, ok := StripNewtype(t).(PrimitiveType)
	if !ok {
		return false
	}
	switch primitive.Name {
	case Int8, Int16, Int32, Int64, Int128,
		UInt8, UInt16, UInt32, UInt64, UInt128,
		UntypedInt, UntypedSignedInt:
		return true
	default:
		// Floats, and every non-numeric type. UntypedFloat is deliberately here:
		// a range whose bounds are float literals steps in floats.
		return false
	}
}
