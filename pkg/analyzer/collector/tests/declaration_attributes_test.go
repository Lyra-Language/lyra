package collector_test

import (
	"strings"
	"testing"
)

// An attribute on a declaration was parsed and silently dropped until 09/13, so a
// misspelled `@borrowed` changed nothing and said nothing. `@borrowed` is the one there is,
// and it needs a function.
func TestDeclarationAttributes(t *testing.T) {
	for _, c := range []struct{ name, src, want string }{
		{"unknown", "@borowed\nlet f = () -> i64 => 1", "unknown attribute `@borowed` on a declaration"},
		{"on a value", "@borrowed\nlet n = 5", "goes on a function declaration"},
		{"with arguments", "@borrowed(x)\nlet f = () -> i64 => 1", "`@borrowed` takes no arguments"},
		{"on a destructuring", "@borrowed\nlet (a, b) = t", "goes on a function declaration"},
	} {
		t.Run(c.name, func(t *testing.T) {
			errs := parseAndCollectErrors(t, c.src)
			found := false
			for _, e := range errs {
				found = found || strings.Contains(e.Error(), c.want)
			}
			if !found {
				t.Errorf("want an error containing %q, got %v", c.want, errs)
			}
		})
	}
	for _, e := range parseAndCollectErrors(t, "@borrowed\npub let f = () -> i64 => 1") {
		t.Errorf("unexpected %v", e)
	}
}
