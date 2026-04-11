package collector_test

import (
	"path/filepath"
	"testing"
)

func TestCollectPostfixPropertyAccess(t *testing.T) {
	source := `let name = person.name`
	program, _ := parseAndCollect(t, source)
	got := captureProgramPrint(program)
	checkGolden(t, got, filepath.Join("testdata", "postfix_property_access.golden"))
}

func TestCollectPostfixOptionalPropertyAccess(t *testing.T) {
	source := `let name = person?.name`
	program, _ := parseAndCollect(t, source)
	got := captureProgramPrint(program)
	checkGolden(t, got, filepath.Join("testdata", "postfix_optional_property_access.golden"))
}

func TestCollectPostfixArrayIndexing(t *testing.T) {
	source := `let first = array[0]`
	program, _ := parseAndCollect(t, source)
	got := captureProgramPrint(program)
	checkGolden(t, got, filepath.Join("testdata", "postfix_array_indexing.golden"))
}

func TestCollectPostfixOptionalArrayIndexing(t *testing.T) {
	source := `let first = array?[0]`
	program, _ := parseAndCollect(t, source)
	got := captureProgramPrint(program)
	checkGolden(t, got, filepath.Join("testdata", "postfix_optional_array_indexing.golden"))
}

func TestCollectPostfixTryExpression(t *testing.T) {
	source := `let file = open_file("foo.txt")?`
	program, _ := parseAndCollect(t, source)
	got := captureProgramPrint(program)
	checkGolden(t, got, filepath.Join("testdata", "postfix_try_expression.golden"))
}

func TestCollectPostfixChainedOptionalMemberCalls(t *testing.T) {
	source := `let data = open_file(path)?.read()?.parse()?`
	program, _ := parseAndCollect(t, source)
	got := captureProgramPrint(program)
	checkGolden(t, got, filepath.Join("testdata", "postfix_chained_optional_member_calls.golden"))
}

func TestCollectPostfixChainedOptionalIndexAccess(t *testing.T) {
	source := `let value = matrix?[0]?[1]`
	program, _ := parseAndCollect(t, source)
	got := captureProgramPrint(program)
	checkGolden(t, got, filepath.Join("testdata", "postfix_chained_optional_index_access.golden"))
}

func TestCollectPostfixComplexChain(t *testing.T) {
	source := `let foo = struct.function("arg")?[0].property`
	program, _ := parseAndCollect(t, source)
	got := captureProgramPrint(program)
	checkGolden(t, got, filepath.Join("testdata", "postfix_complex_chain.golden"))
}