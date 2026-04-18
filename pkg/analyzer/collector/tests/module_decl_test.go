package collector_test

import (
	"path/filepath"
	"testing"
)

func TestCollector_ModuleDeclaration(t *testing.T) {
	source := `module myapp.utils`
	program, _ := parseAndCollect(t, source)
	got := captureProgramPrint(program)
	checkGolden(t, got, filepath.Join("testdata", "module_declaration.golden"))
}
