package collector_test

import (
	"strings"
	"testing"
)

// A struct literal giving one field two values, or a struct pattern naming one twice, is
// lyra-E075. The later value used to win silently — every literal form reads its fields by
// name — so `P { x: 1, x: 2, y: 3 }` built `x = 2` and type-checked, since each value fits.
// Reported once, at the second occurrence, naming the first (09/13).
func TestDuplicateStructField_EveryLiteralFormAndPattern(t *testing.T) {
	for _, c := range []struct{ name, src, want string }{
		{"named literal", `let p = P { x: 1, x: 2, y: 3 }`, `field "x" is given a value twice in this literal (first at 1:13)`},
		{"anonymous literal", `let q = { a: 1, a: 2 }`, `field "a" is given a value twice in this literal (first at 1:11)`},
		{"record update", `let p = P { b | y: 5, y: 6 }`, `field "y" is given a value twice in this literal (first at 1:17)`},
		{"inline-record constructor", `let n = Node { v: 1, v: 2, w: 3 }`, `field "v" is given a value twice in this literal`},
		{"struct pattern", `let f = (p: P) -> i64 => match p { P { x, x: y } => y }`, `field "x" is matched twice in this pattern`},
	} {
		t.Run(c.name, func(t *testing.T) {
			errs := parseAndCollectErrors(t, c.src)
			var hits []string
			for _, e := range errs {
				if strings.Contains(e.Error(), "twice in this") {
					hits = append(hits, e.Error())
				}
			}
			if len(hits) != 1 || !strings.Contains(hits[0], c.want) {
				t.Errorf("want exactly one error containing %q, got %v", c.want, errs)
			}
		})
	}
}

// Distinct fields are untouched, and so are the pattern forms that are not field names.
func TestDuplicateStructField_DistinctFieldsAreFine(t *testing.T) {
	for _, src := range []string{
		`let p = P { x: 1, y: 2 }`,
		`let p = P { b | x: 1, y: 2 }`,
		`let f = (p: P) -> i64 => match p { P { x, y: _ } => x }`,
		`let f = (p: P) -> i64 => match p { P { x, ... } => x }`,
	} {
		for _, e := range parseAndCollectErrors(t, src) {
			if strings.Contains(e.Error(), "twice in this") {
				t.Errorf("%s: unexpected %v", src, e)
			}
		}
	}
}
