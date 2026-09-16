package collector_test

import "testing"

// `${` has no escape in an ordinary string, so a program meaning the two characters
// literally opens an interpolation that swallows source to the next `}` — declarations
// included — and still parses. The report was then an undefined name far below; it is
// lyra-E080 at the `${` itself (09/17).
func TestCollector_InterpolationSpanningLinesIsRefused(t *testing.T) {
	// The shape that cost an hour in lyrafmt: the `${` swallows to the `}` in the *next*
	// declaration's string, so `closes` is never declared and the file type-checks with
	// the wrong meaning.
	errs := parseAndCollectErrors(t, `
let opens = pure (k: string) -> bool => k == "{" || k == "${"
let closes = pure (k: string) -> bool => k == "}"`)
	assertCollectorErrorContains(t, errs, "runs past the end of its line")

	// A newline inside the braces is the same swallow, spelled deliberately.
	assertCollectorErrorContains(t, parseAndCollectErrors(t, `
let n = 1
let s = "${
  n
}"`), "runs past the end of its line")
}

// Every interpolation a program actually writes stays legal: braces of its own, one
// interpolation nested in another, several on a line, and the raw form the diagnostic
// recommends for the literal characters.
func TestCollector_SingleLineInterpolationsAreFine(t *testing.T) {
	for name, src := range map[string]string{
		"a block inside the expression": `let f = (n: i64) -> string => "${if n > 1 { "big" } else { "small" }}"`,
		"nested":                        `let f = (n: i64) -> string => "outer ${"inner ${n}"} done"`,
		"several on one line":           `let f = (n: i64) -> string => "a ${n} b ${n} c ${n}"`,
		"the raw spelling":              "let f = () -> string => `a literal ${ stays literal`",
	} {
		t.Run(name, func(t *testing.T) {
			if errs := parseAndCollectErrors(t, src); len(errs) != 0 {
				t.Errorf("expected no collector errors, got %v", errs)
			}
		})
	}
}
