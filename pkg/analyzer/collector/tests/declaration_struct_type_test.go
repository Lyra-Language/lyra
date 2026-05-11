package collector_test

import (
	"path/filepath"
	"testing"
)

func TestCollector_StructTypeDeclaration(t *testing.T) {
	source := `
		pub stack struct Point {
			x: int,
			y: int = 0,
		}
	`
	program, _ := parseAndCollect(t, source)
	got := captureProgramPrint(program)
	checkGolden(t, got, filepath.Join("testdata", "basic_struct_type_declaration.golden"))
}

func TestCollector_StructTypeDeclarationWithGenericParameters(t *testing.T) {
	source := `
		pub stack struct Point<t> {
			x: t,
			y: t,
		}
	`
	program, _ := parseAndCollect(t, source)
	got := captureProgramPrint(program)
	checkGolden(t, got, filepath.Join("testdata", "struct_type_declaration_with_generic_parameters.golden"))
}
