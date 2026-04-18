package collector_test

import (
	"path/filepath"
	"testing"
)

func TestCollector_ImportStatement(t *testing.T) {
	source := `import myapp.utils`
	program, _ := parseAndCollect(t, source)
	got := captureProgramPrint(program)
	checkGolden(t, got, filepath.Join("testdata", "import_statement.golden"))
}

func TestCollector_ImportStatementWithAlias(t *testing.T) {
	source := `import myapp.utils as utils`
	program, _ := parseAndCollect(t, source)
	got := captureProgramPrint(program)
	checkGolden(t, got, filepath.Join("testdata", "import_statement_with_alias.golden"))
}

func TestCollector_ImportStatementWithMembers(t *testing.T) {
	source := `import myapp.utils.{ Map, Set }`
	program, _ := parseAndCollect(t, source)
	got := captureProgramPrint(program)
	checkGolden(t, got, filepath.Join("testdata", "import_statement_with_members.golden"))
}

func TestCollector_ImportStatementWithMemberAliases(t *testing.T) {
	source := `import std.collections.{ HashMap as HM, Set }`
	program, _ := parseAndCollect(t, source)
	got := captureProgramPrint(program)
	checkGolden(t, got, filepath.Join("testdata", "import_statement_with_member_aliases.golden"))
}

func TestCollector_ImportStatementWithMultipleImports(t *testing.T) {
	source := `
	import std.io as io
	import std.collections.{ Map, Set }
	import math.{ sin, cos, PI }`
	program, _ := parseAndCollect(t, source)
	got := captureProgramPrint(program)
	checkGolden(t, got, filepath.Join("testdata", "import_statement_with_multiple_imports.golden"))
}