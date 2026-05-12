package collector_test

import "testing"

func TestCollector_StructDerivesSingle(t *testing.T) {
	source := `
		@derive(Eq) struct Point {
			x: f32,
			y: f32,
		}
	`
	runGoldenTest(t, source, "struct_derives_single")
}

func TestCollector_StructDerivesMultiple(t *testing.T) {
	source := `
		@derive(Eq, Hash, Show) struct Point {
			x: f32,
			y: f32,
		}
	`
	runGoldenTest(t, source, "struct_derives_multiple")
}

func TestCollector_DataTypeDerives(t *testing.T) {
	source := `
		@derive(Eq, Show) data Color = Red | Green | Blue
	`
	runGoldenTest(t, source, "data_type_derives")
}

func TestCollector_StructNoDerive(t *testing.T) {
	source := `
		struct Point {
			x: f32,
			y: f32,
		}
	`
	runGoldenTest(t, source, "struct_no_derive")
}
