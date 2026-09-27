package driver_test

import (
	"strings"
	"testing"

	"github.com/Lyra-Language/lyra/pkg/driver"
)

func TestDriver_CollectsLinksSortedAndDeduplicated(t *testing.T) {
	res := driver.Analyze([]byte(`
@link("m")
unsafe extern pure sqrt: (x: f64) -> f64
@link("m")
unsafe extern pure log: (x: f64) -> f64
@link("z")
extern compress: (n: i64) -> i64
extern plain: () -> i32
`))
	if got := strings.Join(res.Links, ","); got != "m,z" {
		t.Errorf("Links = %q; want \"m,z\" (sorted, deduplicated)", got)
	}
}

func TestDriver_NoLinksWhenNothingAsks(t *testing.T) {
	res := driver.Analyze([]byte(`let main = () -> void => { println(1) }`))
	if len(res.Links) != 0 {
		t.Errorf("Links = %v; want none", res.Links)
	}
}

// **`@link`'s `pkg:` names where a library is** (09/26): the pkg-config package lyrac asks
// for its directory, so a Homebrew install links with no `LIBRARY_PATH`. Collected from a
// module header and from an extern alike, sorted and deduplicated like the libraries.
func TestDriver_CollectsLinkPackages(t *testing.T) {
	res := driver.Analyze([]byte(`@link("SDL3", pkg: "sdl3")
module main
@link("SDL3_image", pkg: "sdl3-image")
unsafe extern img: (n: i32) -> i32
@link("m")
unsafe extern pure sqrt: (x: f64) -> f64
`))
	if res.HasErrors() {
		t.Fatalf("unexpected errors: %v", res.Errors())
	}
	if got := strings.Join(res.Links, ","); got != "SDL3,SDL3_image,m" {
		t.Errorf("Links = %q; want \"SDL3,SDL3_image,m\"", got)
	}
	if got := strings.Join(res.Packages, ","); got != "sdl3,sdl3-image" {
		t.Errorf("Packages = %q; want \"sdl3,sdl3-image\"", got)
	}
}

// A named argument is read, or refused by name — never parsed and dropped.
func TestDriver_NamedAttributeArgumentsAreCheckedNotIgnored(t *testing.T) {
	for _, c := range []struct{ src, want string }{
		{"@link(\"SDL3\", package: \"sdl3\")\nmodule main", "`@link` has one named argument, `pkg:`"},
		{"@link(\"SDL3\", pkg: sdl3)\nmodule main", "`pkg:` names a pkg-config package as a string"},
		{"@link(pkg: \"sdl3\")\nmodule main", "names the library itself as well as its package"},
		{"@must_release(close, pkg: \"x\") struct H { n: i64 }\nlet close = (h: H) -> void => {}", "`@must_release` takes no named arguments"},
	} {
		res := driver.Analyze([]byte(c.src))
		found := false
		for _, e := range res.Errors() {
			if strings.Contains(e.Message, c.want) {
				found = true
			}
		}
		if !found {
			t.Errorf("%q: want an error containing %q; got %v", c.src, c.want, res.Errors())
		}
	}
}
