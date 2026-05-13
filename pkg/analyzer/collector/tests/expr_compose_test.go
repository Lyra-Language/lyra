package collector_test

import "testing"

func TestCollect_ComposeExpr(t *testing.T) {
	source := `let transform = negate ->> double`
	runGoldenTest(t, source, "expr_compose")
}

func TestCollect_ComposeExprChained(t *testing.T) {
	source := `let pipeline = parse ->> validate ->> serialize`
	runGoldenTest(t, source, "expr_compose_chained")
}
