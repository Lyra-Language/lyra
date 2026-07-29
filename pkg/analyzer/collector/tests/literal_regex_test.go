package collector_test

import "testing"

func TestCollect_RegexLiteral(t *testing.T) {
	source := `let phone = r"[2-9][0-9]{2} [2-9][0-9]{2} [0-9]{4}$"`
	runGoldenTest(t, source, "literal_regex")
}

// A `/` is ordinary content now that the delimiters are quotes — the old `r/…/`
// form needed `\/` for every slash, which is why this case exists.
func TestCollect_RegexLiteralWithSlashes(t *testing.T) {
	source := `let path = r"/usr/local/bin"`
	runGoldenTest(t, source, "literal_regex_with_slashes")
}

// The delimiter itself is what needs escaping now: `\"` inside the pattern.
func TestCollect_RegexLiteralWithEscape(t *testing.T) {
	source := `let quoted = r"\"[a-z]+\""`
	runGoldenTest(t, source, "literal_regex_with_escape")
}
