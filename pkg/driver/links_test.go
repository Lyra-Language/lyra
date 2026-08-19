package driver_test

import (
	"strings"
	"testing"

	"github.com/Lyra-Language/lyra/pkg/driver"
)

func TestDriver_CollectsLinksSortedAndDeduplicated(t *testing.T) {
	res := driver.Analyze([]byte(`
@link("m")
unsafe extern pure sqrt: (f64) -> f64
@link("m")
unsafe extern pure log: (f64) -> f64
@link("z")
extern compress: (i64) -> i64
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
