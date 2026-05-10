package collector_test

import (
	"path/filepath"
	"testing"
)

func TestCollector_BasicDataType(t *testing.T) {
	source := `pub stack data ColorName = Red | Green | Blue`
	program, _ := parseAndCollect(t, source)
	got := captureProgramPrint(program)
	checkGolden(t, got, filepath.Join("testdata", "basic_data_type.golden"))
}

func TestCollector_DataTypeWithGenericParameter(t *testing.T) {
	source := `pub data Maybe<t> = Nil | Some t`
	program, _ := parseAndCollect(t, source)
	got := captureProgramPrint(program)
	checkGolden(t, got, filepath.Join("testdata", "data_type_with_generic_parameter.golden"))
}

func TestCollector_DataTypeWithStructFields(t *testing.T) {
	source := `data Tree<t> = Nil | Leaf t | Node { left: Tree, value: t, right: Tree }`
	program, _ := parseAndCollect(t, source)
	got := captureProgramPrint(program)
	checkGolden(t, got, filepath.Join("testdata", "data_type_with_struct_fields.golden"))
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
	program, _ := parseAndCollect(t, source)
	got := captureProgramPrint(program)
	checkGolden(t, got, filepath.Join("testdata", "complex_data_type.golden"))
}
