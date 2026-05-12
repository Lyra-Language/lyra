package collector_test

import "testing"

func TestCollector_ModuleDeclaration(t *testing.T) {
	runGoldenTest(t, `module myapp.utils`, "module_declaration")
}
