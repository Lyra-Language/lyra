package types

import "testing"

// InvalidStepReason is the one definition of a legal range step, asked by both
// spellings — an expression range's `:step` and a newtype's `step()` constraint.
// Testing it here rather than only through the two callers is the point: a rule
// with two callers drifts the moment it has two implementations.
func TestInvalidStepReason(t *testing.T) {
	cases := []struct {
		name          string
		step          float64
		integerDomain bool
		wantInvalid   bool
	}{
		{"whole step over integers", 2, true, false},
		{"whole step over floats", 2, false, false},
		{"fractional step over floats", 0.25, false, false},
		{"fractional step over integers", 0.5, true, true},
		{"zero over integers", 0, true, true},
		{"zero over floats", 0, false, true},
		{"negative whole step is not rejected here", -2, true, false},
		{"negative fractional over integers", -0.5, true, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := InvalidStepReason(c.step, c.integerDomain)
			if (got != "") != c.wantInvalid {
				t.Errorf("InvalidStepReason(%v, %v) = %q; wantInvalid=%v",
					c.step, c.integerDomain, got, c.wantInvalid)
			}
		})
	}
}

// A step is checked against the range's *domain*, so which types count as
// integer-stepping is part of the shared rule rather than each caller's guess.
func TestStepDomainIsInteger(t *testing.T) {
	integer := []PrimitiveType{
		{Name: Int8}, {Name: Int64}, {Name: Int128},
		{Name: UInt8}, {Name: UInt64}, {Name: UInt128},
		{Name: UntypedInt}, {Name: UntypedSignedInt},
	}
	for _, ty := range integer {
		if !StepDomainIsInteger(ty) {
			t.Errorf("%s should be an integer step domain", ty.Name)
		}
	}
	// A float domain steps in floats — including an *untyped* float literal
	// range, whose bounds are floats even before a width is settled.
	notInteger := []Type{
		PrimitiveType{Name: Float16}, PrimitiveType{Name: Float32},
		PrimitiveType{Name: Float64}, PrimitiveType{Name: UntypedFloat},
		PrimitiveType{Name: String}, PrimitiveType{Name: Boolean},
		nil,
	}
	for _, ty := range notInteger {
		if StepDomainIsInteger(ty) {
			t.Errorf("%v should not be an integer step domain", ty)
		}
	}
}

// A newtype is nominal but represented as its base, so a step over
// `newtype Count = u8` is an integer step.
func TestStepDomainIsIntegerThroughNewtype(t *testing.T) {
	count := &ConstrainedType{Name: "Count", Type: PrimitiveType{Name: UInt8}}
	if !StepDomainIsInteger(count) {
		t.Error("a newtype over u8 should be an integer step domain")
	}
	quarter := &ConstrainedType{Name: "Quarter", Type: PrimitiveType{Name: Float32}}
	if StepDomainIsInteger(quarter) {
		t.Error("a newtype over f32 should not be an integer step domain")
	}
}
