package collector

import (
	"path/filepath"
	"testing"
)

func TestCollector_BasicConstrainedTypeWithoutConstraints(t *testing.T) {
	source := `type Angle = float`
	program, _ := parseAndCollect(t, source)
	got := captureProgramPrint(program)
	checkGolden(t, got, filepath.Join("testdata", "basic_constrained_type_without_constraints.golden"))
}

func TestCollector_ConstrainedTypeWithLiteralUnionConstraintWithStrings(t *testing.T) {
	source := `type Color = string where values("red", "green", "blue")`
	program, _ := parseAndCollect(t, source)
	got := captureProgramPrint(program)
	checkGolden(t, got, filepath.Join("testdata", "constrained_type_with_literal_union_constraint_with_strings.golden"))
}

func TestCollector_ConstrainedTypeWithLiteralUnionConstraintWithNumbers(t *testing.T) {
	source := `type Status = i32 where values(200, 404, 500)`
	program, _ := parseAndCollect(t, source)
	got := captureProgramPrint(program)
	checkGolden(t, got, filepath.Join("testdata", "constrained_type_with_literal_union_constraint_with_numbers.golden"))
}

func TestCollector_SimpleRangeConstrainedType(t *testing.T) {
	source := `type Angle = float where range(0..<360)`
	program, _ := parseAndCollect(t, source)
	got := captureProgramPrint(program)
	checkGolden(t, got, filepath.Join("testdata", "simple_range_constrained_type.golden"))
}

func TestCollector_RangeConstrainedTypeWithConstantMultiplication(t *testing.T) {
	source := `type Radian = float where range(0.0..<PI*2.0)`
	program, _ := parseAndCollect(t, source)
	got := captureProgramPrint(program)
	checkGolden(t, got, filepath.Join("testdata", "range_constrained_type_with_constant_multiplication.golden"))
}

func TestCollector_RangeConstrainedTypeWithVariableAdditionAndSubtraction(t *testing.T) {
	source := `
		let pi = 3.14159
		type Radian = float where range(pi-3..<pi+3)
	`
	program, _ := parseAndCollect(t, source)
	got := captureProgramPrint(program)
	checkGolden(t, got, filepath.Join("testdata", "range_constrained_type_with_variable_addition_and_subtraction.golden"))
}

func TestCollector_MultipleConstraints(t *testing.T) {
	source := `type Radian = float where range(0..<PI*2), precision(0.01)`
	program, _ := parseAndCollect(t, source)
	got := captureProgramPrint(program)
	checkGolden(t, got, filepath.Join("testdata", "multiple_constraints.golden"))
}

func TestCollector_StepConstrainedType(t *testing.T) {
	source := `type CompassHeading = int where range(0..<360), step(15)`
	program, _ := parseAndCollect(t, source)
	got := captureProgramPrint(program)
	checkGolden(t, got, filepath.Join("testdata", "step_constrained_type.golden"))
}

func TestCollector_PatternConstrainedType(t *testing.T) {
	source := `type HexStr = string where pattern(r/^#(?:[0-9a-fA-F]{3}){1,2}$/)`
	program, _ := parseAndCollect(t, source)
	got := captureProgramPrint(program)
	checkGolden(t, got, filepath.Join("testdata", "pattern_constrained_type.golden"))
}
