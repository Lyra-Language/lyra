package collector_test

import "testing"

func TestCollect_YieldExpr(t *testing.T) {
	source := `
	let counter = gen () -> Gen<i32> => {
		yield 1
		yield 2
	}`
	runGoldenTest(t, source, "expr_yield")
}

func TestCollect_YieldFromExpr(t *testing.T) {
	source := `
	let behavior = gen (enemy: Enemy) -> Gen<Command> => {
		yield from patrol(enemy.path)
	}`
	runGoldenTest(t, source, "expr_yield_from")
}

func TestCollect_YieldAndYieldFrom(t *testing.T) {
	source := `
	let mixed = gen () -> Gen<i32> => {
		yield 1
		yield from range(2, 5)
		yield 6
	}`
	runGoldenTest(t, source, "expr_yield_and_yield_from")
}
