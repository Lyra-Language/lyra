package collector_test

import "testing"

func TestCollect_RegexLiteral(t *testing.T) {
	source := `let phone = r/[2-9][0-9]{2} [2-9][0-9]{2} [0-9]{4}$/`
	runGoldenTest(t, source, "literal_regex")
}

func TestCollect_RegexLiteralWithEscape(t *testing.T) {
	source := `let path = r/\/usr\/local\/bin/`
	runGoldenTest(t, source, "literal_regex_with_escape")
}
