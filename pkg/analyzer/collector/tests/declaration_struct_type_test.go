package collector_test

import "testing"

func TestCollector_StructTypeDeclaration(t *testing.T) {
	source := `
		pub stack struct Point {
			x: int,
			y: int = 0,
		}
	`
	runGoldenTest(t, source, "basic_struct_type_declaration")
}

func TestCollector_StructTypeDeclarationWithGenericParameters(t *testing.T) {
	source := `
		pub stack struct Point<t> {
			x: t,
			y: t,
		}
	`
	runGoldenTest(t, source, "struct_type_declaration_with_generic_parameters")
}

func TestCollector_StructTypeDeclarationWithGenericParamConstraints(t *testing.T) {
	source := `
		struct Pair<a: Display, b: Clone + Debug> {
			first: a,
			second: b,
		}
	`
	runGoldenTest(t, source, "struct_type_declaration_with_generic_param_constraints")
}
