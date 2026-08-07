package collector_test

import (
	"testing"

	"github.com/Lyra-Language/lyra/pkg/parser"
)

func TestCollector_BasicConstrainedTypeWithoutConstraints(t *testing.T) {
	runGoldenTest(t, `newtype Angle = f64`, "basic_constrained_type_without_constraints")
}

func TestCollector_ConstrainedTypeWithLiteralUnionConstraintWithStrings(t *testing.T) {
	runGoldenTest(t, `newtype Color = string where values("red", "green", "blue")`, "constrained_type_with_literal_union_constraint_with_strings")
}

func TestCollector_ConstrainedTypeWithLiteralUnionConstraintWithNumbers(t *testing.T) {
	runGoldenTest(t, `newtype Status = i32 where values(200, 404, 500)`, "constrained_type_with_literal_union_constraint_with_numbers")
}

func TestCollector_SimpleRangeConstrainedType(t *testing.T) {
	runGoldenTest(t, `newtype Angle = f64 where range(0..<360)`, "simple_range_constrained_type")
}

func TestCollector_RangeConstrainedTypeWithConstantMultiplication(t *testing.T) {
	runGoldenTest(t, `newtype Radian = f64 where range(0.0..<PI*2.0)`, "range_constrained_type_with_constant_multiplication")
}

func TestCollector_RangeConstrainedTypeWithNegation(t *testing.T) {
	runGoldenTest(t, `newtype Temp = i64 where range(-100..<200)`, "range_constrained_type_with_negation")
}

func TestCollector_RangeConstrainedTypeWithVariableAdditionAndSubtraction(t *testing.T) {
	source := `
		let pi = 3.14159
		newtype Radian = f64 where range(pi-3..<pi+3)
	`
	runGoldenTest(t, source, "range_constrained_type_with_variable_addition_and_subtraction")
}

func TestCollector_MultipleConstraints(t *testing.T) {
	runGoldenTest(t, `newtype Radian = f64 where range(0..<PI*2), precision(0.01)`, "multiple_constraints")
}

func TestCollector_StepConstrainedType(t *testing.T) {
	runGoldenTest(t, `newtype CompassHeading = i64 where range(0..<360), step(15)`, "step_constrained_type")
}

func TestCollector_PatternConstrainedType(t *testing.T) {
	runGoldenTest(t, `newtype HexStr = string where pattern(r"^#(?:[0-9a-fA-F]{3}){1,2}$")`, "pattern_constrained_type")
}

// **A generic `newtype` does not parse.** `newtype Point<t> = Tuple` puts the `<t>` in an
// ERROR node, and the golden this test used to run recorded a ConstrainedType with the
// parameters silently dropped — a golden file documenting the bug as if it were the
// intended output, under a test named for the feature.
//
// It passed because the golden helper never asked whether the source parsed: a truncated
// AST prints just fine, and a golden regenerated from one bakes the truncation in. That
// helper now checks (08/07), which is what surfaced this.
//
// Kept, inverted, as the record of the gap: flip it back to a golden test when a generic
// newtype parses (todo.md).
func TestCollector_ParameterizedConstrainedTypeIsNotYetParseable(t *testing.T) {
	tree, err := parser.Parse(`newtype Point<t> = Tuple`)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if !tree.RootNode().HasError() {
		t.Fatal("a generic newtype parses now — restore the golden test")
	}
}
