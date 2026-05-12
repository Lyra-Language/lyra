package collector_test

import "testing"

func TestCollectPostfixPropertyAccess(t *testing.T) {
	runGoldenTest(t, `let name = person.name`, "postfix_property_access")
}

func TestCollectPostfixMemberConstPropertyAccess(t *testing.T) {
	runGoldenTest(t, `let max = limits.MAX`, "postfix_member_const_property_access")
}

func TestCollectPostfixOptionalPropertyAccess(t *testing.T) {
	runGoldenTest(t, `let name = person?.name`, "postfix_optional_property_access")
}

func TestCollectPostfixArrayIndexing(t *testing.T) {
	runGoldenTest(t, `let first = array[0]`, "postfix_array_indexing")
}

func TestCollectPostfixOptionalArrayIndexing(t *testing.T) {
	runGoldenTest(t, `let first = array?[0]`, "postfix_optional_array_indexing")
}

func TestCollectPostfixTryExpression(t *testing.T) {
	runGoldenTest(t, `let file = open_file("foo.txt")?`, "postfix_try_expression")
}

func TestCollectPostfixChainedOptionalMemberCalls(t *testing.T) {
	runGoldenTest(t, `let data = open_file(path)?.read()?.parse()?`, "postfix_chained_optional_member_calls")
}

func TestCollectPostfixChainedOptionalIndexAccess(t *testing.T) {
	runGoldenTest(t, `let value = matrix?[0]?[1]`, "postfix_chained_optional_index_access")
}

func TestCollectPostfixComplexChain(t *testing.T) {
	runGoldenTest(t, `let foo = struct.function("arg")?[0].property`, "postfix_complex_chain")
}
