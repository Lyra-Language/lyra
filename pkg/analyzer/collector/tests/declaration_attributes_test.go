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

// `@must_release` reads one release function and **reports a second rather than dropping
// it**. It always read only the first, deliberately — a release is one call — but the
// extra name vanished silently, which reads as "both of these discharge it": the
// obligation stayed live and the attribute said, on its face, why it should not have.
// Reported after a `delete` wrapper was added to `bindings/treesitter` in exactly that
// shape and the warning it was meant to silence did not move (09/25).
func TestMustReleaseAttribute_SecondArgumentIsReported(t *testing.T) {
	src := "@must_release(free_it, delete)\npub struct R { raw: i64 }\n" +
		"let free_it = (r: own R) -> void => {}\n"
	found := false
	for _, e := range parseAndCollectErrors(t, src) {
		found = found || strings.Contains(e.Error(), `"delete" is a second`)
	}
	if !found {
		t.Error("a second release function should be reported, not ignored")
	}
}

// One argument is the ordinary shape and stays silent.
func TestMustReleaseAttribute_OneArgumentIsClean(t *testing.T) {
	src := "@must_release(free_it)\npub struct R { raw: i64 }\n" +
		"let free_it = (r: own R) -> void => {}\n"
	for _, e := range parseAndCollectErrors(t, src) {
		if strings.Contains(e.Error(), "must_release") {
			t.Errorf("unexpected diagnostic: %v", e)
		}
	}
}
