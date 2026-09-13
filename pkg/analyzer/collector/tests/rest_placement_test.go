package collector_test

import (
	"strings"
	"testing"
)

// A `...rest` is lyra-E076 where it cannot say what it covers: before the end of an array
// pattern, or a second one in any pattern. A tuple's rest may sit anywhere, since its
// positions after the rest count from the end (09/13).
func TestRestPlacement(t *testing.T) {
	for _, c := range []struct{ name, src, want string }{
		{"array rest at start", `let f = (xs: []i64) -> i64 => match xs { [...head, a] => a, _ => 0 }`, "must be the last element of an array pattern"},
		{"array rest in middle", `let f = (xs: []i64) -> i64 => match xs { [a, ...mid, c] => a, _ => 0 }`, "must be the last element of an array pattern"},
		{"two tuple rests", `let (a, ...r, ...s) = t`, "at most one `...rest`"},
		{"two payload rests", `let f = (s: S) -> i64 => match s { Tri(...a, ...b) => 0, _ => 1 }`, "at most one `...rest`"},
	} {
		t.Run(c.name, func(t *testing.T) {
			errs := parseAndCollectErrors(t, c.src)
			var hits []string
			for _, e := range errs {
				if strings.Contains(e.Error(), "...rest") {
					hits = append(hits, e.Error())
				}
			}
			if len(hits) != 1 || !strings.Contains(hits[0], c.want) {
				t.Errorf("want exactly one error containing %q, got %v", c.want, errs)
			}
		})
	}
}

func TestRestPlacement_AllowedPositions(t *testing.T) {
	for _, src := range []string{
		`let (...r, z) = t`,
		`let (a, ...r, z) = t`,
		`let (a, ...r) = t`,
		`let f = (xs: []i64) -> i64 => match xs { [h, ...t] => h, [...all] => 0 }`,
	} {
		for _, e := range parseAndCollectErrors(t, src) {
			if strings.Contains(e.Error(), "...rest") {
				t.Errorf("%s: unexpected %v", src, e)
			}
		}
	}
}
