package collector_test

import "testing"

func TestCollector_BasicDataType(t *testing.T) {
	runGoldenTest(t, `pub stack data ColorName = Red | Green | Blue`, "basic_data_type")
}

func TestCollector_DataTypeWithGenericParameter(t *testing.T) {
	runGoldenTest(t, `pub data Maybe<t> = Nil | Some t`, "data_type_with_generic_parameter")
}

func TestCollector_DataTypeWithStructFields(t *testing.T) {
	runGoldenTest(t, `data Tree<t> = Nil | Leaf t | Node { left: Tree, value: t, right: Tree }`, "data_type_with_struct_fields")
}

func TestCollector_ComplexDataType(t *testing.T) {
	source := `
		data CSSColor =
			| ColorName CSSColorName
			| Hex HexStr
			| RGB { r: i8, g: i8, b: i8 }
			| HSL { hue: Hue, sat: float, light: float, alpha: Alpha }

		data Hue = HueDeg Angle | HueRadian Radian | HueTurn Turn
	`
	runGoldenTest(t, source, "complex_data_type")
}
