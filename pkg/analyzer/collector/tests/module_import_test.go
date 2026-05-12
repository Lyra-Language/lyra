package collector_test

import "testing"

func TestCollector_ImportStatement(t *testing.T) {
	runGoldenTest(t, `import myapp.utils`, "import_statement")
}

func TestCollector_ImportStatementWithAlias(t *testing.T) {
	runGoldenTest(t, `import myapp.utils as utils`, "import_statement_with_alias")
}

func TestCollector_ImportStatementWithMembers(t *testing.T) {
	runGoldenTest(t, `import myapp.utils.{ Map, Set }`, "import_statement_with_members")
}

func TestCollector_ImportStatementWithMemberAliases(t *testing.T) {
	runGoldenTest(t, `import std.collections.{ HashMap as HM, Set }`, "import_statement_with_member_aliases")
}

func TestCollector_ImportStatementWithMultipleImports(t *testing.T) {
	source := `
	import std.io as io
	import std.collections.{ Map, Set }
	import math.{ sin, cos, PI }`
	runGoldenTest(t, source, "import_statement_with_multiple_imports")
}
